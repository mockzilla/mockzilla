package portable

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/mockzilla/mockzilla/v2/pkg/api"
	"github.com/mockzilla/mockzilla/v2/pkg/factory"
	"github.com/mockzilla/mockzilla/v2/pkg/generator"
	validator "github.com/pb33f/libopenapi-validator"
)

// handler implements the api.Handler interface using a factory.Factory
// to generate mock responses directly from an OpenAPI spec - no codegen needed.
type handler struct {
	factory *factory.Factory
	routes  api.RouteDescriptions
}

// newHandler creates a handler from raw OpenAPI spec bytes.
func newHandler(specBytes []byte, opts ...factory.FactoryOption) (*handler, error) {
	f, err := factory.NewFactory(specBytes, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating factory: %w", err)
	}

	ops := f.Operations()
	routes := make(api.RouteDescriptions, 0, len(ops))
	for _, op := range ops {
		routes = append(routes, &api.RouteDescription{
			ID:       op.ID,
			Method:   op.Method,
			Path:     op.Path,
			IsStatic: op.IsStatic,
		})
	}
	routes.Sort()

	return &handler{
		factory: f,
		routes:  routes,
	}, nil
}

// Routes returns the route descriptions extracted from the OpenAPI spec.
func (h *handler) Routes() api.RouteDescriptions {
	return h.routes
}

// RegisterRoutes registers a catch-all that delegates to the factory for matching.
func (h *handler) RegisterRoutes(router chi.Router) {
	router.HandleFunc("/*", h.handleRequest)
}

// Generate handles UI generate requests. It decodes a GenerateRequest from the
// body and returns a generated request for the specified path and method.
// This is called by the UI's /.services/{name}/generate endpoint.
func (h *handler) Generate(w http.ResponseWriter, r *http.Request) {
	var req api.GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		message := err.Error()
		if errors.Is(err, io.EOF) {
			message = "request body is empty or incomplete"
		}
		slog.Error("Failed to decode generate request", "error", err)
		http.Error(w, message, http.StatusBadRequest)
		return
	}

	res, err := h.factory.Request(req.Path, req.Method, req.Context)
	if err != nil {
		slog.Debug("No matching operation for generate", "method", req.Method, "path", req.Path, "error", err)
		http.Error(w, fmt.Sprintf("no matching operation: %s %s", req.Method, req.Path), http.StatusNotFound)
		return
	}

	api.NewJSONResponse(w).Send(res)
}

// handleRequest serves mock API responses for incoming HTTP requests.
// The endpoint path is extracted from chi's wildcard parameter, which gives us
// the path relative to the service mount point (prefix already stripped).
func (h *handler) handleRequest(w http.ResponseWriter, r *http.Request) {
	ctx := api.ExtractContextFromRequest(r)

	// chi's "*" param gives us the path within the mounted sub-router,
	// with the service prefix already stripped by chi's Route().
	endpointPath := "/" + chi.URLParam(r, "*")

	specPath, ok := h.factory.MatchPath(endpointPath, r.Method)
	if !ok {
		slog.Debug("No matching operation", "method", r.Method, "path", endpointPath)
		http.Error(w, fmt.Sprintf("no matching operation: %s %s", r.Method, endpointPath), http.StatusNotFound)
		return
	}

	resp, err := h.factory.Response(specPath, r.Method, ctx, generator.WithRequest(r))
	if err != nil {
		slog.Debug("Failed to generate response", "method", r.Method, "path", specPath, "error", err)
		http.Error(w, fmt.Sprintf("failed to generate response: %s %s", r.Method, endpointPath), http.StatusInternalServerError)
		return
	}

	op := h.factory.FindOperation(specPath, r.Method)

	// Set response headers from the generated response
	for key, values := range resp.Headers {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}

	// Set content-type if not already set by response headers.
	// Prefer the content-type declared in the spec for this operation's success
	// response, falling back to application/json when the spec doesn't declare one.
	if w.Header().Get("Content-Type") == "" {
		contentType := "application/json"
		if op != nil && op.Response != nil {
			if item := op.Response.GetSuccess(); item != nil && item.ContentType != "" {
				contentType = item.ContentType
			}
		}
		w.Header().Set("Content-Type", contentType)
	}

	// Factory.SuccessStatusCode rewrites the codegen-fabricated 204 when
	// the spec declares no 2xx, substituting a code the spec actually
	// declares so response validation passes.
	if statusCode := h.factory.SuccessStatusCode(specPath, r.Method); statusCode > 0 {
		w.WriteHeader(statusCode)
	}

	// RFC 9110 §15.6: HEAD responses must not include a body.
	if r.Method != http.MethodHead && resp.Body != nil {
		_, _ = w.Write(resp.Body)
	}
}

// swappableHandler wraps a handler with a mutex for hot-swapping. The
// validator is stored alongside so hot-reloads can rebuild both
// atomically; the validation middleware reads through Validator() so it
// always sees the validator built from the most recent spec.
type swappableHandler struct {
	mu        sync.RWMutex
	handler   *handler
	validator validator.Validator
	buildFn   func() (validator.Validator, error)

	// buildStarted/buildDone gate the lazy validator build. CompareAndSwap
	// on buildStarted ensures exactly one builder goroutine runs;
	// buildDone closes when that goroutine finishes (success or fail) so
	// WaitForValidator can park on it. Both are reset by swap so a
	// hot-reload triggers a fresh build for the new spec.
	buildStarted atomic.Bool
	buildDone    chan struct{}
}

// Validator returns the currently active validator for this service, or
// nil when validation isn't configured for it.
func (s *swappableHandler) Validator() validator.Validator {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.validator
}

// EnsureValidator kicks off the lazy validator build (idempotently) and
// returns whatever's already cached. Non-blocking: if the build is in
// flight it returns nil and the caller proceeds without validation - a
// later request after the build completes will see the validator. This
// matters for pathological specs whose validator construction takes
// minutes; blocking the request path would hang every caller behind it.
// Use WaitForValidator if you need to block until the build finishes.
func (s *swappableHandler) EnsureValidator() validator.Validator {
	if v := s.Validator(); v != nil {
		return v
	}
	s.startBuild()
	return s.Validator()
}

// WaitForValidator triggers the lazy build (idempotently) and blocks
// until it finishes. Returns the built validator, or nil if buildFn is
// unset or the build failed. The eager startup goroutine uses this so
// portable.Setup.WaitForValidators(ctx) can tell when every service is
// ready; production request handlers should call EnsureValidator
// instead so they degrade rather than hang.
func (s *swappableHandler) WaitForValidator() validator.Validator {
	if v := s.Validator(); v != nil {
		return v
	}

	s.startBuild()
	s.mu.RLock()
	done := s.buildDone
	s.mu.RUnlock()

	if done == nil {
		return nil
	}

	<-done
	return s.Validator()
}

// startBuild fires the validator construction in a background goroutine
// exactly once. Subsequent callers see buildStarted already true and
// return immediately; they can either accept the current Validator()
// state or park on buildDone.
func (s *swappableHandler) startBuild() {
	if !s.buildStarted.CompareAndSwap(false, true) {
		return
	}

	s.mu.RLock()
	build := s.buildFn
	done := s.buildDone
	s.mu.RUnlock()
	if build == nil || done == nil {
		if done != nil {
			close(done)
		}
		return
	}

	go func() {
		defer close(done)
		built, err := build()
		if err != nil {
			slog.Warn("Lazy validator construction failed; service will run without validation", "error", err)
			return
		}
		s.setValidator(built)
	}()
}

// MatchPath resolves a concrete request path and method to the spec path
// pattern that handles it. The validation middleware uses this to detect
// routes whose spec keys use the `#`-discriminator convention so it can
// skip validation for them; see [middleware.CreateValidationMiddleware].
func (s *swappableHandler) MatchPath(reqPath, method string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.handler.factory.MatchPath(reqPath, method)
}

func (s *swappableHandler) Routes() api.RouteDescriptions {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.handler.Routes()
}

func (s *swappableHandler) RegisterRoutes(router chi.Router) {
	router.HandleFunc("/*", s.handleRequest)
}

// Generate handles UI generate requests (called via /.services/{name}/generate).
func (s *swappableHandler) Generate(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	s.handler.Generate(w, r)
}

// ensureValidator adapts EnsureValidator/Validator to the middleware's
// ValidatorSource signature: ensure=true triggers the sync-once build,
// ensure=false returns whatever's already loaded (nil if nothing yet).
func (s *swappableHandler) ensureValidator(ensure bool) validator.Validator {
	if ensure {
		return s.EnsureValidator()
	}
	return s.Validator()
}

// handleRequest delegates to the current handler's handleRequest.
func (s *swappableHandler) handleRequest(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	s.handler.handleRequest(w, r)
}

func (s *swappableHandler) swap(h *handler, v validator.Validator, build func() (validator.Validator, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = h
	s.validator = v
	s.buildFn = build

	// Reset the lazy-build guard so a request that opts into validation
	// after this reload rebuilds against the new spec, not the previous
	// cached miss. An in-flight build from before the swap still runs
	// to completion and writes its result via setValidator - the same
	// race the previous sync.Once-based implementation had.
	s.buildStarted.Store(false)
	s.buildDone = make(chan struct{})
}

// setValidator swaps just the validator in place. Callers race with
// concurrent reads from Validator() in the middleware, so the swap
// happens under the same mutex.
func (s *swappableHandler) setValidator(v validator.Validator) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.validator = v
}

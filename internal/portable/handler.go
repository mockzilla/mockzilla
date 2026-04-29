package portable

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/mockzilla/mockzilla/v2/pkg/api"
	"github.com/mockzilla/mockzilla/v2/pkg/factory"
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
			ID:     op.ID,
			Method: op.Method,
			Path:   op.Path,
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

	resp, err := h.factory.Response(specPath, r.Method, ctx)
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

	// Determine status code from the spec
	if op != nil && op.Response != nil && op.Response.SuccessCode > 0 {
		w.WriteHeader(op.Response.SuccessCode)
	}

	if resp.Body != nil {
		_, _ = w.Write(resp.Body)
	}
}

// swappableHandler wraps a handler with a mutex for hot-swapping.
type swappableHandler struct {
	mu      sync.RWMutex
	handler *handler
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

// handleRequest delegates to the current handler's handleRequest.
func (s *swappableHandler) handleRequest(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	s.handler.handleRequest(w, r)
}

func (s *swappableHandler) swap(h *handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = h
}

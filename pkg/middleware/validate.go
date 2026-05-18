package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
	validator "github.com/pb33f/libopenapi-validator"
	"github.com/pb33f/libopenapi-validator/errors"
)

// ValidatorSource yields the validator to use for the current request.
// Portable mode hot-reloads the spec; the source closes over a slot
// that the reload swaps so the middleware always picks up the validator
// built from the latest spec. Returning nil disables validation for
// this request (e.g. when the validator failed to build for the spec).
type ValidatorSource func() validator.Validator

// SpecPathLookup resolves a prefix-stripped request path and method to
// the spec path that handles it (e.g. `/users/{id}`). It's used by the
// validation middleware to detect routes that libopenapi-validator
// can't validate without panicking (see [CreateValidationMiddleware]).
// Returning ok=false means no match; the middleware falls through to
// normal validation.
type SpecPathLookup func(reqPath, method string) (specPath string, ok bool)

// CreateValidationMiddleware returns middleware that validates incoming
// requests and outgoing responses against the OpenAPI document.
//
// Request validation runs before the handler. On failure the request is
// short-circuited with a 400 and a JSON body describing the failures.
//
// Response validation runs after the handler. The response writer is
// captured so the generated payload can be validated; on failure the
// captured body is discarded and a 500 is returned. When validation
// passes, the captured response is written through unchanged.
//
// Either check is skipped when the corresponding flag in the service
// config's validation block is false. When lookup is non-nil and
// resolves the request to a spec path containing a `#` discriminator
// suffix (e.g. AWS's `/foo#qparam` convention), validation is skipped
// entirely - see the TODO inside.
func CreateValidationMiddleware(params *Params, source ValidatorSource, lookup SpecPathLookup) func(http.Handler) http.Handler {
	log := params.Logger("validation")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			v := source()
			if v == nil {
				next.ServeHTTP(w, req)
				return
			}

			cfg := params.GetServiceConfig(req)
			validateReq := cfg == nil || cfg.Validate.RequestEnabled()
			validateResp := cfg == nil || cfg.Validate.ResponseEnabled()

			// Validator works against the OpenAPI spec's path space (no
			// service mount prefix). Build a sibling request with the
			// mount stripped from URL.Path so spec lookup matches.
			validatorReq := requestForValidator(req, cfg)

			// Skip validation for spec paths libopenapi-validator can't
			// reliably look up:
			//   * `#qparam` discriminators (its FindPath returns the
			//     discriminator-suffixed key, then path-param regex
			//     iteration panics in path_parameters.go:86 because the
			//     literal `name#qparam` segment never matches the
			//     request's `name`).
			//   * spec paths with characters Go would URL-encode
			//     (spaces, etc.). FindPath compares its escaped request
			//     path against the spec's literal key, so a space in the
			//     spec becomes `%20` in the lookup and never matches.
			// In both cases mockzilla's own path matcher routes the
			// request correctly; only the validator's path lookup is
			// broken.
			// TODO: re-enable once libopenapi-validator handles these
			// shapes upstream.
			if lookup != nil {
				if specPath, ok := lookup(validatorReq.URL.Path, validatorReq.Method); ok && validatorCannotLookup(specPath) {
					next.ServeHTTP(w, req)
					return
				}
			}

			if validateReq {
				body, restore, err := snapshotBody(req)
				if err != nil {
					RequestLog(log, req).Warn("Validation: failed to read request body", "error", err)
				} else {
					req.Body = restore
					validatorReq.Body = io.NopCloser(bytes.NewReader(body))
					if ok, validationErrs := safeValidateRequest(v, validatorReq); !ok {
						if isValidatorPanic(validationErrs) {
							RequestLog(log, req).Error("Request validator panicked",
								"method", req.Method,
								"path", req.URL.Path,
								"reason", validationErrs[0].Reason)
							writeValidationError(w, http.StatusInternalServerError, "request validator panicked", validationErrs)
							return
						}
						// "Path not found in spec" is a 404 condition,
						// not a 400. The downstream handler returns its
						// own 404; skip validation and let it through.
						if !allPathMissing(validationErrs) {
							writeValidationError(w, http.StatusBadRequest, "request validation failed", validationErrs)
							return
						}
					}
					req.Body = io.NopCloser(bytes.NewReader(body))
				}
			}

			if !validateResp {
				next.ServeHTTP(w, req)
				return
			}

			rw := &responseWriter{
				ResponseWriter: w,
				body:           new(bytes.Buffer),
				statusCode:     http.StatusOK,
			}
			next.ServeHTTP(rw, req)

			// Only validate successful responses. Non-2xx are typically
			// generated by middleware (error injection, upstream errors)
			// and their schemas often aren't declared in the spec.
			if rw.statusCode < 200 || rw.statusCode >= 300 {
				writeThrough(w, rw)
				return
			}

			resp := &http.Response{
				StatusCode: rw.statusCode,
				Header:     rw.Header().Clone(),
				Body:       io.NopCloser(bytes.NewReader(rw.body.Bytes())),
			}

			ok, validationErrs := safeValidateResponse(v, validatorReq, resp)
			if ok {
				writeThrough(w, rw)
				return
			}

			if isValidatorPanic(validationErrs) {
				RequestLog(log, req).Error("Response validator panicked",
					"method", req.Method,
					"path", req.URL.Path,
					"reason", validationErrs[0].Reason)
				writeValidationError(w, http.StatusInternalServerError, "response validator panicked", validationErrs)
				return
			}

			// Schema-render failures (circular $ref chains the validator
			// can't unroll) are libopenapi-validator limitations, not
			// generator bugs. Log and let the response through; our
			// generator handles recursion.
			if allSchemaRenderFailure(validationErrs) {
				RequestLog(log, req).Warn("Validator schema render failed; skipping response validation",
					"method", req.Method,
					"path", req.URL.Path,
					"errors", len(validationErrs))
				writeThrough(w, rw)
				return
			}

			if allAmbiguousOneOf(validationErrs) {
				RequestLog(log, req).Warn("oneOf variants overlap; skipping response validation",
					"method", req.Method,
					"path", req.URL.Path,
					"errors", len(validationErrs))
				writeThrough(w, rw)
				return
			}

			RequestLog(log, req).Warn("Response validation failed",
				"method", req.Method,
				"path", req.URL.Path,
				"errors", len(validationErrs))
			writeValidationError(w, http.StatusInternalServerError, "response validation failed", validationErrs)
		})
	}
}

// safeValidateRequest wraps validator.ValidateHttpRequestSync in a
// recover so a panic inside libopenapi-validator does not take down the
// request handler. A recovered panic is reported as a validation
// failure with a synthetic ValidationError describing the panic (and
// the captured stack), so the caller can short-circuit with a 4xx/5xx
// the same way it would for a real failure, and integration-test
// output gets a useful pointer to where libopenapi-validator blew up.
func safeValidateRequest(v validator.Validator, req *http.Request) (ok bool, errs []*errors.ValidationError) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
			errs = []*errors.ValidationError{panicValidationError(r, debug.Stack(), req, "request")}
		}
	}()
	return v.ValidateHttpRequestSync(req)
}

// safeValidateResponse is the response-side counterpart to
// safeValidateRequest. See that function's doc for the panic-handling
// rationale.
func safeValidateResponse(v validator.Validator, req *http.Request, resp *http.Response) (ok bool, errs []*errors.ValidationError) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
			errs = []*errors.ValidationError{panicValidationError(r, debug.Stack(), req, "response")}
		}
	}()
	return v.ValidateHttpResponse(req, resp)
}

func panicValidationError(r any, stack []byte, req *http.Request, kind string) *errors.ValidationError {
	return &errors.ValidationError{
		Message:           "validator panicked during " + kind + " validation",
		Reason:            fmt.Sprintf("%v\n%s", r, stack),
		ValidationType:    "panic",
		ValidationSubType: kind,
		RequestPath:       req.URL.Path,
		RequestMethod:     req.Method,
	}
}

func isValidatorPanic(errs []*errors.ValidationError) bool {
	for _, e := range errs {
		if e.ValidationType == "panic" {
			return true
		}
	}
	return false
}

// requestForValidator returns a shallow clone of req with URL.Path
// rewritten to the spec-relative path (i.e. with the service mount
// prefix stripped). libopenapi-validator looks up paths in the spec
// verbatim, so a request to `/foo/bar/pets` against a spec that
// declares `/pets` mounted at `/foo/bar` must reach the validator as
// `/pets`. When cfg is nil or no prefix is configured, the request is
// returned with only URL and Body cloned (the body is rewritten by the
// caller).
func requestForValidator(req *http.Request, cfg *config.ServiceConfig) *http.Request {
	clone := req.Clone(req.Context())
	if cfg == nil {
		return clone
	}

	prefix := servicePrefix(cfg)
	if prefix == "" || prefix == "/" {
		return clone
	}

	cloneURL := *req.URL
	cloneURL.Path = stripPrefix(cloneURL.Path, prefix)
	if cloneURL.RawPath != "" {
		cloneURL.RawPath = stripPrefix(cloneURL.RawPath, prefix)
	}

	clone.URL = &cloneURL
	clone.RequestURI = ""
	return clone
}

// servicePrefix mirrors api.ServicePrefix without pulling in pkg/api
// (which would be a cyclic import). Logic must stay in lockstep.
func servicePrefix(cfg *config.ServiceConfig) string {
	if cfg.Mount != "" {
		if strings.HasPrefix(cfg.Mount, "/") {
			return cfg.Mount
		}
		return "/" + cfg.Mount
	}
	if cfg.Name == "" {
		return "/"
	}
	return "/" + cfg.Name
}

func stripPrefix(p, prefix string) string {
	p = strings.TrimSuffix(p, "/")
	prefix = strings.TrimSuffix(prefix, "/")
	if !strings.HasPrefix(p, prefix) {
		return p
	}
	stripped := strings.TrimPrefix(p, prefix)
	if stripped == "" {
		return "/"
	}
	if !strings.HasPrefix(stripped, "/") {
		// Prefix matched mid-segment; not a real mount hit. Leave path alone.
		return p
	}
	return stripped
}

// validatorCannotLookup reports whether libopenapi-validator's path
// matching is known to mishandle this spec path. The middleware
// short-circuits validation when this returns true; mockzilla's own
// path matcher is the source of truth for these cases.
func validatorCannotLookup(specPath string) bool {
	if strings.Contains(specPath, "#") {
		return true
	}

	// FindPath compares URL.EscapedPath() against the spec key. If a
	// literal segment contains a character Go would percent-encode
	// (spaces, reserved punctuation), the escaped form will never
	// match the literal spec key. Detect via a per-segment escape
	// round-trip, skipping placeholder segments like `{id}` whose
	// braces always need escaping but never appear in real URLs.
	for _, seg := range strings.Split(specPath, "/") {
		if seg == "" || isPlaceholderSegment(seg) {
			continue
		}
		if url.PathEscape(seg) != seg {
			return true
		}
	}
	return false
}

func isPlaceholderSegment(seg string) bool {
	return len(seg) >= 2 && seg[0] == '{' && seg[len(seg)-1] == '}'
}

// allPathMissing reports whether every validation failure is the
// "path/operation not declared in spec" kind. When true the middleware
// hands the request to the next handler so the handler's own 404 path
// wins; surfacing a 400 here would be wrong since the request itself
// wasn't malformed - the spec simply doesn't describe that endpoint.
func allPathMissing(errs []*errors.ValidationError) bool {
	if len(errs) == 0 {
		return false
	}
	for _, e := range errs {
		if e.ValidationType != "path" || e.ValidationSubType != "missing" {
			return false
		}
	}
	return true
}

// allAmbiguousOneOf reports whether every validation failure is the
// "oneOf matched more than one subschema" case. libopenapi-validator
// surfaces this on the nested SchemaValidationErrors as `'oneOf'
// failed, subschemas X, Y matched` (in contrast to `none matched`,
// which is a real generator bug). Multiple matches mean the spec's
// oneOf variants are not mutually exclusive, usually because the
// variants don't declare `required` fields or
// `additionalProperties: false`; no mock body can satisfy strict oneOf
// semantics in that situation, so the middleware treats it as a
// warning rather than a 500.
func allAmbiguousOneOf(errs []*errors.ValidationError) bool {
	if len(errs) == 0 {
		return false
	}
	for _, e := range errs {
		if len(e.SchemaValidationErrors) == 0 {
			return false
		}
		for _, sve := range e.SchemaValidationErrors {
			if !isAmbiguousOneOfReason(sve.Reason) {
				return false
			}
		}
	}
	return true
}

func isAmbiguousOneOfReason(reason string) bool {
	return strings.Contains(reason, "'oneOf' failed") &&
		strings.Contains(reason, "matched") &&
		!strings.Contains(reason, "none matched")
}

// allSchemaRenderFailure reports whether every validation failure is a
// libopenapi-validator schema-rendering failure (typically caused by
// circular $ref chains the validator can't unroll). These aren't
// generator bugs — our mock body may well be fine — they're validator
// limitations. The middleware treats them as "validation skipped" rather
// than surfacing them as 500s to the client.
func allSchemaRenderFailure(errs []*errors.ValidationError) bool {
	if len(errs) == 0 {
		return false
	}
	for _, e := range errs {
		if !strings.Contains(e.Reason, "schema render failure") &&
			!strings.Contains(e.Message, "failed schema rendering") {
			return false
		}
	}
	return true
}

// snapshotBody reads the request body into memory and returns a fresh
// reader so the request can be validated and then handed unchanged to
// the next handler. Returns (nil, nil, nil) for empty bodies.
func snapshotBody(req *http.Request) ([]byte, io.ReadCloser, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, http.NoBody, nil
	}
	body, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		return nil, nil, err
	}
	return body, io.NopCloser(bytes.NewReader(body)), nil
}

// validationErrorPayload is the JSON shape returned to clients on a
// validation failure. Keep the surface small: a top-level message and
// a list of validation-error details from libopenapi-validator.
type validationErrorPayload struct {
	Error   string                    `json:"error"`
	Details []*errors.ValidationError `json:"details,omitempty"`
}

func writeValidationError(w http.ResponseWriter, status int, message string, validationErrs []*errors.ValidationError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(validationErrorPayload{
		Error:   message,
		Details: validationErrs,
	})
}

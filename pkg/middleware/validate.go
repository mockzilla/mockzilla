package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
	validator "github.com/pb33f/libopenapi-validator"
	"github.com/pb33f/libopenapi-validator/errors"
	"go.yaml.in/yaml/v4"
)

// ValidatorSource yields the validator for the current request; nil disables validation.
// Portable mode uses this to hot-swap validators after a spec reload.
type ValidatorSource func() validator.Validator

// SpecPathLookup resolves a prefix-stripped request path/method to the spec path.
type SpecPathLookup func(reqPath, method string) (specPath string, ok bool)

// CreateValidationMiddleware validates requests/responses against the OpenAPI document.
// Request failures return 400; response failures return 500. When lookup flags a route
// that libopenapi-validator can't safely handle, validation is skipped.
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

			validatorReq := requestForValidator(req, cfg)

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
						switch {
						case isValidatorPanic(validationErrs):
							RequestLog(log, req).Warn("Request validator panicked; skipping request validation",
								"method", req.Method,
								"path", req.URL.Path,
								"reason", validationErrs[0].Reason)
						case allPathMissing(validationErrs):
							// 404 is the downstream handler's job, not ours.
						default:
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

			// Skip non-2xx: error responses often aren't declared in the spec.
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
				RequestLog(log, req).Warn("Response validator panicked; skipping response validation",
					"method", req.Method,
					"path", req.URL.Path,
					"reason", validationErrs[0].Reason)
				writeThrough(w, rw)
				return
			}

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

			if allPathMissing(validationErrs) {
				RequestLog(log, req).Warn("Spec path not found by validator (likely server base-path mismatch); skipping response validation",
					"method", req.Method,
					"path", req.URL.Path,
					"errors", len(validationErrs))
				writeThrough(w, rw)
				return
			}

			if allJSLiteralPattern(validationErrs) {
				RequestLog(log, req).Warn("Spec pattern uses JS regex literal `/.../`; skipping response validation",
					"method", req.Method,
					"path", req.URL.Path,
					"errors", len(validationErrs))
				writeThrough(w, rw)
				return
			}

			if allDescriptivePattern(validationErrs) {
				RequestLog(log, req).Warn("Spec pattern is prose, not regex; skipping response validation",
					"method", req.Method,
					"path", req.URL.Path,
					"errors", len(validationErrs))
				writeThrough(w, rw)
				return
			}

			if allUnsatisfiableSchema(validationErrs) {
				RequestLog(log, req).Warn("Spec schema is unsatisfiable (required name missing from properties + additionalProperties:false); skipping response validation",
					"method", req.Method,
					"path", req.URL.Path,
					"errors", len(validationErrs))
				writeThrough(w, rw)
				return
			}

			if allContentTypeParamsOnly(validationErrs) {
				RequestLog(log, req).Warn("Spec content type only differs by media-type parameters; skipping response validation",
					"method", req.Method,
					"path", req.URL.Path,
					"errors", len(validationErrs))
				writeThrough(w, rw)
				return
			}

			if allWildcardContentType(validationErrs) {
				RequestLog(log, req).Warn("Spec declares wildcard `*/*` content type; skipping response validation",
					"method", req.Method,
					"path", req.URL.Path,
					"errors", len(validationErrs))
				writeThrough(w, rw)
				return
			}

			if allStatusCodeNotDeclared(validationErrs) {
				RequestLog(log, req).Warn("Response status code not declared in spec; skipping response validation",
					"method", req.Method,
					"path", req.URL.Path,
					"status", rw.statusCode,
					"errors", len(validationErrs))
				writeThrough(w, rw)
				return
			}

			if allRouterAmbiguity(validationErrs, resp.Header.Get("Content-Type")) {
				RequestLog(log, req).Warn("libopenapi-validator matched a different spec path than the router; skipping response validation",
					"method", req.Method,
					"path", req.URL.Path,
					"errors", len(validationErrs))
				writeThrough(w, rw)
				return
			}

			if lookup != nil {
				if chiSpecPath, ok := lookup(validatorReq.URL.Path, validatorReq.Method); ok && allErrorsForDifferentSpecPath(validationErrs, chiSpecPath) {
					RequestLog(log, req).Warn("libopenapi-validator matched a different spec path than the router; skipping response validation",
						"method", req.Method,
						"path", req.URL.Path,
						"chiSpec", chiSpecPath,
						"errors", len(validationErrs))
					writeThrough(w, rw)
					return
				}
			}

			RequestLog(log, req).Warn("Response validation failed",
				"method", req.Method,
				"path", req.URL.Path,
				"errors", len(validationErrs))
			writeValidationError(w, http.StatusInternalServerError, "response validation failed", validationErrs)
		})
	}
}

// safeValidateRequest recovers panics from libopenapi-validator as a synthetic
// ValidationError so the handler stays up.
func safeValidateRequest(v validator.Validator, req *http.Request) (ok bool, errs []*errors.ValidationError) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
			errs = []*errors.ValidationError{panicValidationError(r, debug.Stack(), req, "request")}
		}
	}()
	return v.ValidateHttpRequestSync(req)
}

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

// requestForValidator clones req with the service mount prefix stripped from URL.Path
// so libopenapi-validator's verbatim path lookup matches spec's mount-relative paths.
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

// servicePrefix mirrors api.ServicePrefix; duplicated here to avoid a cyclic import.
// Keep the two in lockstep.
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

// validatorCannotLookup reports paths libopenapi-validator's FindPath mishandles:
// `#` discriminators panic in its path-param iteration; segments containing chars
// Go's URL escaping alters (spaces, etc.) never match its EscapedPath()-based lookup.
func validatorCannotLookup(specPath string) bool {
	if strings.Contains(specPath, "#") {
		return true
	}
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
	if len(seg) < 2 || seg[0] != '{' || seg[len(seg)-1] != '}' {
		return false
	}
	return strings.Count(seg, "{") == 1 && strings.Count(seg, "}") == 1
}

// allPathMissing reports "path/operation not declared" failures; the middleware
// then defers to the handler's own 404 instead of returning 400.
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

// allContentTypeParamsOnly reports content-type failures that match once media-type
// parameters are stripped from both sides. libopenapi-validator strips them from the
// response but not from the spec key.
func allContentTypeParamsOnly(errs []*errors.ValidationError) bool {
	if len(errs) == 0 {
		return false
	}
	for _, e := range errs {
		if e.ValidationType != "response" || e.ValidationSubType != "contentType" {
			return false
		}
		actual := extractQuotedToken(e.Message)
		spec := extractAfter(e.HowToFix, "supported types for this operation: ")
		if actual == "" || spec == "" {
			return false
		}
		actualMT, _, _ := mime.ParseMediaType(actual)
		specMT, _, _ := mime.ParseMediaType(spec)
		if actualMT == "" || specMT == "" || actualMT != specMT {
			return false
		}
	}
	return true
}

func extractQuotedToken(s string) string {
	start := strings.IndexByte(s, '\'')
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(s[start+1:], '\'')
	if end < 0 {
		return ""
	}
	return s[start+1 : start+1+end]
}

func extractAfter(s, marker string) string {
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(s[i+len(marker):])
}

// allErrorsForDifferentSpecPath reports failures anchored to a spec path other than
// the router-matched one. The router's match is authoritative; the validator's path
// resolution can diverge when multiple templates match (literal vs templated suffix).
func allErrorsForDifferentSpecPath(errs []*errors.ValidationError, chiSpecPath string) bool {
	if len(errs) == 0 || chiSpecPath == "" {
		return false
	}
	for _, e := range errs {
		if e.SpecPath == "" || e.SpecPath == chiSpecPath {
			return false
		}
	}
	return true
}

// allRouterAmbiguity reports content-type failures where chi picked a literal route
// but libopenapi-validator picked a sibling templated route. Heuristic: response
// Content-Type isn't application/json (the codegen default) and every error is a
// content-type failure; we'd never emit non-JSON if we'd routed to the JSON path.
func allRouterAmbiguity(errs []*errors.ValidationError, respCT string) bool {
	if len(errs) == 0 || respCT == "" {
		return false
	}
	respMT, _, _ := mime.ParseMediaType(respCT)
	if respMT == "" || strings.EqualFold(respMT, "application/json") {
		return false
	}
	for _, e := range errs {
		if e.ValidationType != "response" || e.ValidationSubType != "contentType" {
			return false
		}
	}
	return true
}

// allWildcardContentType reports content-type failures where the spec only declares
// `*/*`; the validator literal-matches and rejects every concrete type.
func allWildcardContentType(errs []*errors.ValidationError) bool {
	if len(errs) == 0 {
		return false
	}
	for _, e := range errs {
		if e.ValidationType != "response" || e.ValidationSubType != "contentType" {
			return false
		}
		if !strings.Contains(e.HowToFix, "*/*") {
			return false
		}
	}
	return true
}

// allStatusCodeNotDeclared reports failures where the response's status code isn't
// declared in the operation and the validator won't use a content-less `default` as
// a catch-all, so no status we pick would pass.
func allStatusCodeNotDeclared(errs []*errors.ValidationError) bool {
	if len(errs) == 0 {
		return false
	}
	for _, e := range errs {
		if e.ValidationType != "response" || e.ValidationSubType != "statusCode" {
			return false
		}
	}
	return true
}

// allJSLiteralPattern reports pattern-mismatch failures where the spec wrote the
// regex as a JS literal `/.../`; the slashes get compiled into the pattern.
func allJSLiteralPattern(errs []*errors.ValidationError) bool {
	if len(errs) == 0 {
		return false
	}
	for _, e := range errs {
		if len(e.SchemaValidationErrors) == 0 {
			return false
		}
		for _, sve := range e.SchemaValidationErrors {
			if !reasonIsJSLiteralPattern(sve.Reason) {
				return false
			}
		}
	}
	return true
}

func reasonIsJSLiteralPattern(reason string) bool {
	const marker = "does not match pattern '"
	i := strings.Index(reason, marker)
	if i < 0 {
		return false
	}
	rest := reason[i+len(marker):]
	end := strings.IndexByte(rest, '\'')
	if end < 0 {
		return false
	}
	pattern := rest[:end]
	return len(pattern) >= 2 && pattern[0] == '/' && pattern[len(pattern)-1] == '/'
}

func allDescriptivePattern(errs []*errors.ValidationError) bool {
	if len(errs) == 0 {
		return false
	}
	for _, e := range errs {
		if len(e.SchemaValidationErrors) == 0 {
			return false
		}
		for _, sve := range e.SchemaValidationErrors {
			if !reasonIsDescriptivePattern(sve.Reason) {
				return false
			}
		}
	}
	return true
}

// reasonIsDescriptivePattern flags `pattern:` values written as prose, not regex
// (e.g. `a-z, A-Z, 0-9, /, _, -`). Comma-space isn't a quantifier and isn't legal
// elsewhere in a typical OpenAPI regex, so it's a reliable tell.
func reasonIsDescriptivePattern(reason string) bool {
	const marker = "does not match pattern '"
	i := strings.Index(reason, marker)
	if i < 0 {
		return false
	}
	rest := reason[i+len(marker):]
	end := strings.IndexByte(rest, '\'')
	if end < 0 {
		return false
	}
	pattern := rest[:end]
	return strings.Contains(pattern, ", ")
}

// allUnsatisfiableSchema reports internally inconsistent schemas: a `required` name
// that isn't in `properties` combined with `additionalProperties: false`.
func allUnsatisfiableSchema(errs []*errors.ValidationError) bool {
	if len(errs) == 0 {
		return false
	}

	for _, e := range errs {
		if len(e.SchemaValidationErrors) == 0 {
			return false
		}
		for _, sve := range e.SchemaValidationErrors {
			if !schemaFailureIsUnsatisfiable(sve) {
				return false
			}
		}
	}
	return true
}

func schemaFailureIsUnsatisfiable(sve *errors.SchemaValidationFailure) bool {
	if sve == nil || sve.ReferenceSchema == "" {
		return false
	}

	key := extractPropertyName(sve.Reason)
	if key == "" {
		return false
	}
	var root any
	if err := yaml.Unmarshal([]byte(sve.ReferenceSchema), &root); err != nil {
		return false
	}

	target := resolveJSONPointer(root, trimKeywordSuffix(sve.KeywordLocation))
	obj, ok := target.(map[string]any)
	if !ok {
		return false
	}

	addProps, ok := obj["additionalProperties"]
	if !ok {
		return false
	}

	if b, ok := addProps.(bool); !ok || b {
		return false
	}

	required, _ := obj["required"].([]any)
	requiredHasKey := false
	for _, r := range required {
		if s, ok := r.(string); ok && s == key {
			requiredHasKey = true
			break
		}
	}
	if !requiredHasKey {
		return false
	}

	properties, _ := obj["properties"].(map[string]any)
	if _, declared := properties[key]; declared {
		return false
	}
	return true
}

// trimKeywordSuffix points at the containing schema (`.../foo/required` -> `.../foo`).
func trimKeywordSuffix(pointer string) string {
	if i := strings.LastIndex(pointer, "/"); i > 0 {
		return pointer[:i]
	}
	return ""
}

func resolveJSONPointer(root any, pointer string) any {
	if pointer == "" {
		return root
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil
	}

	cur := root
	for _, raw := range strings.Split(pointer[1:], "/") {
		token := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[token]
			if !ok {
				return nil
			}
			cur = next
		case []any:
			idx, err := strconv.Atoi(token)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil
			}
			cur = node[idx]
		default:
			return nil
		}
	}
	return cur
}

func extractPropertyName(reason string) string {
	for _, prefix := range []string{"missing property '", "additional properties '"} {
		if idx := strings.Index(reason, prefix); idx >= 0 {
			rest := reason[idx+len(prefix):]
			if end := strings.IndexByte(rest, '\''); end >= 0 {
				return rest[:end]
			}
		}
	}
	return ""
}

// allAmbiguousOneOf reports failures where `oneOf` matched more than one subschema
// (variants aren't mutually exclusive). "none matched" stays a real failure.
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

// allSchemaRenderFailure reports failures that are libopenapi-validator render or
// compile errors on the response schema itself (circular $refs it can't unroll, or
// spec defects). Treated as "validation skipped" rather than surfacing as 500.
func allSchemaRenderFailure(errs []*errors.ValidationError) bool {
	if len(errs) == 0 {
		return false
	}
	for _, e := range errs {
		switch {
		case strings.Contains(e.Reason, "schema render failure"),
			strings.Contains(e.Message, "failed schema rendering"),
			strings.Contains(e.Message, "failed schema compilation"),
			strings.Contains(e.Reason, "JSON schema compile failed"):
			continue
		default:
			return false
		}
	}
	return true
}

// snapshotBody reads req.Body and returns the bytes plus a fresh reader so the
// body can be validated and then handed on unchanged. Empty bodies return (nil, nil, nil).
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

// validationErrorPayload is the JSON shape returned on a validation failure.
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

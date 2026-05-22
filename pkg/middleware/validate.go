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
	"time"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
	validator "github.com/pb33f/libopenapi-validator"
	"github.com/pb33f/libopenapi-validator/errors"
	"go.yaml.in/yaml/v4"
)

// Default lives in pkg/config so the timeout fallback is a single
// source of truth shared with TimeoutOrDefault. Per-request cfg may
// override via validate.timeout / X-Mockzilla-Validate-Timeout.

// ValidatorSource yields the validator for the current request; nil disables validation.
// Portable mode uses this to hot-swap validators after a spec reload. The ensure flag
// asks the source to lazy-build if it hasn't already - used when a per-request
// override turns on validation for a service that booted with it off.
type ValidatorSource func(ensure bool) validator.Validator

// SpecPathLookup resolves a prefix-stripped request path/method to the spec path.
type SpecPathLookup func(reqPath, method string) (specPath string, ok bool)

// CreateValidationMiddleware validates requests/responses against the OpenAPI document.
// Request failures return 400; response failures return 500. When lookup flags a route
// that libopenapi-validator can't safely handle, validation is skipped.
func CreateValidationMiddleware(params *Params, source ValidatorSource, lookup SpecPathLookup) func(http.Handler) http.Handler {
	log := params.Logger("validation")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			cfg := params.GetServiceConfig(req)
			validateReq := cfg != nil && cfg.Validate.RequestEnabled()
			validateResp := cfg != nil && cfg.Validate.ResponseEnabled()

			// Cheap no-op path: neither side wants validation, so skip
			// the validator fetch entirely. Keeps the middleware free
			// for services that boot with validation disabled and
			// never see an override header.
			if !validateReq && !validateResp {
				next.ServeHTTP(w, req)
				return
			}

			v := source(true)
			if v == nil {
				next.ServeHTTP(w, req)
				return
			}

			validatorReq := requestForValidator(req, cfg)
			validationTimeout := cfg.Validate.TimeoutOrDefault()

			if requestPathHasEmptySegments(validatorReq.URL.Path) {
				next.ServeHTTP(w, req)
				return
			}

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
					if ok, validationErrs := safeValidateRequest(v, validatorReq, validationTimeout); !ok {
						switch {
						case isValidatorPanic(validationErrs):
							RequestLog(log, req).Warn("Request validator panicked; skipping request validation",
								"method", req.Method,
								"path", req.URL.Path,
								"reason", validationErrs[0].Reason)
						case isValidatorTimeout(validationErrs):
							RequestLog(log, req).Warn("Request validator exceeded timeout; skipping request validation",
								"method", req.Method,
								"path", req.URL.Path,
								"timeout", validationTimeout)
						case allPathMissing(validationErrs):
							// 404 is the downstream handler's job, not ours.
						default:
							writeValidationError(w, http.StatusBadRequest, "request validation failed", validationErrs, cfg.Validate.VerboseEnabled())
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

			ok, validationErrs := safeValidateResponse(v, validatorReq, resp, validationTimeout)
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

			if isValidatorTimeout(validationErrs) {
				RequestLog(log, req).Warn("Response validator exceeded timeout; skipping response validation",
					"method", req.Method,
					"path", req.URL.Path,
					"timeout", validationTimeout)
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

			if allConflictingAllOfTypes(validationErrs) {
				RequestLog(log, req).Warn("Spec allOf has conflicting branch types (e.g. allOf: [{type:object},{type:array}]); skipping response validation",
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
			writeValidationError(w, http.StatusInternalServerError, "response validation failed", validationErrs, cfg.Validate.VerboseEnabled())
		})
	}
}

// safeValidateRequest recovers panics from libopenapi-validator as a synthetic
// ValidationError so the handler stays up.
func safeValidateRequest(v validator.Validator, req *http.Request, timeout time.Duration) (ok bool, errs []*errors.ValidationError) {
	type result struct {
		ok   bool
		errs []*errors.ValidationError
	}

	done := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- result{false, []*errors.ValidationError{panicValidationError(r, debug.Stack(), req, "request")}}
			}
		}()
		o, e := v.ValidateHttpRequestSync(req)
		done <- result{o, e}
	}()

	select {
	case r := <-done:
		return r.ok, r.errs
	case <-time.After(timeout):
		return false, []*errors.ValidationError{timeoutValidationError(req, "request", timeout)}
	}
}

func safeValidateResponse(v validator.Validator, req *http.Request, resp *http.Response, timeout time.Duration) (ok bool, errs []*errors.ValidationError) {
	type result struct {
		ok   bool
		errs []*errors.ValidationError
	}
	done := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- result{false, []*errors.ValidationError{panicValidationError(r, debug.Stack(), req, "response")}}
			}
		}()
		o, e := v.ValidateHttpResponse(req, resp)
		done <- result{o, e}
	}()
	select {
	case r := <-done:
		return r.ok, r.errs
	case <-time.After(timeout):
		return false, []*errors.ValidationError{timeoutValidationError(req, "response", timeout)}
	}
}

func timeoutValidationError(req *http.Request, kind string, timeout time.Duration) *errors.ValidationError {
	return &errors.ValidationError{
		Message:           "validator exceeded " + timeout.String() + " during " + kind + " validation",
		Reason:            "schema rendering or evaluation took too long (likely a pathologically recursive composition)",
		ValidationType:    "timeout",
		ValidationSubType: kind,
		RequestPath:       req.URL.Path,
		RequestMethod:     req.Method,
	}
}

func isValidatorTimeout(errs []*errors.ValidationError) bool {
	for _, e := range errs {
		if e.ValidationType == "timeout" {
			return true
		}
	}
	return false
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

// requestPathHasEmptySegments reports paths that contain consecutive `/`
// anywhere. libopenapi-validator's path matcher splits on `/` and
// indexes the resulting segments without guarding the empty-string
// case, so an empty segment crashes its preflight before our
// safeValidate* recover can catch the panic.
func requestPathHasEmptySegments(p string) bool {
	return strings.Contains(p, "//")
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

// allConflictingAllOfTypes reports failures where every error is a type
// mismatch against an `allOf` branch whose siblings declare different
// scalar types - the canonical unsatisfiable shape is `allOf: [{type:
// object}, {type: array}]`. The generator can produce one or the other
// but never both, and the validator dutifully reports whichever branch
// the generated value isn't. Treated as "validation skipped" rather
// than surfacing as a 500.
func allConflictingAllOfTypes(errs []*errors.ValidationError) bool {
	if len(errs) == 0 {
		return false
	}
	for _, e := range errs {
		if len(e.SchemaValidationErrors) == 0 {
			return false
		}
		for _, sve := range e.SchemaValidationErrors {
			if sve == nil {
				return false
			}
			if !schemaFailureIsConflictingAllOfTypes(sve.ReferenceSchema, sve.KeywordLocation, sve.Reason) {
				return false
			}
		}
	}
	return true
}

func schemaFailureIsConflictingAllOfTypes(referenceSchema, keywordLocation, reason string) bool {
	if referenceSchema == "" {
		return false
	}
	if !strings.Contains(reason, "want ") || !strings.Contains(reason, "got ") {
		return false
	}

	// Only confident when the failing keyword is the branch's `type`;
	// other allOf failures (required, schema, etc.) have their own
	// dedicated heuristics or warrant real diagnostic output.
	if !strings.HasSuffix(keywordLocation, "/type") {
		return false
	}

	var root any
	if err := yaml.Unmarshal([]byte(referenceSchema), &root); err != nil {
		return false
	}
	return containsAllOfTypeConflict(root)
}

// containsAllOfTypeConflict walks a parsed schema and returns true when
// any `allOf` branch list contains two or more branches that declare
// different non-empty scalar `type` values. Walks both objects and
// arrays so allOf nested under properties/items/additionalProperties is
// reached.
func containsAllOfTypeConflict(node any) bool {
	switch v := node.(type) {
	case map[string]any:
		if allOf, ok := v["allOf"].([]any); ok {
			seen := map[string]bool{}
			for _, branch := range allOf {
				b, ok := branch.(map[string]any)
				if !ok {
					continue
				}

				switch t := b["type"].(type) {
				case string:
					if t != "" && t != "null" {
						seen[t] = true
					}
				case []any:
					for _, x := range t {
						if s, ok := x.(string); ok && s != "" && s != "null" {
							seen[s] = true
						}
					}
				}
			}
			if len(seen) > 1 {
				return true
			}
		}

		for _, child := range v {
			if containsAllOfTypeConflict(child) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if containsAllOfTypeConflict(item) {
				return true
			}
		}
	}
	return false
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

	// Two acceptable shapes:
	//   1. every SVE is itself an ambiguous-oneOf reason - sibling
	//      fields both have ambiguous oneOfs at unrelated paths.
	//   2. at least one SVE is ambiguous AND all SVEs share a prefix
	//      through /anyOf or /oneOf - parent ambiguity plus child
	//      explanations from the same composition chain.
	// Distinguishes "swallowable ambiguity" from "ambiguous + an
	// unrelated real failure".
	for _, e := range errs {
		if len(e.SchemaValidationErrors) == 0 {
			return false
		}
		if allAmbiguousOneOfSVE(e.SchemaValidationErrors) {
			continue
		}
		if !anyAmbiguousOneOf(e.SchemaValidationErrors) {
			return false
		}
		if !errorsShareCompositionRoot(e.SchemaValidationErrors) {
			return false
		}
	}

	return true
}

func allAmbiguousOneOfSVE(sves []*errors.SchemaValidationFailure) bool {
	for _, sve := range sves {
		if !isAmbiguousOneOfReason(sve.Reason) {
			return false
		}
	}
	return true
}

func anyAmbiguousOneOf(sves []*errors.SchemaValidationFailure) bool {
	for _, sve := range sves {
		if isAmbiguousOneOfReason(sve.Reason) {
			return true
		}
	}
	return false
}

func errorsShareCompositionRoot(sves []*errors.SchemaValidationFailure) bool {
	if len(sves) <= 1 {
		return true
	}

	prefix := sves[0].KeywordLocation
	for _, sve := range sves[1:] {
		prefix = commonPathPrefix(prefix, sve.KeywordLocation)
		if prefix == "" {
			return false
		}
	}

	return strings.Contains(prefix, "/anyOf") || strings.Contains(prefix, "/oneOf")
}

func commonPathPrefix(a, b string) string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}

	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return a[:i]
		}
	}
	return a[:n]
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

func writeValidationError(w http.ResponseWriter, status int, message string, validationErrs []*errors.ValidationError, verbose bool) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if verbose {
		_ = json.NewEncoder(w).Encode(validationErrorPayload{
			Error:   message,
			Details: validationErrs,
		})
		return
	}
	_ = json.NewEncoder(w).Encode(slimValidationErrorPayload{
		Error:   message,
		Details: slimValidationErrors(validationErrs),
	})
}

// Slim payload shape returned in non-verbose mode. Drops every
// libopenapi-validator-supplied envelope field (message, howToFix,
// validationType/SubType, requestPath, specPath, requestMethod, line/col,
// parameterName) and keeps only the per-failure reason plus the nested
// schema-validation details that name what actually broke. Verbose mode
// keeps the full envelope for debugging.
type slimValidationErrorPayload struct {
	Error   string               `json:"error"`
	Details []slimValidationItem `json:"details,omitempty"`
}

type slimValidationItem struct {
	Reason           string                            `json:"reason,omitempty"`
	ValidationErrors []*errors.SchemaValidationFailure `json:"validationErrors,omitempty"`
}

// slimValidationErrors maps each ValidationError to its slim form:
// reason + per-failure list with ReferenceSchema/ReferenceObject blanked
// on every SchemaValidationFailure. Those two fields are the full
// offending schema YAML and the entire submitted payload - invaluable
// for debugging but easily megabytes per response. Returns a fresh
// slice so the caller's *ValidationError pointers are safe to log
// elsewhere with full detail.
func slimValidationErrors(in []*errors.ValidationError) []slimValidationItem {
	if len(in) == 0 {
		return nil
	}
	out := make([]slimValidationItem, len(in))
	for i, ve := range in {
		if ve == nil {
			continue
		}
		item := slimValidationItem{Reason: ve.Reason}
		if len(ve.SchemaValidationErrors) > 0 {
			sves := make([]*errors.SchemaValidationFailure, len(ve.SchemaValidationErrors))
			for j, sve := range ve.SchemaValidationErrors {
				if sve == nil {
					continue
				}
				sveClone := *sve
				sveClone.ReferenceSchema = ""
				sveClone.ReferenceObject = ""
				sves[j] = &sveClone
			}
			item.ValidationErrors = sves
		}
		out[i] = item
	}
	return out
}

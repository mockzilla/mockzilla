package factory

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/mockzilla/mockzilla/v2/pkg/api"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/generator"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/mockzilla/mockzilla/v2/pkg/typedef"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// Factory generates mock requests and responses based on an OpenAPI spec.
// It wraps the registry and generator for convenient programmatic use.
type Factory struct {
	registry typedef.OperationRegistry
	gen      generator.Generate
	matcher  *pathMatcher

	specBytes []byte
	docOnce   sync.Once
	doc       libopenapi.Document
	docErr    error
	logger    *slog.Logger

	declaredCodes sync.Map // operationKey ("PATH:METHOD") -> map[int]struct{}
}

type factoryConfig struct {
	serviceContext []byte
	specOptions    *config.SpecOptions
	logger         *slog.Logger
}

// FactoryOption configures a Factory.
type FactoryOption func(*factoryConfig)

// WithServiceContext sets a service-specific context YAML for value replacements.
func WithServiceContext(contextYAML []byte) FactoryOption {
	return func(c *factoryConfig) {
		c.serviceContext = contextYAML
	}
}

// WithSpecOptions sets OpenAPI spec parsing options.
func WithSpecOptions(opts *config.SpecOptions) FactoryOption {
	return func(c *factoryConfig) {
		c.specOptions = opts
	}
}

// WithLogger sets the slog.Logger used when constructing the libopenapi
// Document. Without this option, libopenapi falls back to its built-in
// JSON-stdout logger that bypasses slog.Default; supply the desired
// logger here so spec-parse warnings flow through the host process's
// logging configuration.
func WithLogger(l *slog.Logger) FactoryOption {
	return func(c *factoryConfig) {
		c.logger = l
	}
}

type declaredResponseInfo struct {
	codes      map[int]struct{}
	hasDefault bool
}

// NewFactory creates a Factory from raw OpenAPI spec bytes.
// Default replacement contexts (common, fake, words) are loaded automatically.
func NewFactory(specBytes []byte, opts ...FactoryOption) (*Factory, error) {
	fc := &factoryConfig{}
	for _, opt := range opts {
		opt(fc)
	}

	registry, err := typedef.NewRegistry(specBytes, typedef.RegistryOptions{
		SpecOptions: fc.specOptions,
		Logger:      fc.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("creating registry: %w", err)
	}

	defaultContexts := generator.LoadDefaultContexts()
	orderedCtx := generator.LoadServiceContext(fc.serviceContext, defaultContexts)
	gen, err := generator.NewGenerator(orderedCtx, defaultContexts)
	if err != nil {
		return nil, fmt.Errorf("creating generator: %w", err)
	}

	matcher := newPathMatcher(registry.GetRouteInfo())

	return &Factory{
		registry:  registry,
		gen:       gen,
		matcher:   matcher,
		specBytes: specBytes,
		logger:    fc.logger,
	}, nil
}

// Document returns the parsed libopenapi document, building it lazily on
// first call. Callers that need access to the raw spec model (validators,
// inspectors) use this to avoid re-parsing.
//
// When a logger was provided via WithLogger, libopenapi is constructed
// with a DocumentConfiguration that routes its spec-parse warnings
// through that logger; otherwise libopenapi falls back to its default
// JSON-stdout logger, which bypasses slog.Default and produces output
// the host process can't easily silence or attribute.
func (f *Factory) Document() (libopenapi.Document, error) {
	f.docOnce.Do(func() {
		if dp, ok := f.registry.(typedef.DocumentProvider); ok {
			// A registry that released its document falls through and re-parses.
			if doc, err := dp.Document(); err == nil && doc != nil {
				f.doc = doc
				return
			}
		}
		if f.logger != nil {
			cfg := datamodel.NewDocumentConfiguration()
			cfg.Logger = f.logger
			f.doc, f.docErr = libopenapi.NewDocumentWithConfiguration(f.specBytes, cfg)
			return
		}
		f.doc, f.docErr = libopenapi.NewDocument(f.specBytes)
	})
	return f.doc, f.docErr
}

// SuccessStatusCode returns the HTTP status code to use for a successful
// response. It mirrors op.Response.SuccessCode when that code is
// actually declared in the spec, otherwise picks a code the spec does
// declare so response validation can match against a real response
// schema. The registry fabricates a 204 entry when no 2xx is declared
// (OAuth redirects, deletes-with-only-error-responses, "default"-only
// operations); returning that synthetic 204 would fail validation, so
// this method falls back as follows:
//
//  1. SuccessCode declared in spec: keep it.
//  2. Lowest declared 2xx, then 3xx, then 4xx.
//  3. "default" declared: 200 (preferred over 5xx so the happy path stays green).
//  4. Lowest declared 5xx.
//  5. Nothing available: SuccessCode unchanged.
func (f *Factory) SuccessStatusCode(path, method string) int {
	op := f.registry.FindOperation(path, method)
	if op == nil || op.Response == nil {
		return 200
	}
	regCode := op.Response.SuccessCode
	codes, hasDefault := f.declaredResponses(path, method)
	if _, ok := codes[regCode]; ok {
		return regCode
	}

	keys := make([]int, 0, len(codes))
	for code := range codes {
		keys = append(keys, code)
	}
	min2xx, min3xx, min4xx, min5xx := schema.MinResponseCodes(keys)

	switch {
	case min2xx > 0:
		return min2xx
	case min3xx > 0:
		return min3xx
	case min4xx > 0:
		return min4xx
	case hasDefault:
		return 200
	case min5xx > 0:
		return min5xx
	}
	return regCode
}

// Response generates a mock response for the given spec path and method.
// path should be the OpenAPI path pattern (e.g., "/users/{id}").
// ctx is an optional replacement context for controlling generated values.
// Pass generator.WithRequest to let `request:` context values read the incoming request.
func (f *Factory) Response(path, method string, ctx map[string]any, opts ...generator.ResponseOption) (schema.ResponseData, error) {
	respSchema := f.registry.GetResponseSchema(path, method)
	if respSchema == nil {
		return schema.ResponseData{}, fmt.Errorf("no operation found for %s %s", method, path)
	}
	return f.gen.Response(respSchema, ctx, opts...), nil
}

// Request generates a mock request for the given spec path and method.
// Returns a GeneratedRequest with path (param values filled), contentType, headers, and body.
// ctx is an optional replacement context for controlling generated values.
func (f *Factory) Request(path, method string, ctx map[string]any) (schema.GeneratedRequest, error) {
	op := f.registry.FindOperation(path, method)
	if op == nil {
		return schema.GeneratedRequest{}, fmt.Errorf("no operation found for %s %s", method, path)
	}
	req := &api.GenerateRequest{
		Path:   path,
		Method: method,
	}
	raw := f.gen.Request(req, op, ctx)
	if raw == nil {
		return schema.GeneratedRequest{}, fmt.Errorf("failed to generate request for %s %s", method, path)
	}
	var result schema.GeneratedRequest
	if err := json.Unmarshal(raw, &result); err != nil {
		return schema.GeneratedRequest{}, fmt.Errorf("unmarshalling generated request: %w", err)
	}
	return result, nil
}

// ResponseFromRequest generates a mock response matching the given HTTP request.
// It automatically matches the request path (e.g., /users/42) to the corresponding
// spec path pattern (e.g., /users/{id}) and generates a response.
// ctx is an optional replacement context for controlling generated values.
func (f *Factory) ResponseFromRequest(r *http.Request, ctx map[string]any) (schema.ResponseData, error) {
	specPath, ok := f.matcher.Match(r.URL.Path, r.Method)
	if !ok {
		return schema.ResponseData{}, fmt.Errorf("no matching operation for %s %s", r.Method, r.URL.Path)
	}
	return f.Response(specPath, r.Method, ctx, generator.WithRequest(r))
}

// ResponseBody generates just the response body bytes for the given spec path and method.
// ctx is an optional replacement context for controlling generated values.
func (f *Factory) ResponseBody(path, method string, ctx map[string]any) (json.RawMessage, error) {
	resp, err := f.Response(path, method, ctx)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// RequestBody generates just the request body bytes for the given spec path and method.
// ctx is an optional replacement context for controlling generated values.
func (f *Factory) RequestBody(path, method string, ctx map[string]any) (json.RawMessage, error) {
	req, err := f.Request(path, method, ctx)
	if err != nil {
		return nil, err
	}
	return req.Body, nil
}

// ResponseBodyFromRequest generates response body bytes matching the given HTTP request.
// It automatically matches the request path (e.g., /users/42) to the corresponding
// spec path pattern (e.g., /users/{id}).
// ctx is an optional replacement context for controlling generated values.
func (f *Factory) ResponseBodyFromRequest(r *http.Request, ctx map[string]any) (json.RawMessage, error) {
	specPath, ok := f.matcher.Match(r.URL.Path, r.Method)
	if !ok {
		return nil, fmt.Errorf("no matching operation for %s %s", r.Method, r.URL.Path)
	}
	return f.ResponseBody(specPath, r.Method, ctx)
}

// Operations returns route info for all available operations.
func (f *Factory) Operations() []typedef.RouteInfo {
	return f.registry.GetRouteInfo()
}

// MatchPath resolves a concrete request path (e.g., /users/42) to the
// corresponding OpenAPI spec path pattern (e.g., /users/{id}).
// Returns the spec path and true if a match is found.
func (f *Factory) MatchPath(requestPath, method string) (string, bool) {
	return f.matcher.Match(requestPath, method)
}

// FindOperation returns the parsed operation for the given spec path and method.
func (f *Factory) FindOperation(path, method string) *schema.Operation {
	return f.registry.FindOperation(path, method)
}

// declaredResponses returns the set of declared numeric status codes
// plus a flag indicating whether a "default" response is declared.
// Results are cached per operation.
func (f *Factory) declaredResponses(path, method string) (map[int]struct{}, bool) {
	key := path + ":" + strings.ToUpper(method)
	if v, ok := f.declaredCodes.Load(key); ok {
		info := v.(declaredResponseInfo)
		return info.codes, info.hasDefault
	}
	info := f.computeDeclaredResponses(path, method)
	f.declaredCodes.Store(key, info)
	return info.codes, info.hasDefault
}

func (f *Factory) computeDeclaredResponses(path, method string) declaredResponseInfo {
	info := declaredResponseInfo{codes: map[int]struct{}{}}

	doc, err := f.Document()
	if err != nil || doc == nil {
		return info
	}
	model, err := doc.BuildV3Model()
	if err != nil || model == nil || model.Model.Paths == nil {
		return info
	}

	pathItem, ok := model.Model.Paths.PathItems.Get(path)
	if !ok {
		return info
	}

	var op *v3high.Operation
	upper := strings.ToUpper(method)
	for m, o := range pathItem.GetOperations().FromOldest() {
		if strings.ToUpper(m) == upper {
			op = o
			break
		}
	}
	if op == nil || op.Responses == nil {
		return info
	}

	info.hasDefault = op.Responses.Default != nil
	for status := range op.Responses.Codes.FromOldest() {
		if n, err := strconv.Atoi(status); err == nil {
			info.codes[n] = struct{}{}
		}
	}
	return info
}

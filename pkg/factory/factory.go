package factory

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	"github.com/mockzilla/mockzilla/v2/pkg/api"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/generator"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/mockzilla/mockzilla/v2/pkg/typedef"
)

// Factory generates mock requests and responses based on an OpenAPI spec.
// It wraps the registry and generator for convenient programmatic use.
type Factory struct {
	registry typedef.OperationRegistry
	gen      generator.Generate
	matcher  *pathMatcher
}

type factoryConfig struct {
	serviceContext []byte
	specOptions    *config.SpecOptions
	codegenCfg     *codegen.Configuration
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

// WithCodegenConfig sets the codegen configuration used for spec parsing.
// When not provided, the default codegen configuration is used.
func WithCodegenConfig(cfg codegen.Configuration) FactoryOption {
	return func(c *factoryConfig) {
		c.codegenCfg = &cfg
	}
}

// NewFactory creates a Factory from raw OpenAPI spec bytes.
// Default replacement contexts (common, fake, words) are loaded automatically.
func NewFactory(specBytes []byte, opts ...FactoryOption) (*Factory, error) {
	fc := &factoryConfig{}
	for _, opt := range opts {
		opt(fc)
	}

	codegenCfg := codegen.NewDefaultConfiguration()
	if fc.codegenCfg != nil {
		codegenCfg = fc.codegenCfg.WithDefaults()
	}

	registry, err := typedef.NewRegistryFromSpec(specBytes, codegenCfg, fc.specOptions)
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
		registry: registry,
		gen:      gen,
		matcher:  matcher,
	}, nil
}

// Response generates a mock response for the given spec path and method.
// path should be the OpenAPI path pattern (e.g., "/users/{id}").
// ctx is an optional replacement context for controlling generated values.
func (f *Factory) Response(path, method string, ctx map[string]any) (schema.ResponseData, error) {
	respSchema := f.registry.GetResponseSchema(path, method)
	if respSchema == nil {
		return schema.ResponseData{}, fmt.Errorf("no operation found for %s %s", method, path)
	}
	return f.gen.Response(respSchema, ctx), nil
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
	return f.Response(specPath, r.Method, ctx)
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

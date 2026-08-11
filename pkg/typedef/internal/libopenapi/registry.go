// Package libopenapi parses OpenAPI specs via pb33f/libopenapi and
// exposes a Registry whose methods match typedef.OperationRegistry.
// It is reached through typedef.NewRegistry; nothing outside
// pkg/typedef should import this package directly.
package libopenapi

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
)

// Options configures the libopenapi-direct provider.
type Options struct {
	SpecOptions *config.SpecOptions
	Logger      *slog.Logger

	// ReleaseDocument drops the parsed document and v3 model once every
	// operation is converted, trading [Registry.Document] for the memory they
	// pin for the process lifetime. Ignored unless SpecOptions disables
	// LazyLoad, since a later conversion would need the model back.
	ReleaseDocument bool
}

// Registry is the libopenapi-backed runtime registry.
type Registry struct {
	doc      libopenapi.Document
	model    *v3.Document
	released bool

	routes          []schema.RouteInfo
	index           map[string]*opEntry
	staticResponses map[staticResponseKey]string
	usedIDs         map[string]int
}

// opEntry holds one (path, method, *v3.Operation) row plus its lazily
// converted *schema.Operation value.
type opEntry struct {
	path   string
	method string
	op     *v3.Operation

	once sync.Once
	conv *schema.Operation
}

// NewRegistry parses specBytes with libopenapi, indexes its operations,
// and returns a Registry. When opts.SpecOptions.LazyLoad is false, all
// operations are pre-converted before returning so spec-level
// conversion failures surface at startup.
func NewRegistry(specBytes []byte, opts Options) (*Registry, error) {
	doc, err := newDocument(specBytes, opts.Logger)
	if err != nil {
		return nil, fmt.Errorf("creating libopenapi document: %w", err)
	}

	simplify := false
	var optProps *config.OptionalProperties
	if opts.SpecOptions != nil {
		simplify = opts.SpecOptions.Simplify
		if simplify {
			optProps = opts.SpecOptions.OptionalProperties
		}
	}

	model, err := BuildModel(doc, simplify, optProps)
	if err != nil {
		return nil, fmt.Errorf("building v3 model: %w", err)
	}

	reg := &Registry{
		doc:             doc,
		model:           model,
		index:           map[string]*opEntry{},
		staticResponses: map[staticResponseKey]string{},
	}

	reg.buildIndex()

	eager := opts.SpecOptions != nil && !opts.SpecOptions.LazyLoad
	if eager {
		for _, e := range reg.index {
			reg.convertEntry(e)
		}
		if opts.ReleaseDocument {
			reg.release()
		}
	}

	return reg, nil
}

func (r *Registry) FindOperation(path, method string) *schema.Operation {
	e, ok := r.index[indexKey(path, method)]
	if !ok {
		return nil
	}
	return r.convertEntry(e)
}

func (r *Registry) Operations() []*schema.Operation {
	out := make([]*schema.Operation, 0, len(r.index))
	for _, e := range r.index {
		if e.conv != nil {
			out = append(out, e.conv)
		}
	}
	return out
}

func (r *Registry) GetRouteInfo() []schema.RouteInfo {
	return r.routes
}

func (r *Registry) GetResponseSchema(path, method string) *schema.ResponseSchema {
	op := r.FindOperation(path, method)
	if op == nil || op.Response == nil {
		return nil
	}
	out := &schema.ResponseSchema{}
	if success := op.Response.GetSuccess(); success != nil {
		out.ContentType = success.ContentType
		out.Body = success.Content
		out.Headers = success.Headers
	}
	return out
}

// Document returns the parsed libopenapi document so callers (like
// pkg/factory) can hand it to the libopenapi-validator without re-parsing.
// It errors when the document was released, so callers re-parse rather than
// silently treating a nil document as a spec that declares nothing.
func (r *Registry) Document() (libopenapi.Document, error) {
	if r.released {
		return nil, ErrDocumentReleased
	}
	return r.doc, nil
}

// release drops every reference into libopenapi once conversion is done.
// opEntry.op has to go too: the index would otherwise keep the whole v3 model
// reachable and free nothing.
func (r *Registry) release() {
	for _, e := range r.index {
		e.op = nil
	}
	r.doc = nil
	r.model = nil
	r.released = true
}

// componentSchemas is the document's schema components, or nil when it declares
// none. convertCtx resolves a $ref against them.
func (r *Registry) componentSchemas() *orderedmap.Map[string, *base.SchemaProxy] {
	if r.model == nil || r.model.Components == nil {
		return nil
	}
	return r.model.Components.Schemas
}

func (r *Registry) buildIndex() {
	if r.model == nil || r.model.Paths == nil || r.model.Paths.PathItems == nil {
		return
	}

	for path, pathItem := range r.model.Paths.PathItems.FromOldest() {
		if pathItem == nil {
			continue
		}
		for method, op := range pathItem.GetOperations().FromOldest() {
			if op == nil {
				continue
			}

			upper := strings.ToUpper(method)
			e := &opEntry{path: path, method: upper, op: op}
			r.index[indexKey(path, upper)] = e
			isStatic := r.collectStaticResponses(path, upper, op)

			r.routes = append(r.routes, schema.RouteInfo{
				ID:       operationID(op, upper, path),
				Method:   upper,
				Path:     path,
				IsStatic: isStatic,
			})
		}
	}
}

func (r *Registry) convertEntry(e *opEntry) *schema.Operation {
	if e == nil {
		return nil
	}
	e.once.Do(func() {
		e.conv = r.convertOperation(e)
	})
	return e.conv
}

// convertOperation builds a *schema.Operation for one indexed entry by
// walking the libopenapi *v3.Operation directly. Schema conversion uses
// a fresh convertCtx per call so per-operation caches don't leak across
// independent FindOperation invocations.
func (r *Registry) convertOperation(e *opEntry) *schema.Operation {
	op := e.op
	if op == nil {
		return nil
	}

	ctx := newConvertCtx()
	ctx.components = r.componentSchemas()
	id := operationID(op, e.method, e.path)
	id = r.uniqueOperationID(id)

	pathSchema, headerSchema, querySchema := convertParameters(op.Parameters, ctx)

	contentType := "application/json"
	var bodySchema *schema.Schema
	var bodyEncoding map[string]codegen.RequestBodyEncoding
	if op.RequestBody != nil && op.RequestBody.Content != nil && op.RequestBody.Content.Len() > 0 {
		mediaType, picked := pickContent(op.RequestBody.Content)
		if mediaType != "" {
			contentType = normaliseJSONMediaType(mediaType)
		}
		if picked != nil {
			bodySchema = convertProxy(picked.Schema, ctx)
			bodyEncoding = convertRequestBodyEncoding(picked.Encoding)
		}
	}

	response := r.convertResponses(op.Responses, e.method, e.path, ctx)

	return &schema.Operation{
		ID:           id,
		Method:       e.method,
		Path:         e.path,
		ContentType:  contentType,
		Headers:      headerSchema,
		PathParams:   pathSchema,
		Query:        querySchema,
		Response:     response,
		Body:         bodySchema,
		BodyEncoding: bodyEncoding,
	}
}

// convertResponses walks every declared response and produces the
// schema.Response with .SuccessCode populated to the lowest 2xx (or the
// lowest declared code when no 2xx exists; matches today's behaviour
// before factory.SuccessStatusCode picks a more refined code).
func (r *Registry) convertResponses(responses *v3.Responses, method, path string, ctx *convertCtx) *schema.Response {
	all := map[int]*schema.ResponseItem{}

	if responses != nil && responses.Codes != nil {
		for codeStr, resp := range responses.Codes.FromOldest() {
			code, ok := parseStatusCode(codeStr)
			if !ok {
				continue
			}
			item := r.convertResponseItem(code, resp, ctx)
			if static, ok := r.staticResponses[newStaticResponseKey(method, path, code)]; ok && item != nil {
				if item.Content == nil {
					item.Content = &schema.Schema{}
				}
				item.Content.StaticContent = static
			}
			all[code] = item
		}
	}

	// When no 2xx is declared, fabricate an entry to serve as SuccessCode.
	// If a `default` response is declared, inherit its content/headers so
	// the response writer picks the right content-type and schema; otherwise
	// the entry stays empty. factory.SuccessStatusCode still substitutes a
	// spec-declared code for the HTTP status line.
	successCode := pickSuccessCode(all)
	if successCode < 200 || successCode >= 300 {
		fabricated := &schema.ResponseItem{StatusCode: 204}
		if responses != nil && responses.Default != nil {
			if item := r.convertResponseItem(204, responses.Default, ctx); item != nil {
				fabricated.ContentType = item.ContentType
				fabricated.Content = item.Content
				fabricated.Headers = item.Headers
			}
		}
		all[204] = fabricated
		successCode = 204
	}
	return schema.NewResponse(all, successCode)
}

func (r *Registry) convertResponseItem(code int, resp *v3.Response, ctx *convertCtx) *schema.ResponseItem {
	if resp == nil {
		return &schema.ResponseItem{StatusCode: code}
	}

	contentType := ""
	var contentSchema *schema.Schema
	if resp.Content != nil && resp.Content.Len() > 0 {
		mediaType, picked := pickContent(resp.Content)
		contentType = mediaType
		if picked != nil {
			contentSchema = convertProxy(picked.Schema, ctx)
		}
	}

	headers := map[string]*schema.Schema{}
	if resp.Headers != nil {
		for k, h := range resp.Headers.FromOldest() {
			if h == nil {
				continue
			}
			if sub := convertProxy(h.Schema, ctx); sub != nil {
				headers[k] = sub
			}
		}
	}

	return &schema.ResponseItem{
		StatusCode:  code,
		ContentType: contentType,
		Content:     contentSchema,
		Headers:     headers,
	}
}

// collectStaticResponses walks an operation's responses and records any
// x-static-response extension values. Called from buildIndex so the
// extraction happens during the same model walk that builds the
// operation index (no separate libopenapi parse). Returns true if at
// least one (status code, content type) was a static overlay, so the
// caller can stamp `RouteInfo.Static = true` for UI badging.
func (r *Registry) collectStaticResponses(path, method string, op *v3.Operation) bool {
	if op == nil || op.Responses == nil || op.Responses.Codes == nil {
		return false
	}
	found := false
	for codeStr, resp := range op.Responses.Codes.FromOldest() {
		if resp == nil || resp.Content == nil {
			continue
		}
		code, err := strconv.Atoi(codeStr)
		if err != nil {
			continue
		}
		for _, mt := range resp.Content.FromOldest() {
			if mt == nil || mt.Extensions == nil {
				continue
			}
			ext, ok := mt.Extensions.Get(extStaticResponse)
			if !ok || ext == nil {
				continue
			}
			value := strings.TrimSpace(ext.Value)
			if value == "" {
				continue
			}
			r.staticResponses[newStaticResponseKey(method, path, code)] = value
			found = true
		}
	}
	return found
}

// uniqueOperationID disambiguates colliding operation IDs by appending
// a counter suffix to repeats. The first occurrence keeps its name.
func (r *Registry) uniqueOperationID(id string) string {
	if id == "" {
		return id
	}
	if r.usedIDs == nil {
		r.usedIDs = map[string]int{}
	}
	if count, ok := r.usedIDs[id]; ok {
		r.usedIDs[id] = count + 1
		return fmt.Sprintf("%s%d", id, count+1)
	}
	r.usedIDs[id] = 1
	return id
}

func indexKey(path, method string) string {
	return path + ":" + strings.ToUpper(method)
}

func newDocument(specBytes []byte, logger *slog.Logger) (libopenapi.Document, error) {
	cfg := datamodel.NewDocumentConfiguration()
	if logger != nil {
		cfg.Logger = logger
	}
	// Cyclic component refs (e.g. stripe payment_intent <-> charge) would
	// otherwise fail BuildV3Model; convertProxy bounds the recursion itself.
	cfg.SkipCircularReferenceCheck = true
	return libopenapi.NewDocumentWithConfiguration(specBytes, cfg)
}

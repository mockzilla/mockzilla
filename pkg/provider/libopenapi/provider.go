package libopenapi

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/mockzilla/mockzilla/v2/pkg/typedef"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// Options configures the libopenapi-direct provider. Only SpecOptions
// fields LazyLoad, Simplify, and OptionalProperties are honoured here;
// the codegen.Configuration that the codegen-based registry consumes
// is intentionally absent (the libopenapi parser has no equivalent
// notion of filter/overlay; those features stay with the codegen path).
type Options struct {
	SpecOptions *config.SpecOptions
	Logger      *slog.Logger
}

// Registry is the libopenapi-direct OperationRegistry implementation.
// It also implements typedef.DocumentProvider so the factory can share
// the parsed document with validator construction.
type Registry struct {
	doc   libopenapi.Document
	model *v3.Document

	routes []typedef.RouteInfo

	index map[string]*opEntry

	staticResponses map[staticResponseKey]string

	// usedIDs counts synthesised operation IDs so identical names get
	// disambiguated with a trailing counter (mirrors typedef/registry.go).
	usedIDs map[string]int
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
func NewRegistry(specBytes []byte, opts Options) (typedef.OperationRegistry, error) {
	doc, err := newDocument(specBytes, opts.Logger)
	if err != nil {
		return nil, fmt.Errorf("creating libopenapi document: %w", err)
	}

	simplify := false
	var optProps *typedef.OptionalPropertyConfig
	if opts.SpecOptions != nil {
		simplify = opts.SpecOptions.Simplify
		if simplify && opts.SpecOptions.OptionalProperties != nil {
			optProps = &typedef.OptionalPropertyConfig{
				Min: opts.SpecOptions.OptionalProperties.Min,
				Max: opts.SpecOptions.OptionalProperties.Max,
			}
		}
	}

	model, err := typedef.BuildModel(doc, simplify, optProps)
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
	}

	return reg, nil
}

// FindOperation implements typedef.OperationRegistry.
func (r *Registry) FindOperation(path, method string) *schema.Operation {
	e, ok := r.index[indexKey(path, method)]
	if !ok {
		return nil
	}
	return r.convertEntry(e)
}

// Operations implements typedef.OperationRegistry.
func (r *Registry) Operations() []*schema.Operation {
	out := make([]*schema.Operation, 0, len(r.index))
	for _, e := range r.index {
		if e.conv != nil {
			out = append(out, e.conv)
		}
	}
	return out
}

// GetRouteInfo implements typedef.OperationRegistry.
func (r *Registry) GetRouteInfo() []typedef.RouteInfo {
	return r.routes
}

// GetResponseSchema implements typedef.OperationRegistry.
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

// Document implements typedef.DocumentProvider so pkg/factory can reuse
// the parsed document for the libopenapi-validator without re-parsing.
func (r *Registry) Document() (libopenapi.Document, error) {
	return r.doc, nil
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
			r.routes = append(r.routes, typedef.RouteInfo{
				ID:     operationID(op, upper, path),
				Method: upper,
				Path:   path,
			})
			r.collectStaticResponses(path, upper, op)
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

func indexKey(path, method string) string {
	return path + ":" + strings.ToUpper(method)
}

func newDocument(specBytes []byte, logger *slog.Logger) (libopenapi.Document, error) {
	cfg := datamodel.NewDocumentConfiguration()
	if logger != nil {
		cfg.Logger = logger
	}
	// Codegen's CreateDocument sets this too; without it, real-world specs
	// with mutually-referencing components (stripe payment_intent <-> charge)
	// fail BuildV3Model. The schema converter bounds recursion on its own
	// proxy depth counter, so libopenapi-side detection adds no safety.
	cfg.SkipCircularReferenceCheck = true
	return libopenapi.NewDocumentWithConfiguration(specBytes, cfg)
}

package typedef

import (
	"log/slog"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/mockzilla/mockzilla/v2/pkg/typedef/internal/libopenapi"
	pblibopenapi "github.com/pb33f/libopenapi"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

type RouteInfo = schema.RouteInfo

// OperationRegistry is the interface for accessing parsed operations.
type OperationRegistry interface {
	FindOperation(path, method string) *schema.Operation
	Operations() []*schema.Operation
	GetRouteInfo() []RouteInfo
	GetResponseSchema(path, method string) *schema.ResponseSchema
}

// DocumentProvider exposes the parsed OpenAPI document so callers
// (the factory's validator construction) can reuse it without re-parsing.
type DocumentProvider interface {
	Document() (pblibopenapi.Document, error)
}

// RegistryOptions configures NewRegistry.
type RegistryOptions struct {
	SpecOptions *config.SpecOptions
	Logger      *slog.Logger
}

// NewRegistry parses an OpenAPI spec and returns an OperationRegistry.
func NewRegistry(specBytes []byte, opts RegistryOptions) (OperationRegistry, error) {
	return libopenapi.NewRegistry(specBytes, libopenapi.Options{
		SpecOptions: opts.SpecOptions,
		Logger:      opts.Logger,
	})
}

// BuildModel returns the v3 model. When simplify is true the model is
// reshaped in place: unions reduced to their first variant, optional
// properties pruned per opts.
func BuildModel(doc pblibopenapi.Document, simplify bool, opts *config.OptionalProperties) (*v3.Document, error) {
	return libopenapi.BuildModel(doc, simplify, opts)
}

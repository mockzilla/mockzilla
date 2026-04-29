package typedef

import (
	"fmt"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
)

// StaticRoute represents a static route with its content.
type StaticRoute struct {
	Method      string
	Path        string
	ContentType string
	Content     string
}

// NewRegistryFromStaticRoutes creates a TypeDefinitionRegistry from static routes.
// It builds Operation objects with schemas inferred from the static content.
func NewRegistryFromStaticRoutes(routes []StaticRoute) (*TypeDefinitionRegistry, error) {
	operations := make([]*schema.Operation, 0, len(routes))

	for _, route := range routes {
		// Build schema from content
		responseSchema, err := schema.BuildSchemaFromContent([]byte(route.Content), route.ContentType)
		if err != nil {
			return nil, fmt.Errorf("failed to build schema for %s %s: %w", route.Method, route.Path, err)
		}

		// Create operation
		op := &schema.Operation{
			Method:      route.Method,
			Path:        route.Path,
			ContentType: route.ContentType,
			Response: schema.NewResponse(map[int]*schema.ResponseItem{
				200: {
					StatusCode:  200,
					Content:     responseSchema,
					ContentType: route.ContentType,
				},
			}, 200),
		}

		operations = append(operations, op)
	}

	// Create registry with operations
	registry := &TypeDefinitionRegistry{
		operations:           operations,
		operationsLookUp:     make(map[string]*schema.Operation),
		typeDefinitionLookUp: make(map[string]*codegen.TypeDefinition),
	}

	// Build operations lookup
	for _, op := range operations {
		key := op.Path + ":" + op.Method
		registry.operationsLookUp[key] = op
	}

	return registry, nil
}

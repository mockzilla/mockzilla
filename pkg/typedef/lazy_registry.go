package typedef

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
)

// OperationRegistry is the interface for accessing parsed operations.
// Both TypeDefinitionRegistry (eager) and LazyTypeDefinitionRegistry (lazy) implement this.
type OperationRegistry interface {
	// FindOperation finds an operation by path and method.
	FindOperation(path, method string) *schema.Operation

	// Operations returns all operations (for lazy registry, only cached ones).
	Operations() []*schema.Operation

	// GetRouteInfo returns minimal route info for all operations.
	GetRouteInfo() []RouteInfo

	// GetResponseSchema returns the success response schema for an operation.
	// Returns nil if the operation is not found or has no success response.
	GetResponseSchema(path, method string) *schema.ResponseSchema
}

// RouteInfo holds minimal route information extracted at startup.
type RouteInfo struct {
	ID     string
	Method string
	Path   string
}

// LazyTypeDefinitionRegistry is a registry that parses operations on-demand.
// This significantly speeds up server startup for large specs by deferring
// the expensive schema parsing until an operation is actually accessed.
type LazyTypeDefinitionRegistry struct {
	// Configuration for parsing
	specBytes   []byte
	codegenCfg  codegen.Configuration
	specOptions *config.SpecOptions

	// Cached parsed operations
	operations sync.Map

	// Static responses extracted once at startup
	staticResponses map[StaticResponseKey]string

	// Route info extracted at startup (fast, minimal parse)
	routeInfo []RouteInfo

	mu sync.Mutex
}

// NewLazyTypeDefinitionRegistry creates a new lazy registry.
// It only extracts route paths/methods at startup - actual schema parsing is deferred.
func NewLazyTypeDefinitionRegistry(specBytes []byte, codegenCfg codegen.Configuration, specOptions *config.SpecOptions) (*LazyTypeDefinitionRegistry, error) {
	start := time.Now()

	// Extract static responses (fast operation)
	staticResponses, err := ExtractStaticResponses(specBytes)
	if err != nil {
		staticResponses = make(map[StaticResponseKey]string)
	}

	// Extract just route info (paths + methods + operation IDs)
	routeInfo, err := extractRouteInfo(specBytes, codegenCfg)
	if err != nil {
		return nil, fmt.Errorf("extracting route info: %w", err)
	}

	slog.Info("Lazy registry initialized",
		"routes", len(routeInfo),
		"duration", time.Since(start))

	return &LazyTypeDefinitionRegistry{
		specBytes:       specBytes,
		codegenCfg:      codegenCfg,
		specOptions:     specOptions,
		staticResponses: staticResponses,
		routeInfo:       routeInfo,
	}, nil
}

// FindOperation finds an operation by path and method.
// If not cached, it parses the operation on-demand and caches it.
// Method is case-insensitive (normalized to uppercase).
func (r *LazyTypeDefinitionRegistry) FindOperation(path, method string) *schema.Operation {
	method = strings.ToUpper(method)
	key := fmt.Sprintf("%s:%s", path, method)

	// Check cache first
	if cached, ok := r.operations.Load(key); ok {
		return cached.(*schema.Operation)
	}

	// Parse on-demand
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring lock
	if cached, ok := r.operations.Load(key); ok {
		return cached.(*schema.Operation)
	}

	op, err := r.parseOperation(path, method)
	if err != nil {
		slog.Error("Failed to parse operation", "path", path, "method", method, "error", err)
		return nil
	}

	if op != nil {
		r.operations.Store(key, op)
	}

	return op
}

// Operations returns all cached operations.
// Note: For lazy registry, this only returns operations that have been accessed.
func (r *LazyTypeDefinitionRegistry) Operations() []*schema.Operation {
	var ops []*schema.Operation
	r.operations.Range(func(key, value any) bool {
		ops = append(ops, value.(*schema.Operation))
		return true
	})
	return ops
}

// GetRouteInfo returns the list of all routes (for registering handlers).
func (r *LazyTypeDefinitionRegistry) GetRouteInfo() []RouteInfo {
	return r.routeInfo
}

// GetResponseSchema returns the success response schema for an operation.
func (r *LazyTypeDefinitionRegistry) GetResponseSchema(path, method string) *schema.ResponseSchema {
	op := r.FindOperation(path, method)
	if op == nil {
		return nil
	}

	respSchema := &schema.ResponseSchema{}
	if successResp := op.Response.GetSuccess(); successResp != nil {
		respSchema.ContentType = successResp.ContentType
		respSchema.Body = successResp.Content
		respSchema.Headers = successResp.Headers
	}
	return respSchema
}

// parseOperation parses a single operation by filtering to just that operation ID.
func (r *LazyTypeDefinitionRegistry) parseOperation(path, method string) (*schema.Operation, error) {
	start := time.Now()

	// Find the operation ID for this path/method
	var operationID string
	for _, ri := range r.routeInfo {
		if ri.Path == path && ri.Method == method {
			operationID = ri.ID
			break
		}
	}

	if operationID == "" {
		return nil, fmt.Errorf("operation not found: %s %s", method, path)
	}

	// Create a config that filters to just this operation
	cfg := r.codegenCfg
	cfg.Filter = codegen.FilterConfig{
		Include: codegen.FilterParamsConfig{
			OperationIDs: []string{operationID},
		},
	}

	// Parse with filter
	parseCtx, errs := CreateParseContext(r.specBytes, cfg, r.specOptions)
	if len(errs) > 0 {
		return nil, errs[0]
	}

	if parseCtx == nil || len(parseCtx.Operations) == 0 {
		return nil, fmt.Errorf("no operations found for %s", operationID)
	}

	// Build the registry for just this operation
	registry := NewTypeDefinitionRegistry(parseCtx, 0, r.specBytes)

	op := registry.FindOperation(path, method)

	slog.Debug("Parsed operation on-demand",
		"operationID", operationID,
		"path", path,
		"method", method,
		"duration", time.Since(start))

	return op, nil
}

// extractRouteInfo extracts minimal route information from the spec without full parsing.
// This is much faster than full parsing as it only reads paths and methods.
func extractRouteInfo(specBytes []byte, cfg codegen.Configuration) ([]RouteInfo, error) {
	// Use libopenapi directly for minimal parsing
	doc, err := codegen.CreateDocument(specBytes, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating document: %w", err)
	}

	model, err := doc.BuildV3Model()
	if err != nil {
		return nil, fmt.Errorf("building model: %w", err)
	}

	if model == nil || model.Model.Paths == nil {
		return nil, nil
	}

	var routes []RouteInfo

	for path, pathItem := range model.Model.Paths.PathItems.FromOldest() {
		for method, op := range pathItem.GetOperations().FromOldest() {
			// Uppercase method to match oapi-codegen behavior
			upperMethod := strings.ToUpper(method)

			opID := op.OperationId
			if opID == "" {
				// Generate operation ID if not provided
				opID = method + "_" + path
			}

			routes = append(routes, RouteInfo{
				ID:     opID,
				Method: upperMethod,
				Path:   path,
			})
		}
	}

	return routes, nil
}

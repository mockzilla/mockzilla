package typedef

import (
	"fmt"
	"mime"
	"strings"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/pb33f/libopenapi/datamodel/high/base"
)

// TypeDefinitionRegistry is a struct that holds all type definitions and operations.
type TypeDefinitionRegistry struct {
	operations           []*schema.Operation
	operationsLookUp     map[string]*schema.Operation
	typeDefinitionLookUp map[string]*codegen.TypeDefinition
}

func (td *TypeDefinitionRegistry) Operations() []*schema.Operation {
	return td.operations
}

// GetTypeDefinitionLookup returns a map of all type definitions.
func (td *TypeDefinitionRegistry) GetTypeDefinitionLookup() map[string]*codegen.TypeDefinition {
	return td.typeDefinitionLookUp
}

// FindOperation finds an operation by path and method.
// Method is case-insensitive (normalized to uppercase).
func (td *TypeDefinitionRegistry) FindOperation(path, method string) *schema.Operation {
	return td.operationsLookUp[fmt.Sprintf("%s:%s", path, strings.ToUpper(method))]
}

// GetRouteInfo returns minimal route info for all operations.
// This implements the OperationRegistry interface.
func (td *TypeDefinitionRegistry) GetRouteInfo() []RouteInfo {
	routes := make([]RouteInfo, 0, len(td.operations))
	for _, op := range td.operations {
		routes = append(routes, RouteInfo{
			ID:     op.ID,
			Method: op.Method,
			Path:   op.Path,
		})
	}
	return routes
}

// GetResponseSchema returns the success response schema for an operation.
func (td *TypeDefinitionRegistry) GetResponseSchema(path, method string) *schema.ResponseSchema {
	op := td.FindOperation(path, method)
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

// NewTypeDefinitionRegistry creates a new TypeDefinitionRegistry instance.
// It extracts x-static-response extensions from the spec if specBytes is provided.
func NewTypeDefinitionRegistry(parseCtx *codegen.ParseContext, maxRecursionDepth int, specBytes []byte) *TypeDefinitionRegistry {
	// Extract static responses if spec bytes are provided
	var staticResponses map[StaticResponseKey]string
	if specBytes != nil {
		var err error
		staticResponses, err = ExtractStaticResponses(specBytes)
		if err != nil {
			// Log error but continue - static responses are optional
			staticResponses = make(map[StaticResponseKey]string)
		}
	}
	// Use TypeTracker as the single source of truth for all type definitions
	tdsLookUp := parseCtx.TypeTracker.AsMap()

	var operations []*schema.Operation
	operationsLookUp := make(map[string]*schema.Operation)

	// Track used IDs to ensure uniqueness
	usedIDs := make(map[string]int)

	// enhance all operations
	for _, op := range parseCtx.Operations {
		var (
			pathSchema   *schema.Schema
			hdrSchema    *schema.Schema
			reqSchema    *schema.Schema
			bodyEncoding map[string]codegen.RequestBodyEncoding
		)

		contentType := "application/json"

		if op.PathParams != nil {
			resolved := resolveCodegenSchema(&op.PathParams.Schema, tdsLookUp, nil)
			pathSchema = newSchemaFromGoSchema(resolved, tdsLookUp, maxRecursionDepth)
		}

		if op.Header != nil {
			resolved := resolveCodegenSchema(&op.Header.TypeDef.Schema, tdsLookUp, nil)
			hdrSchema = newSchemaFromGoSchema(resolved, tdsLookUp, maxRecursionDepth)
		}

		if op.Body != nil {
			contentType = op.Body.ContentType
			// Normalize JSON-based content types (e.g., application/merge-patch+json) to application/json
			if isMediaTypeJSON(contentType) {
				contentType = "application/json"
			}
			resolved := resolveCodegenSchema(&op.Body.Schema, tdsLookUp, nil)
			reqSchema = newSchemaFromGoSchema(resolved, tdsLookUp, maxRecursionDepth)
			bodyEncoding = op.Body.Encoding
		}

		all := make(map[int]*schema.ResponseItem)
		for code, resp := range op.Response.All {
			headers := make(map[string]*schema.Schema)
			if resp.Headers != nil {
				for k, v := range resp.Headers {
					resolved := resolveCodegenSchema(&v, tdsLookUp, nil)
					respHeaders := newSchemaFromGoSchema(resolved, tdsLookUp, maxRecursionDepth)
					headers[k] = respHeaders
				}
			}

			resolved := resolveCodegenSchema(&resp.Schema, tdsLookUp, nil)
			respContent := newSchemaFromGoSchema(resolved, tdsLookUp, maxRecursionDepth)

			// Check for static response content
			if staticResponses != nil {
				key := NewStaticResponseKey(op.Method, op.Path, code)
				if staticContent, ok := staticResponses[key]; ok {
					respContent.StaticContent = staticContent
				}
			}

			// Normalize JSON-based content types (e.g., application/vnd.api+json) to application/json
			respContentType := resp.ContentType
			if isMediaTypeJSON(respContentType) {
				respContentType = "application/json"
			}

			all[code] = &schema.ResponseItem{
				Headers:     headers,
				StatusCode:  code,
				ContentType: respContentType,
				Content:     respContent,
			}
		}
		response := schema.NewResponse(all, op.Response.SuccessStatusCode)

		var queryParams schema.QueryParameters
		if op.Query != nil && len(op.Query.Params) > 0 {
			queryParams = make(schema.QueryParameters)
			for _, p := range op.Query.Params {
				resolved := resolveCodegenSchema(&p.Schema, tdsLookUp, nil)
				paramSchema := newSchemaFromGoSchema(resolved, tdsLookUp, maxRecursionDepth)

				var encoding *codegen.ParameterEncoding
				if enc, ok := op.Query.Encoding[p.ParamName]; ok {
					encoding = &enc
				}

				queryParams[p.ParamName] = &schema.QueryParameter{
					Schema:   paramSchema,
					Required: p.Required,
					Encoding: encoding,
				}
			}
		}

		// Normalize operation ID to ensure uniqueness
		opID := op.ID
		if opID == "" {
			opID = strings.ToLower(op.Method) + "_" + strings.ReplaceAll(strings.ReplaceAll(op.Path, "/", "_"), "{", "")
			opID = strings.ReplaceAll(opID, "}", "")
		}
		if count, exists := usedIDs[opID]; exists {
			usedIDs[opID] = count + 1
			opID = fmt.Sprintf("%s%d", opID, count+1)
		} else {
			usedIDs[opID] = 1
		}

		enhanced := &schema.Operation{
			ID:           opID,
			Method:       op.Method,
			Path:         op.Path,
			ContentType:  contentType,
			Headers:      hdrSchema,
			PathParams:   pathSchema,
			Query:        queryParams,
			Response:     response,
			Body:         reqSchema,
			BodyEncoding: bodyEncoding,
		}
		operations = append(operations, enhanced)
		operationsLookUp[fmt.Sprintf("%s:%s", op.Path, op.Method)] = enhanced
	}

	return &TypeDefinitionRegistry{
		operations:           operations,
		operationsLookUp:     operationsLookUp,
		typeDefinitionLookUp: tdsLookUp,
	}
}

// NewRegistryFromSpec creates an OperationRegistry from raw OpenAPI spec bytes.
// This is a convenience function that handles parsing and registry creation.
// If specOptions.LazyLoad is true, operations are parsed on-demand for faster startup.
func NewRegistryFromSpec(specBytes []byte, codegenCfg codegen.Configuration, specOptions *config.SpecOptions) (OperationRegistry, error) {
	if specOptions != nil && specOptions.LazyLoad {
		registry, err := NewLazyTypeDefinitionRegistry(specBytes, codegenCfg, specOptions)
		if err != nil {
			return nil, fmt.Errorf("creating lazy registry: %w", err)
		}
		return registry, nil
	}

	parseCtx, errs := CreateParseContext(specBytes, codegenCfg, specOptions)
	if len(errs) > 0 {
		return nil, fmt.Errorf("parsing OpenAPI spec: %w", errs[0])
	}

	return NewTypeDefinitionRegistry(parseCtx, 0, specBytes), nil
}

func resolveCodegenSchema(schema *codegen.GoSchema, tdLookIp map[string]*codegen.TypeDefinition, state map[string]*codegen.GoSchema) *codegen.GoSchema {
	if state == nil {
		state = make(map[string]*codegen.GoSchema)
	}

	// Use RefType or GoType as logical ID
	var refName string
	if schema.RefType != "" {
		refName = schema.RefType
	} else {
		refName = schema.GoType
	}

	if refName != "" {
		if res, exists := state[refName]; exists {
			// Cycle or cached resolution
			return res
		}

		if td, ok := tdLookIp[refName]; ok {
			// Type is already defined in the lookup
			// If the input schema is just a reference (empty GoType, no ArrayType, no Properties),
			// use the type definition's schema instead
			if schema.GoType == "" && schema.ArrayType == nil && len(schema.Properties) == 0 {
				// This is just a reference - use the actual type definition
				schema = &td.Schema
			}

			// Check if this is a union wrapper (has a single embedded property that references a union type)
			// If so, unwrap it by looking up the actual union type
			// BUT: skip this for array types - the wrapper belongs to the items, not the array itself
			tdSchema := &td.Schema
			tdIsArrayType := tdSchema.ArrayType != nil || (tdSchema.GoType != "" && strings.HasPrefix(tdSchema.GoType, "[]"))
			if len(tdSchema.Properties) == 1 && len(tdSchema.UnionElements) == 0 && !tdIsArrayType {
				for _, prop := range tdSchema.Properties {
					if prop.JsonFieldName == "" && prop.Schema.RefType != "" {
						if refTd, ok := tdLookIp[prop.Schema.RefType]; ok {
							if len(refTd.Schema.UnionElements) > 0 {
								// This is a union wrapper, use the actual union type
								schema = &refTd.Schema
								break
							}
						}
					}
				}
			}

			// Cache and return to avoid exponential expansion
			state[refName] = schema
			return schema
		}
	}

	// Handle array types where GoType is []TypeName but ArrayType is nil
	// The element type might be a reference that needs to be resolved
	if schema.ArrayType == nil && schema.GoType != "" && strings.HasPrefix(schema.GoType, "[]") {
		elemTypeName := strings.TrimPrefix(schema.GoType, "[]")
		if elemTypeDef, ok := tdLookIp[elemTypeName]; ok {
			// Create a copy of the schema to avoid modifying the shared type definition
			elemSchemaCopy := elemTypeDef.Schema

			// Check if this is a union wrapper (has a single embedded property that references a union type)
			// If so, unwrap it by looking up the actual union type
			if len(elemSchemaCopy.Properties) == 1 && len(elemSchemaCopy.UnionElements) == 0 {
				for _, prop := range elemSchemaCopy.Properties {
					// Check if this is an embedded field (empty JsonFieldName) with a RefType that contains unions
					if prop.JsonFieldName == "" && prop.Schema.RefType != "" {
						if refTd, ok := tdLookIp[prop.Schema.RefType]; ok {
							if len(refTd.Schema.UnionElements) > 0 {
								// This is a union wrapper, use the actual union type
								elemSchemaCopy = refTd.Schema
								break
							}
						}
					}
				}
			}

			schema.ArrayType = &elemSchemaCopy
		} else if strings.HasPrefix(elemTypeName, "[]") {
			// Nested array (e.g., [][]T) - create a synthetic ArrayType for the inner array
			innerArraySchema := &codegen.GoSchema{
				GoType: elemTypeName,
			}
			schema.ArrayType = innerArraySchema
		}
	}

	// Now resolve children
	if schema.ArrayType != nil {
		schema.ArrayType = resolveCodegenSchema(schema.ArrayType, tdLookIp, state)
	}

	for i := range schema.Properties {
		prop := &schema.Properties[i].Schema
		// Check if this property references a type that's already in tdLookup
		refName := prop.RefType
		if refName == "" {
			refName = prop.GoType
		}
		// Only resolve if it's NOT already a top-level type definition
		// If it's in tdLookup, it will be resolved when needed
		if refName == "" || tdLookIp[refName] == nil {
			propSchema := resolveCodegenSchema(prop, tdLookIp, state)
			schema.Properties[i].Schema = *propSchema
		}
	}

	// Check if this is a union wrapper (has a single embedded property that references a union type)
	// If so, unwrap it by looking up the actual union type
	// BUT: skip this for array types - the wrapper belongs to the items, not the array itself
	var parentOneOfSchemas []*base.SchemaProxy
	isArrayType := schema.ArrayType != nil || (schema.GoType != "" && strings.HasPrefix(schema.GoType, "[]"))
	if len(schema.Properties) == 1 && len(schema.UnionElements) == 0 && !isArrayType {
		for _, prop := range schema.Properties {
			// Check if this is an embedded field (empty JsonFieldName) with a RefType that contains unions
			if prop.JsonFieldName == "" && prop.Schema.RefType != "" {
				if refTd, ok := tdLookIp[prop.Schema.RefType]; ok {
					if len(refTd.Schema.UnionElements) > 0 {
						// Save the parent's OneOf schemas before unwrapping
						if schema.OpenAPISchema != nil && schema.OpenAPISchema.OneOf != nil {
							parentOneOfSchemas = schema.OpenAPISchema.OneOf
						}

						// This is a union wrapper, replace with the actual union schema
						schema = &refTd.Schema
						break
					}
				}
			}
		}
	}

	// For oneOf/anyOf, pick the first union element
	// BUT: skip this for array types - the union applies to the items, not the array itself
	// Recalculate isArrayType in case schema was modified above
	isArrayType = schema.ArrayType != nil || (schema.GoType != "" && strings.HasPrefix(schema.GoType, "[]"))
	if len(schema.UnionElements) > 0 && !isArrayType {
		name := schema.UnionElements[0].TypeName
		if unionSchema := tdLookIp[name]; unionSchema != nil {
			resolved := resolveCodegenSchema(&unionSchema.Schema, tdLookIp, state)
			// Replace the schema's properties with the union element's properties
			schema.Properties = resolved.Properties

			if resolved.ArrayType != nil {
				schema.ArrayType = resolveCodegenSchema(resolved.ArrayType, tdLookIp, state)
			}
		} else {
			// The union element is not a named type in tdLookup (e.g., primitive types like bool, string, int)
			// Try to get the actual OpenAPI schema from the OneOf array to preserve constraints
			var oneOfSchema *base.Schema

			// First try parentOneOfSchemas (from unwrapped oneOf wrapper)
			if len(parentOneOfSchemas) > 0 {
				oneOfSchemaProxy := parentOneOfSchemas[0]
				if oneOfSchemaProxy != nil {
					oneOfSchema = oneOfSchemaProxy.Schema()
				}
			}

			// If not found, try the schema's own OneOf array (direct oneOf)
			if oneOfSchema == nil && schema.OpenAPISchema != nil && len(schema.OpenAPISchema.OneOf) > 0 {
				oneOfSchemaProxy := schema.OpenAPISchema.OneOf[0]
				if oneOfSchemaProxy != nil {
					oneOfSchema = oneOfSchemaProxy.Schema()
				}
			}

			// Clear properties since this is a primitive type
			schema.Properties = nil
			schema.GoType = name

			// Set the OpenAPI schema
			oapiType := types.GoTypeToOpenAPIType(name)
			if oneOfSchema != nil {
				// Use the actual OneOf schema which preserves enum, minLength, pattern, etc.
				schema.OpenAPISchema = oneOfSchema
			} else if schema.OpenAPISchema == nil {
				schema.OpenAPISchema = &base.Schema{
					Type: []string{oapiType},
				}
			} else {
				// Preserve existing schema but update the type
				schema.OpenAPISchema.Type = []string{oapiType}
			}
		}
	}

	return schema
}

func collectTypeDefinitions(tds []codegen.TypeDefinition) []codegen.TypeDefinition {
	var res []codegen.TypeDefinition
	for _, td := range tds {
		res = append(res, td)
		res = append(res, collectTypeDefinitions(td.Schema.AdditionalTypes)...)
	}
	return res
}

// isMediaTypeJSON checks if a media type is JSON or JSON-based (e.g., application/merge-patch+json).
func isMediaTypeJSON(mediaType string) bool {
	parsed, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		return false
	}
	return parsed == "application/json" || strings.HasSuffix(parsed, "+json")
}

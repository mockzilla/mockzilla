package libopenapi

import (
	"fmt"
	"math/rand"
	"slices"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// BuildModel builds the v3 model from a parsed libopenapi document.
// When simplify is true the model is simplified in place: unions are
// reduced to their first variant and optional properties are pruned
// per optConfig.
func BuildModel(doc libopenapi.Document, simplify bool, optConfig *config.OptionalProperties) (*v3high.Document, error) {
	model, err := doc.BuildV3Model()
	if err != nil {
		return nil, fmt.Errorf("error building model: %w", err)
	}

	if simplify {
		var rng *rand.Rand
		if optConfig != nil {
			rng = rand.New(rand.NewSource(rand.Int63()))
		}
		simplifyDocument(&model.Model, optConfig, rng)
	}

	return &model.Model, nil
}

func simplifyDocument(model *v3high.Document, optConfig *config.OptionalProperties, rng *rand.Rand) {
	// Create a visited set to track schemas we've already processed (to avoid infinite recursion)
	visited := make(map[*base.Schema]bool)

	// Process all component schemas
	if model.Components != nil && model.Components.Schemas != nil {
		for _, schemaProxy := range model.Components.Schemas.FromOldest() {
			if schemaProxy == nil {
				continue
			}
			schema := schemaProxy.Schema()
			if schema == nil {
				continue
			}

			// Simplify top-level union in component schema
			if len(schema.AnyOf) > 0 || len(schema.OneOf) > 0 {
				simplifyUnionToFirstVariant(schema)
			}

			// Remove optional properties if config is provided
			if optConfig != nil {
				removeOptionalProperties(schema, optConfig, rng)
			}

			// Process this schema's properties, items, etc.
			simplifyUnionsInSchemaProperties(schema, visited)
		}
	}

	// Process component responses
	if model.Components != nil && model.Components.Responses != nil {
		for _, response := range model.Components.Responses.FromOldest() {
			if response == nil || response.Content == nil {
				continue
			}

			for _, mediaType := range response.Content.FromOldest() {
				if mediaType == nil || mediaType.Schema == nil {
					continue
				}
				schema := mediaType.Schema.Schema()
				if schema == nil {
					continue
				}

				// Simplify top-level union in response schema
				if len(schema.AnyOf) > 0 || len(schema.OneOf) > 0 {
					simplifyUnionToFirstVariant(schema)
				}

				// Remove optional properties if config is provided
				if optConfig != nil {
					removeOptionalProperties(schema, optConfig, rng)
				}

				// Process nested properties
				simplifyUnionsInSchemaProperties(schema, visited)
			}
		}
	}

	// Process schemas in paths/operations
	if model.Paths != nil && model.Paths.PathItems != nil {
		for _, pathItem := range model.Paths.PathItems.FromOldest() {
			if pathItem == nil {
				continue
			}

			// Process all operations in this path
			for _, op := range pathItem.GetOperations().FromOldest() {
				if op == nil {
					continue
				}

				// Process parameter schemas
				for _, param := range op.Parameters {
					if param != nil && param.Schema != nil {
						schema := param.Schema.Schema()
						if schema != nil {
							// Simplify top-level union in parameter schema
							if len(schema.AnyOf) > 0 || len(schema.OneOf) > 0 {
								simplifyUnionToFirstVariant(schema)
							}
							// Remove optional properties if config is provided
							if optConfig != nil {
								removeOptionalProperties(schema, optConfig, rng)
							}
							// Process nested properties
							simplifyUnionsInSchemaProperties(schema, visited)
						}
					}
				}

				// Process request body schemas
				if op.RequestBody != nil && op.RequestBody.Content != nil {
					for _, mediaType := range op.RequestBody.Content.FromOldest() {
						if mediaType != nil && mediaType.Schema != nil {
							schema := mediaType.Schema.Schema()
							if schema != nil {
								// Simplify top-level union in request body schema
								if len(schema.AnyOf) > 0 || len(schema.OneOf) > 0 {
									simplifyUnionToFirstVariant(schema)
								}
								// Remove optional properties if config is provided
								if optConfig != nil {
									removeOptionalProperties(schema, optConfig, rng)
								}
								// Process nested properties
								simplifyUnionsInSchemaProperties(schema, visited)
							}
						}
					}
				}

				// Process response schemas
				if op.Responses != nil && op.Responses.Codes != nil {
					for _, response := range op.Responses.Codes.FromOldest() {
						if response != nil && response.Content != nil {
							for _, mediaType := range response.Content.FromOldest() {
								if mediaType != nil && mediaType.Schema != nil {
									schema := mediaType.Schema.Schema()
									if schema != nil {
										// Simplify top-level union in response schema
										if len(schema.AnyOf) > 0 || len(schema.OneOf) > 0 {
											simplifyUnionToFirstVariant(schema)
										}
										// Remove optional properties if config is provided
										if optConfig != nil {
											removeOptionalProperties(schema, optConfig, rng)
										}
										// Process nested properties
										simplifyUnionsInSchemaProperties(schema, visited)
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

// simplifyUnionsInSchemaProperties recursively processes a schema's properties, items, and additionalProperties
// It simplifies unions in properties (removing optional ones, reducing required ones to single variant)
// The visited map prevents infinite recursion on circular references
func simplifyUnionsInSchemaProperties(schema *base.Schema, visited map[*base.Schema]bool) {
	if schema == nil || visited[schema] {
		return
	}
	visited[schema] = true

	// Remove extension fields (but keep examples to avoid dangling references)
	schema.Extensions = nil

	// Process properties
	if schema.Properties != nil {
		var propsToDelete []string

		for propName, propProxy := range schema.Properties.FromOldest() {
			if propProxy == nil {
				continue
			}

			propSchema := propProxy.Schema()
			if propSchema == nil {
				continue
			}

			isRequired := slices.Contains(schema.Required, propName)
			hasUnion := len(propSchema.AnyOf) > 0 || len(propSchema.OneOf) > 0

			if hasUnion {
				if !isRequired {
					// Remove optional union fields
					propsToDelete = append(propsToDelete, propName)
				} else {
					// Simplify required union to single variant
					simplifyUnionToFirstVariant(propSchema)
				}
			}

			// Recurse into nested schemas
			simplifyUnionsInSchemaProperties(propSchema, visited)
		}

		for _, propName := range propsToDelete {
			schema.Properties.Delete(propName)
		}
	}

	// Process array items
	if schema.Items != nil && schema.Items.IsA() {
		itemsSchema := schema.Items.A.Schema()
		if itemsSchema != nil {
			if len(itemsSchema.AnyOf) > 0 || len(itemsSchema.OneOf) > 0 {
				simplifyUnionToFirstVariant(itemsSchema)
			}
			simplifyUnionsInSchemaProperties(itemsSchema, visited)
		}
	}

	// Process additionalProperties
	if schema.AdditionalProperties != nil && schema.AdditionalProperties.IsA() {
		addlPropsSchema := schema.AdditionalProperties.A.Schema()
		if addlPropsSchema != nil {
			if len(addlPropsSchema.AnyOf) > 0 || len(addlPropsSchema.OneOf) > 0 {
				simplifyUnionToFirstVariant(addlPropsSchema)
			}
			simplifyUnionsInSchemaProperties(addlPropsSchema, visited)
		}
	}
}

// simplifyUnionToFirstVariant removes anyOf/oneOf and merges the first variant into the schema.
// For $ref variants, uses allOf with single element to preserve the reference.
func simplifyUnionToFirstVariant(schema *base.Schema) {
	simplifyUnionToFirstVariantSeen(schema, map[*base.Schema]bool{})
}

func simplifyUnionToFirstVariantSeen(schema *base.Schema, seen map[*base.Schema]bool) {
	// seen guards against cyclic anyOf/oneOf graphs (specs where A's first
	// variant is B and B's first variant resolves back to A). Without it,
	// the recursive flatten below blows the stack on real-world specs
	// (keap, contentwarehouse.v1.xl).
	if schema == nil || seen[schema] {
		return
	}
	seen[schema] = true

	var variants []*base.SchemaProxy
	if len(schema.AnyOf) > 0 {
		variants = schema.AnyOf
	} else if len(schema.OneOf) > 0 {
		variants = schema.OneOf
	}
	if len(variants) == 0 {
		return
	}

	// Prefer a $ref variant so the reference survives as an allOf wrapper.
	var firstVariant *base.SchemaProxy
	for _, variant := range variants {
		if variant.GetReference() != "" {
			firstVariant = variant
			break
		}
	}
	if firstVariant == nil {
		firstVariant = variants[0]
	}

	if firstVariant != nil {
		if firstVariant.GetReference() != "" {
			schema.AllOf = []*base.SchemaProxy{firstVariant}
		} else {
			variantSchema := firstVariant.Schema()
			if variantSchema != nil {
				if len(variantSchema.AnyOf) > 0 || len(variantSchema.OneOf) > 0 {
					simplifyUnionToFirstVariantSeen(variantSchema, seen)
				}
				mergeSchemaProperties(schema, variantSchema)
			}
		}
	}

	schema.AnyOf = nil
	schema.OneOf = nil
}

// mergeSchemaProperties merges properties from src into dst (dst takes precedence for conflicts)
func mergeSchemaProperties(dst, src *base.Schema) {
	if src == nil {
		return
	}

	// Merge type
	if len(dst.Type) == 0 && len(src.Type) > 0 {
		dst.Type = src.Type
	}

	// Merge format
	if dst.Format == "" && src.Format != "" {
		dst.Format = src.Format
	}

	// Merge enum
	if len(dst.Enum) == 0 && len(src.Enum) > 0 {
		dst.Enum = src.Enum
	}

	// Merge required
	for _, req := range src.Required {
		if !slices.Contains(dst.Required, req) {
			dst.Required = append(dst.Required, req)
		}
	}

	// Merge properties
	if src.Properties != nil {
		if dst.Properties == nil {
			dst.Properties = src.Properties
		} else {
			for propName, propProxy := range src.Properties.FromOldest() {
				if dst.Properties.GetOrZero(propName) == nil {
					dst.Properties.Set(propName, propProxy)
				}
			}
		}
	}

	// Merge items (for arrays)
	if dst.Items == nil && src.Items != nil {
		dst.Items = src.Items
	}

	// Merge additionalProperties
	if dst.AdditionalProperties == nil && src.AdditionalProperties != nil {
		dst.AdditionalProperties = src.AdditionalProperties
	}
}

// removeOptionalProperties removes optional properties based on the configuration:
// - Keeps properties selected alphabetically (first N names)
// - If Min == Max, keeps exactly that many
// - If Min < Max, keeps a random number between Min and Max
func removeOptionalProperties(schema *base.Schema, opts *config.OptionalProperties, rng *rand.Rand) {
	if schema == nil || schema.Properties == nil || opts == nil {
		return
	}

	// Get all property names
	var allProps []string
	for prop := range schema.Properties.KeysFromOldest() {
		allProps = append(allProps, prop)
	}

	// Determine which properties are optional
	var optionalProps []string
	for _, prop := range allProps {
		if !slices.Contains(schema.Required, prop) {
			optionalProps = append(optionalProps, prop)
		}
	}

	if len(optionalProps) == 0 {
		return
	}

	// Determine how many to keep
	numToKeep := opts.Min
	if opts.Max > opts.Min {
		numToKeep = opts.Min + rng.Intn(opts.Max-opts.Min+1)
	}

	// If we have more optional properties than we want to keep, remove some
	var propsToRemove []string
	if len(optionalProps) > numToKeep {
		// Sort optional properties alphabetically for deterministic selection
		sorted := make([]string, len(optionalProps))
		copy(sorted, optionalProps)
		slices.Sort(sorted)

		// Remove the ones after the first numToKeep (alphabetically last)
		propsToRemove = sorted[numToKeep:]
	}

	// Remove the selected properties
	for _, prop := range propsToRemove {
		schema.Properties.Delete(prop)
	}
}

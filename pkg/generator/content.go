package generator

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/jaswdr/faker/v2"
	"github.com/mockzilla/mockzilla/v2/internal/replacer"
	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
)

// generateContentFromSchema generates content from the given schema.
func generateContentFromSchema(schema *schema.Schema, valueReplacer replacer.ValueReplacer, state *replacer.ReplaceState) any {
	if schema == nil {
		return nil
	}

	if state == nil {
		state = replacer.NewReplaceState()
	}

	// Check if this schema was marked as recursive during schema transformation
	// This means it was truncated due to circular reference
	if schema.Recursive {
		slog.Debug("schema marked as recursive, returning nil",
			"namePath", state.NamePath)
		state.RecursionHit = true
		return nil
	}

	// Check if this is static content - return it directly
	if schema.StaticContent != "" {
		return json.RawMessage(schema.StaticContent)
	}

	// Runtime circular reference detection as safety net
	// SchemaStack tracks schemas by pointer to detect same schema being processed
	if schema.Type == types.TypeObject || schema.Type == types.TypeArray {
		if state.SchemaStack[schema] {
			// Already processing this schema - circular reference detected
			// Set the flag so parent objects know this was due to recursion
			state.RecursionHit = true
			return nil
		}

		// Mark this schema as being processed
		state.SchemaStack[schema] = true
		defer func() {
			delete(state.SchemaStack, schema)
		}()
	}

	// nothing to replace
	if !replacer.IsMatchSchemaReadWriteToState(schema, state) {
		return nil
	}

	typ := schema.Type
	if typ == "" {
		typ = "string"
	}

	// Handle 'any' type - used for empty schemas (items: {}) from OpenAPI specs
	// oapi-codegen generates struct{} for these, which can only unmarshal from {}
	// Generate empty objects {} that can be unmarshaled into struct{}
	if typ == "any" {
		slog.Debug("generating empty object for 'any' type", "namePath", state.NamePath)
		return map[string]any{}
	}

	// fast track with value and correctly resolved type for primitive types
	if valueReplacer != nil && len(state.NamePath) > 0 && typ != types.TypeObject && typ != types.TypeArray {
		// TODO(cubahno): remove IsCorrectlyReplacedType, resolver should do it.
		if res := valueReplacer(schema, state); res != nil && replacer.IsCorrectlyReplacedType(res, typ) {
			if res == replacer.NULL {
				return nil
			}
			return res
		}
	}

	if typ == types.TypeObject {
		obj := generateContentObject(schema, valueReplacer, state)
		// When generating write-only content (requests), don't convert nil to empty object
		// if all properties were filtered out due to being readOnly. This prevents
		// validation errors for nested objects with only readOnly required fields.
		// However, for top-level objects (empty NamePath), still return empty object.
		// Also don't convert to empty object if nil was due to recursion hit.
		isNested := state != nil && len(state.NamePath) > 0
		recursionHit := state != nil && state.RecursionHit
		if obj == nil && !recursionHit && (!schema.Nullable || !isNested) && (state == nil || !state.IsContentWriteOnly || !isNested) {
			obj = map[string]any{}
		}
		return obj
	}

	if typ == types.TypeArray {
		arr := generateContentArray(schema, valueReplacer, state)
		// Don't convert to empty array if nil was due to recursion hit
		recursionHit := state != nil && state.RecursionHit
		if arr == nil && !recursionHit && !schema.Nullable {
			arr = []any{}
		}
		return arr
	}

	// try to resolve anything
	if valueReplacer != nil {
		res := valueReplacer(schema, state)
		if res == replacer.NULL {
			return nil
		}
		return res
	}

	return nil
}

// generateContentObject generates content from the given schema with type `object`.
func generateContentObject(schema *schema.Schema, valueReplacer replacer.ValueReplacer, state *replacer.ReplaceState) any {
	if state == nil {
		state = replacer.NewReplaceState()
	}

	res := map[string]any{}

	// Build a set of required properties for quick lookup
	requiredSet := make(map[string]bool, len(schema.Required))
	for _, r := range schema.Required {
		requiredSet[r] = true
	}

	// Generate values for defined properties
	for name, schemaRef := range schema.Properties {
		// Create child state to track recursion for this property
		childState := state.NewFrom(state).WithOptions(replacer.WithName(name))
		// Reset recursion flag before generating child
		childState.RecursionHit = false

		value := generateContentFromSchema(schemaRef, valueReplacer, childState)

		// TODO(cubahno): decide whether config value needed to include null values
		if value == nil {
			isRequiredRecursion := childState.RecursionHit && requiredSet[name]
			if !isRequiredRecursion {
				continue
			}
			// Required property hit recursion - for arrays use empty array, otherwise fail
			if schemaRef == nil || schemaRef.Type != types.TypeArray {
				state.RecursionHit = true
				return nil
			}
			value = []any{}
		}

		res[name] = value

		if schema.MaxProperties != nil && *schema.MaxProperties > 0 && len(res) >= int(*schema.MaxProperties) {
			break
		}
	}

	// Generate additional properties if specified
	if schema.AdditionalProperties != nil {
		// Generate 3 additional properties by default
		numAdditional := 3
		if schema.MaxProperties != nil && *schema.MaxProperties > 0 {
			// Respect MaxProperties constraint
			remaining := int(*schema.MaxProperties) - len(res)
			if remaining < numAdditional {
				numAdditional = remaining
			}
		}

		f := faker.New()
		startLen := len(res)
		for attempts := 0; len(res)-startLen < numAdditional && attempts < numAdditional*3; attempts++ {
			name := f.Music().Genre()
			name = strings.ToLower(name)
			name = strings.SplitN(name, " ", 2)[0]
			if _, exists := res[name]; exists {
				continue
			}
			s := state.NewFrom(state).WithOptions(replacer.WithName(name))
			value := generateContentFromSchema(schema.AdditionalProperties, valueReplacer, s)
			if value != nil {
				res[name] = value
			}
		}
	}

	// Return nil if no properties were generated (will be converted to {} if not nullable)
	if len(res) == 0 {
		return nil
	}

	return res
}

// generateContentArray generates content from the given schema with type `array`.
func generateContentArray(schema *schema.Schema, valueReplacer replacer.ValueReplacer, state *replacer.ReplaceState) any {
	if state == nil {
		state = replacer.NewReplaceState()
	}

	// If items schema is nil (e.g., due to recursion limit), don't generate array
	if schema.Items == nil {
		return nil
	}

	// avoid generating too many items
	take := 1
	if schema.MinItems != nil {
		take = int(*schema.MinItems)
	}
	if take == 0 {
		take = 1
	}

	var res []any

	for i := 1; i <= take; i++ {
		childState := state.NewFrom(state).WithOptions(replacer.WithElementIndex(i))
		item := generateContentFromSchema(schema.Items, valueReplacer, childState)
		if item == nil {
			// Propagate recursion hit flag to parent
			if childState.RecursionHit {
				state.RecursionHit = true
			}
			continue
		}
		res = append(res, item)
	}

	if len(res) == 0 {
		return nil
	}

	return res
}

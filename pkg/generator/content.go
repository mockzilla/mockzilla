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

	if schema.IsNull {
		return json.RawMessage("null")
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

		// Nested write-only requests need nil to propagate up so the
		// parent can drop fields whose required children all collapsed
		// under read/write filtering; promoting nil to {} here
		// would produce a body the validator rejects as "extra empty
		// object". Outside that one path nil promotes to {} so a
		// no-property type:object schema doesn't propagate up as JSON
		// null - which fails strict validators on every oneOf branch
		// even when the field is nullable.
		isNested := state != nil && len(state.NamePath) > 0

		recursionHit := state != nil && state.RecursionHit
		writeOnlyFilter := isNested && state != nil && state.IsContentWriteOnly
		if obj == nil && !recursionHit && !writeOnlyFilter {
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

		if value == nil && requiredSet[name] && schemaRef != nil &&
			schemaRef.WriteOnly && state != nil && state.IsContentReadOnly {
			// Spec contradiction: writeOnly property marked `required`. In a
			// response (readonly) state the read/write filter would drop it,
			// but the validator still expects the key. Re-generate with the
			// write mode so the field appears. Note: the reverse case
			// (readOnly required in a write request) is intentionally not
			// patched here; clients should not send server-filled fields.
			altState := state.NewFrom(state).WithOptions(replacer.WithName(name))
			altState.RecursionHit = false
			altState.IsContentReadOnly = false
			altState.IsContentWriteOnly = true
			value = generateContentFromSchema(schemaRef, valueReplacer, altState)
		}

		// TODO(cubahno): decide whether config value needed to include null values
		if value == nil {
			isRequiredRecursion := childState.RecursionHit && requiredSet[name]
			if !isRequiredRecursion {
				// A required + nullable property that collapsed to nil
				// (e.g. nested `type: object, nullable: true` with no
				// properties) must still be present so the required-key
				// check passes. Emit JSON null.
				if requiredSet[name] && schemaRef != nil && schemaRef.Nullable {
					res[name] = json.RawMessage("null")
				}
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

	// Skip placeholder fill when additionalProperties: false. Otherwise
	// we'd produce a body the validator rejects for the additional-key
	// violation instead of the original missing-required-key one.
	if !schema.AdditionalPropertiesForbidden {
		for _, name := range schema.Required {
			if _, ok := res[name]; ok {
				continue
			}
			if propSchema, declared := schema.Properties[name]; declared {
				// Skip placeholder for fields filtered out by the
				// read/write mode (e.g. readOnly field in a write body).
				// Inserting a placeholder defeats the filter's purpose and
				// breaks "omit objects whose required fields all collapse"
				// semantics that downstream callers rely on.
				if propSchema != nil && state != nil {
					if propSchema.ReadOnly && state.IsContentWriteOnly {
						continue
					}
					if propSchema.WriteOnly && state.IsContentReadOnly {
						continue
					}
				}
				// Declared required property whose generation collapsed to
				// nil (e.g. unsatisfiable JS-regex pattern handed the
				// replacer chain a NULL sentinel). A missing key fails
				// "required" outright; a placeholder may still fail
				// pattern, but it keeps the field present so downstream
				// invariants (related-field checks, type-of) hold.
				res[name] = placeholderForSchema(propSchema, name)
				continue
			}
			res[name] = name
		}
	}

	if schema.Discriminator != nil {
		name := schema.Discriminator.PropertyName
		if _, ok := res[name]; !ok && name != "" {
			res[name] = name
		}
	}

	minNeeded := 0
	if schema.MinProperties != nil && *schema.MinProperties > 0 {
		need := int(*schema.MinProperties) - len(res)
		if need > 0 {
			minNeeded = need
		}
	}

	if schema.AdditionalProperties != nil || minNeeded > 0 {
		numAdditional := 3
		if minNeeded > numAdditional {
			numAdditional = minNeeded
		}
		if schema.AdditionalProperties == nil {
			numAdditional = minNeeded
		}

		if schema.MaxProperties != nil && *schema.MaxProperties > 0 {
			remaining := int(*schema.MaxProperties) - len(res)
			if remaining < numAdditional {
				numAdditional = remaining
			}
		}

		f := faker.New()
		startLen := len(res)
		maxAttempts := numAdditional*3 + 5
		for attempts := 0; len(res)-startLen < numAdditional && attempts < maxAttempts; attempts++ {
			name := f.Music().Genre()
			name = strings.ToLower(name)
			name = strings.SplitN(name, " ", 2)[0]
			if _, exists := res[name]; exists {
				continue
			}
			var value any
			if schema.AdditionalProperties != nil {
				s := state.NewFrom(state).WithOptions(replacer.WithName(name))
				value = generateContentFromSchema(schema.AdditionalProperties, valueReplacer, s)
			} else {
				// Codegen falls back to `map[string]any` (no
				// AdditionalProperties set) when it can't model the value
				// schema. Many specs declare the value as an object or
				// omit type; an empty object satisfies both cases and
				// never produces a string-where-object-was-expected
				// validation error.
				value = map[string]any{}
			}
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

	// Default to one item so arrays without size constraints aren't empty
	// (an empty array passes type validation but loses signal in the mock
	// response). MinItems raises the floor; MaxItems caps the ceiling.
	// Both are honored so specs declaring `maxItems: 0` (singleton
	// sentinels, deprecated-empty arrays) get an empty array instead of
	// failing validation with "maxItems: got 1, want 0".
	take := 1
	if schema.MinItems != nil && *schema.MinItems > 0 {
		take = int(*schema.MinItems)
	}
	if schema.MaxItems != nil {
		if maxx := int(*schema.MaxItems); maxx < take {
			take = maxx
		}
	}

	if take == 0 {
		return []any{}
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

// placeholderForSchema generates content from the given schema with type `object`.
func placeholderForSchema(s *schema.Schema, name string) any {
	if s == nil {
		return name
	}
	switch s.Type {
	case types.TypeInteger, types.TypeNumber:
		return 0
	case types.TypeBoolean:
		return false
	case types.TypeArray:
		return []any{}
	case types.TypeObject:
		return map[string]any{}
	default:
		if len(s.Enum) > 0 {
			for _, v := range s.Enum {
				if v != nil && v != "null" {
					return v
				}
			}
		}
		return ""
	}
}

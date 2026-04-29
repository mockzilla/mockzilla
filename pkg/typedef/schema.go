package typedef

import (
	"log/slog"
	"strconv"
	"strings"
	"unsafe"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
)

type schemaContext struct {
	cache             map[string]*schema.Schema
	depthTrack        map[string]int
	maxRecursionDepth int

	// expandingUnions tracks union types currently being expanded.
	// This is used to detect circular references through allOf.
	// For example: CounterPartyResponse -> VendorDetailsResponse -> (allOf) -> CounterPartyResponse
	expandingUnions map[string]bool

	// schemaToTypeName is a reverse lookup from schema pointer to type name.
	// This avoids O(n) lookup in tdLookUp for every schema.
	schemaToTypeName map[uintptr]string
}

func newSchemaFromGoSchema(goSchema *codegen.GoSchema, tdLookUp map[string]*codegen.TypeDefinition, maxRecursionDepth int) *schema.Schema {
	// Build reverse lookup once
	schemaToTypeName := make(map[uintptr]string, len(tdLookUp))
	for name, td := range tdLookUp {
		ptr := uintptr(unsafe.Pointer(&td.Schema))
		schemaToTypeName[ptr] = name
	}

	ctx := &schemaContext{
		cache:             make(map[string]*schema.Schema),
		depthTrack:        make(map[string]int),
		maxRecursionDepth: maxRecursionDepth,
		expandingUnions:   make(map[string]bool),
		schemaToTypeName:  schemaToTypeName,
	}
	return newSchemaFromGoSchemaWithContext(goSchema, tdLookUp, ctx)
}

func newSchemaFromGoSchemaWithContext(goSchema *codegen.GoSchema, tdLookUp map[string]*codegen.TypeDefinition, ctx *schemaContext) *schema.Schema {
	key := schemaCacheKey(goSchema)

	// Check recursion depth BEFORE cache to ensure we don't return incomplete
	// placeholders when we've exceeded the recursion limit
	//
	// Only increment depth for schemas that have properties (actual type definitions).
	// References (RefType without properties) will be expanded to their type definitions,
	// and we don't want to double-count them.
	// Also treat GoType references (e.g., "Node" without properties) as references
	// since they will be expanded via tdLookUp.
	isReference := (goSchema.RefType != "" || goSchema.GoType != "") && len(goSchema.Properties) == 0 && len(goSchema.UnionElements) == 0
	slog.Debug("schema processing",
		"key", key, "isReference", isReference, "goType", goSchema.GoType, "refType", goSchema.RefType,
		"numProps", len(goSchema.Properties))

	// Check if this schema is a type definition (i.e., its pointer matches a type in tdLookUp)
	// or if its GoType matches a type name in tdLookUp
	// If so, skip pointer-based depth tracking because reference tracking handles it
	isTypeDefinition := false
	typeDefName := ""

	// Use reverse lookup (O(1)) instead of iterating all type definitions (O(n))
	ptr := uintptr(unsafe.Pointer(goSchema))
	if name, ok := ctx.schemaToTypeName[ptr]; ok {
		isTypeDefinition = true
		typeDefName = name
	}

	// Also check if GoType matches a type name in tdLookUp
	// This handles the case where we're processing a schema that's a copy of a type definition
	if !isTypeDefinition && goSchema.GoType != "" && len(goSchema.Properties) > 0 {
		if _, ok := tdLookUp[goSchema.GoType]; ok {
			isTypeDefinition = true
			typeDefName = goSchema.GoType
		}
	}

	// For type definitions, we need to track that we're processing this type
	// so that reference lookups can detect recursion.
	// We use a special "processing:" prefix to distinguish from "ref:" depth tracking.
	// The "ref:" depth is incremented in the reference lookup, not here.
	if isTypeDefinition && typeDefName != "" {
		processingKey := "processing:" + typeDefName
		if ctx.depthTrack[processingKey] > 0 {
			// We're already processing this type - this is a recursion
			// But we don't block here - we let the reference lookup handle it
			// This is just for tracking
		} else {
			ctx.depthTrack[processingKey]++
			defer func() {
				ctx.depthTrack[processingKey]--
			}()
		}
	}

	if key != "" && !isReference && !isTypeDefinition {
		currentDepth := ctx.depthTrack[key]
		slog.Debug("depth check",
			"key", key, "currentDepth", currentDepth, "maxDepth", ctx.maxRecursionDepth,
			"threshold", ctx.maxRecursionDepth+1)
		// currentDepth 0 = first occurrence (not a recursion)
		// currentDepth 1 = first recursion
		// currentDepth 2 = second recursion, etc.
		if currentDepth >= ctx.maxRecursionDepth+1 {
			// Max depth exceeded, return a placeholder schema marked as recursive
			// This allows the content generator to detect and skip this property
			// NOTE: We do NOT cache this placeholder because:
			// 1. The original schema processing is still in progress and will update the cache
			// 2. We want each recursive reference to get its own placeholder
			slog.Debug("returning recursive placeholder", "key", key)
			return &schema.Schema{Recursive: true}
		}

		// Increment depth for this type
		ctx.depthTrack[key]++
		defer func() {
			ctx.depthTrack[key]--
		}()
	}

	// Check cache - if we've already converted this schema, return it
	// This must come AFTER the depth check to avoid returning incomplete placeholders
	// Skip cache for references - they need to go through expansion to get proper depth tracking
	// Also skip cache for type definitions - they're handled by reference tracking
	if key != "" && !isReference && !isTypeDefinition {
		if cached, exists := ctx.cache[key]; exists {
			return cached
		}
	}

	inner := goSchema.OpenAPISchema

	// Handle oneOf/anyOf union types - pick the first element
	// BUT: skip this for array types - the union applies to the items, not the array itself
	// ALSO: skip if GoType is map[string]any - oapi-codegen simplifies type arrays like
	// [string, object, null] to map[string]any, and we should generate an object, not pick from union
	isArrayType := goSchema.ArrayType != nil || (goSchema.GoType != "" && strings.HasPrefix(goSchema.GoType, "[]"))
	isMapStringAny := goSchema.GoType == "map[string]any" || goSchema.GoType == "map[string]interface{}"
	if len(goSchema.UnionElements) > 0 && !isArrayType && !isMapStringAny {
		// Pick the first element of the union.
		// We always pick the first element because:
		// 1. For primitive unions (e.g., oneOf: [integer, string]), the generated Go type
		//    is Either[A, B] which can unmarshal either type. Generating a value of the
		//    first type will work.
		// 2. For object unions, we need to generate a valid object that matches one of
		//    the union types.
		// Previously we simplified primitive unions to 'any', but this caused issues
		// because the generator would produce {} which can't be unmarshaled into Either[A, B].
		firstElement := goSchema.UnionElements[0]
		if firstElement.TypeName != "" {
			if td, ok := tdLookUp[firstElement.TypeName]; ok {
				// Check recursion depth for the first element (not current schema)
				// Allow up to 3 levels of oneOf expansion to handle nested unions
				firstElementKey := firstElement.TypeName
				if ctx.depthTrack[firstElementKey] < 3 {
					expandedSchema := newSchemaFromGoSchemaWithContext(&td.Schema, tdLookUp, ctx)

					// If there's a discriminator, set the discriminator property's enum
					// to the value that maps to the first element
					if goSchema.Discriminator != nil && expandedSchema != nil {
						discriminatorValue := findDiscriminatorValue(goSchema.Discriminator, firstElement.TypeName)
						if discriminatorValue != "" {
							discProp := goSchema.Discriminator.Property
							if expandedSchema.Properties == nil {
								expandedSchema.Properties = make(map[string]*schema.Schema)
							}
							if propSchema, ok := expandedSchema.Properties[discProp]; ok && propSchema != nil {
								// Property exists - set enum to only the valid discriminator value
								propSchema.Enum = []any{discriminatorValue}
							} else {
								// Property doesn't exist - add it with the discriminator value
								// This handles cases where the discriminator property is not defined
								// in the schema (e.g., Linode's x-linode-ref-name discriminator)
								expandedSchema.Properties[discProp] = &schema.Schema{
									Type: types.TypeString,
									Enum: []any{discriminatorValue},
								}
							}
						}
					}

					return expandedSchema
				}
				// If we've hit the limit, just use the schema directly without recursion
				goSchema = &td.Schema
			} else {
				// firstElement is not in tdLookUp - could be a primitive, inline object,
				// nested union, or any other schema. Recursively process it to handle
				// all cases including constraints, nested unions, properties, etc.
				expanded := newSchemaFromGoSchemaWithContext(&firstElement.Schema, tdLookUp, ctx)
				if expanded != nil {
					return expanded
				}
				// Fall through if expansion failed
			}
		}
	}

	// Check if this is a oneOf/anyOf wrapper (has a single embedded property)
	// If so, unwrap it by looking up the actual union type
	// BUT: skip this for array types - the wrapper belongs to the items, not the array itself
	if len(goSchema.Properties) == 1 && len(goSchema.UnionElements) == 0 && !isArrayType {
		prop := goSchema.Properties[0]
		// Check if this is an embedded field (empty JsonFieldName) with a reference
		if prop.JsonFieldName == "" && prop.Schema.RefType != "" {
			refType := prop.Schema.RefType

			// Check if we're already expanding this union type (circular reference through allOf)
			// For example: CounterPartyResponse -> VendorDetailsResponse -> (allOf) -> CounterPartyResponse
			// In this case, return an empty object schema to break the cycle.
			// This happens when oapi-codegen generates an embedded type for allOf instead of
			// merging properties. The embedded type creates a circular reference that we need to break.
			if ctx.expandingUnions[refType] {
				slog.Debug("breaking circular union reference through allOf", "type", refType)
				// Return an empty object schema - this allows the parent schema to be valid
				// even though we can't expand the circular reference
				return &schema.Schema{
					Type:       types.TypeObject,
					Properties: make(map[string]*schema.Schema),
				}
			}

			// Follow the reference chain to find the ultimate union type
			if unionSchema := findUnionSchema(refType, tdLookUp); unionSchema != nil {
				// Track reference lookups to prevent infinite loops
				refKey := "ref:" + refType
				refDepth := ctx.depthTrack[refKey]
				if refDepth > ctx.maxRecursionDepth {
					slog.Debug("returning recursive placeholder for union reference", "type", refType)
					return &schema.Schema{Recursive: true}
				}
				ctx.depthTrack[refKey]++

				// Mark both the union type AND the parent type as being expanded
				// This handles circular references through allOf composition patterns like:
				// OriginatingAccountResponse -> OriginatingAccountResponse_OneOf
				//   -> BrexCashAccountDetailsResponse -> (allOf) -> OriginatingAccountResponse
				// Without tracking the parent type, the circular reference back to
				// OriginatingAccountResponse would not be detected.
				ctx.expandingUnions[refType] = true
				if typeDefName != "" {
					ctx.expandingUnions[typeDefName] = true
				}

				expanded := newSchemaFromGoSchemaWithContext(unionSchema, tdLookUp, ctx)

				delete(ctx.expandingUnions, refType)
				if typeDefName != "" {
					delete(ctx.expandingUnions, typeDefName)
				}

				ctx.depthTrack[refKey]--

				if key != "" {
					ctx.cache[key] = expanded
				}
				return expanded
			}
		}
	}

	// If GoType or RefType references a type definition (not a primitive), expand it
	// The cache prevents infinite loops for circular references
	typeToLookup := goSchema.RefType
	if typeToLookup == "" {
		typeToLookup = goSchema.GoType
	}

	if typeToLookup != "" && len(goSchema.UnionElements) == 0 && len(goSchema.Properties) == 0 {
		// Check if this is a reference to a type definition (not a primitive or built-in type)
		isPrimitive := false
		switch typeToLookup {
		case "string", "int", "int32", "int64", "float32", "float64", "bool", "any", "interface{}":
			isPrimitive = true
		}

		if !isPrimitive {
			// Not a primitive, check if it's in tdLookup
			if td, ok := tdLookUp[typeToLookup]; ok {
				// Check if we're already expanding this type as part of a union
				// This handles circular references through allOf composition patterns like:
				// OriginatingAccountResponse -> OriginatingAccountResponse_OneOf
				//   -> BrexCashAccountDetailsResponse -> (allOf) -> OriginatingAccountResponse
				// In this case, return an empty object schema to break the cycle.
				if ctx.expandingUnions[typeToLookup] {
					slog.Debug("breaking circular reference through allOf (type lookup)", "type", typeToLookup)
					return &schema.Schema{
						Type:       types.TypeObject,
						Properties: make(map[string]*schema.Schema),
					}
				}

				// Track reference lookups to prevent infinite loops for circular references
				// This is critical because reference schemas (isReference=true) skip the
				// normal depth tracking at line 45-67. Without this, A -> B -> A loops forever.
				refKey := "ref:" + typeToLookup
				refDepth := ctx.depthTrack[refKey]

				// Also check if we're already processing this type (started from a type definition)
				// If so, this lookup is a recursion back to the same type
				processingKey := "processing:" + typeToLookup
				isRecursion := ctx.depthTrack[processingKey] > 0

				// Calculate effective depth:
				// - refDepth counts the number of reference lookups to this type
				// - If we're recursing back to a type we're already processing (isRecursion),
				//   and this is the first reference lookup (refDepth == 0), count it as depth 1
				// - Otherwise, use refDepth as the depth
				effectiveDepth := refDepth
				if isRecursion && refDepth == 0 {
					effectiveDepth = 1
				}

				if effectiveDepth > ctx.maxRecursionDepth {
					slog.Debug("returning recursive placeholder for reference", "type", typeToLookup, "depth", effectiveDepth)
					return &schema.Schema{Recursive: true}
				}
				ctx.depthTrack[refKey]++
				expanded := newSchemaFromGoSchemaWithContext(&td.Schema, tdLookUp, ctx)
				ctx.depthTrack[refKey]--

				// Only cache if not a recursive placeholder
				// Recursive placeholders should not be cached because:
				// 1. The original schema processing is still in progress
				// 2. Caching would overwrite the placeholder being built
				if expanded != nil && key != "" && !expanded.Recursive {
					ctx.cache[key] = expanded
				}
				return expanded
			}
		}
	}

	// Create placeholder in cache BEFORE processing to handle circular references
	// This prevents infinite loops when A references B and B references A
	// We only create the placeholder if we didn't return early above
	placeholder := &schema.Schema{}
	if key != "" {
		ctx.cache[key] = placeholder
	}

	var (
		typ           string
		examples      []any
		example       any
		def           any
		items         *schema.Schema
		enums         []any
		multipleOf    *float64
		minimum       *float64
		maximum       *float64
		minLength     *int64
		maxLength     *int64
		pattern       string
		format        string
		maxItems      *int64
		minItems      *int64
		maxProperties *int64
		minProperties *int64
		required      []string
		nullable      *bool
		readOnly      *bool
		writeOnly     *bool
		deprecated    *bool
	)

	if inner != nil {
		if len(inner.Type) > 0 {
			for _, t := range inner.Type {
				if strings.ToLower(t) != "null" {
					typ = t
					break
				}
			}
		}
		multipleOf = inner.MultipleOf
		minLength = inner.MinLength
		maxLength = inner.MaxLength
		pattern = inner.Pattern
		format = inner.Format
		maxItems = inner.MaxItems
		minItems = inner.MinItems
		maxProperties = inner.MaxProperties
		minProperties = inner.MinProperties
		required = inner.Required
		nullable = inner.Nullable
		readOnly = inner.ReadOnly
		writeOnly = inner.WriteOnly
		deprecated = inner.Deprecated
		if inner.Enum != nil {
			for _, e := range inner.Enum {
				// Convert enum value based on schema type
				enums = append(enums, convertEnumValue(e.Value, typ))
			}
		}
	}

	// Use constraints from goSchema.Constraints first, then fall back to OpenAPISchema
	// This is needed because component references (e.g., $ref: "#/components/schemas/PlayerID")
	// don't have Constraints populated, but the OpenAPISchema has the minimum/maximum values.
	minimum = goSchema.Constraints.Min
	maximum = goSchema.Constraints.Max
	if minimum == nil && inner != nil && inner.Minimum != nil {
		minimum = inner.Minimum
	}
	if maximum == nil && inner != nil && inner.Maximum != nil {
		maximum = inner.Maximum
	}

	properties := make(map[string]*schema.Schema)

	if inner != nil && inner.Examples != nil {
		for _, ex := range inner.Examples {
			examples = append(examples, ex.Value)
		}
	}

	if inner != nil && inner.Example != nil {
		example = inner.Example.Value
	}

	if inner != nil && inner.Default != nil {
		def = inner.Default.Value
	}

	if goSchema.ArrayType != nil {
		// If ArrayType has UnionElements (oneOf/anyOf), pick the first one
		if len(goSchema.ArrayType.UnionElements) > 0 {
			// Try to find the first union element in type definitions
			firstElemName := goSchema.ArrayType.UnionElements[0].TypeName
			if firstElemTd, ok := tdLookUp[firstElemName]; ok {
				items = newSchemaFromGoSchemaWithContext(&firstElemTd.Schema, tdLookUp, ctx)
			} else {
				// If not found in tdLookUp, it's an inline type - use the ArrayType's OpenAPISchema
				items = newSchemaFromGoSchemaWithContext(goSchema.ArrayType, tdLookUp, ctx)
			}
		} else {
			// Check if ArrayType is a union wrapper (single embedded property pointing to a union)
			// BUT: skip this for nested arrays - the wrapper belongs to the innermost items, not the array itself
			arrayType := goSchema.ArrayType
			arrayTypeIsArray := arrayType.ArrayType != nil || (arrayType.GoType != "" && strings.HasPrefix(arrayType.GoType, "[]"))
			if len(arrayType.Properties) == 1 && len(arrayType.UnionElements) == 0 && !arrayTypeIsArray {
				prop := arrayType.Properties[0]
				if prop.JsonFieldName == "" && prop.Schema.RefType != "" {
					if refTd, ok := tdLookUp[prop.Schema.RefType]; ok {
						if len(refTd.Schema.UnionElements) > 0 {
							// Unwrap to the union type
							arrayType = &refTd.Schema
						}
					}
				}
			}
			items = newSchemaFromGoSchemaWithContext(arrayType, tdLookUp, ctx)
		}
	} else if strings.HasPrefix(goSchema.GoType, "[]") {
		// Fallback: if ArrayType is nil but GoType starts with "[]", infer array type from GoType
		// This handles cases where codegen sets GoType to "[]TypeName" but doesn't set ArrayType
		elemTypeName := strings.TrimPrefix(goSchema.GoType, "[]")
		if td, ok := tdLookUp[elemTypeName]; ok {
			// Track depth for the element type name to detect recursion
			currentDepth := ctx.depthTrack[elemTypeName]
			if currentDepth >= ctx.maxRecursionDepth+1 {
				items = &schema.Schema{Recursive: true}
			} else {
				ctx.depthTrack[elemTypeName]++
				items = newSchemaFromGoSchemaWithContext(&td.Schema, tdLookUp, ctx)
				ctx.depthTrack[elemTypeName]--
			}
		} else if strings.HasPrefix(elemTypeName, "[]") {
			// Nested array (e.g., [][]T) - recursively handle
			nestedSchema := &codegen.GoSchema{GoType: elemTypeName}
			items = newSchemaFromGoSchemaWithContext(nestedSchema, tdLookUp, ctx)
		} else {
			// Primitive or inline struct type
			openAPIType := types.GoTypeToOpenAPIType(elemTypeName)
			if openAPIType == types.TypeObject && strings.HasPrefix(elemTypeName, "struct") {
				// Inline struct - extract properties from ArrayType if available
				// For now, treat as object with any properties
				items = &schema.Schema{Type: types.TypeObject}
			} else {
				items = &schema.Schema{Type: openAPIType}
			}
		}
	}

	var additionalProperties *schema.Schema

	if goSchema.AdditionalPropertiesType != nil {
		// AdditionalPropertiesType is set by codegen for both:
		// 1. Objects with properties AND additionalProperties (HasAdditionalProperties=true)
		// 2. Objects with ONLY additionalProperties, rendered as map[string]T (HasAdditionalProperties=false)

		additionalProperties = newSchemaFromGoSchemaWithContext(goSchema.AdditionalPropertiesType, tdLookUp, ctx)
	}

	// Track required fields from embedded schemas (allOf composition)
	var embeddedRequired []string

	if len(goSchema.Properties) > 0 {
		// Determine the target for properties
		// If this schema represents an array (not an object with an array property),
		// then properties should go to the items schema
		targetProperties := properties
		if items != nil && strings.HasPrefix(goSchema.GoType, "[]") {
			if items.Properties == nil {
				items.Properties = make(map[string]*schema.Schema)
			}
			targetProperties = items.Properties
		}

		for _, p := range goSchema.Properties {
			propSchema := newSchemaFromGoSchemaWithContext(&p.Schema, tdLookUp, ctx)
			if propSchema == nil {
				continue
			}
			// Skip array properties with nil items (indicates recursion limit hit for items)
			if propSchema.Type == types.TypeArray && propSchema.Items == nil {
				continue
			}

			// Apply property-level constraints (ReadOnly/WriteOnly) which take precedence
			// over the referenced schema's values.
			if p.Constraints.ReadOnly != nil && *p.Constraints.ReadOnly {
				propSchema.ReadOnly = true
				propSchema.WriteOnly = false
			}
			if p.Constraints.WriteOnly != nil && *p.Constraints.WriteOnly {
				propSchema.WriteOnly = true
				propSchema.ReadOnly = false
			}

			if p.JsonFieldName == "" || p.JsonFieldName == "-" {
				promoteProperties(propSchema, targetProperties)
				// Collect required fields from embedded schemas (allOf composition)
				// This ensures that required fields from $ref schemas in allOf are propagated
				embeddedRequired = append(embeddedRequired, propSchema.Required...)
			} else {
				targetProperties[p.JsonFieldName] = propSchema
			}
		}
	}

	// Merge embedded required fields with the parent's required fields
	if len(embeddedRequired) > 0 {
		required = mergeRequired(required, embeddedRequired)
	}

	if inner != nil && len(inner.Type) > 0 {
		for _, t := range inner.Type {
			if strings.ToLower(t) != "null" {
				typ = t
				break
			}
		}
	}

	// Handle struct{} - oapi-codegen generates this for empty schemas {}
	// Convert to "any" so the generator creates empty objects {} that can be unmarshaled
	if goSchema.GoType == "struct{}" {
		typ = "any"
	}

	// Handle map[string]any - oapi-codegen generates this for type arrays like [string, object, null]
	// or for objects with additionalProperties but no explicit properties.
	// We should generate an object, not pick from the type array.
	if goSchema.GoType == "map[string]any" || goSchema.GoType == "map[string]interface{}" {
		typ = types.TypeObject
	}

	// Infer type from GoType if not set from OpenAPI schema
	// This handles primitive union elements (e.g., type: [integer, boolean]) where
	// the GoSchema has GoType="int64" but no OpenAPISchema.
	if typ == "" && goSchema.GoType != "" {
		inferredType := types.GoTypeToOpenAPIType(goSchema.GoType)
		// Only use inferred type for primitives, not for objects (which would be custom types)
		if inferredType != types.TypeObject {
			typ = inferredType
		}
	}

	// Infer if missing or fix incorrect type
	// Sometimes codegen sets type=array for schemas that are actually objects with properties
	if typ == "" || (typ == types.TypeArray && items == nil && len(properties) > 0) {
		switch {
		case items != nil:
			typ = types.TypeArray
		case len(properties) > 0:
			typ = types.TypeObject
		case additionalProperties != nil:
			// Schema with additionalProperties but no explicit properties is a map (object type)
			typ = types.TypeObject
		default:
			// Only default to string if typ is truly empty
			// Don't override if typ was already set from OpenAPI schema (e.g., "array")
			if typ == "" {
				typ = types.TypeString
			}
		}
	}

	res := &schema.Schema{
		Type:                 typ,
		Examples:             examples,
		Items:                items,
		MultipleOf:           multipleOf,
		Maximum:              maximum,
		Minimum:              minimum,
		MaxLength:            maxLength,
		MinLength:            minLength,
		Pattern:              pattern,
		Format:               format,
		MaxItems:             maxItems,
		MinItems:             minItems,
		MaxProperties:        maxProperties,
		MinProperties:        minProperties,
		Required:             required,
		Enum:                 enums,
		Properties:           properties,
		Default:              def,
		Nullable:             deref(nullable),
		ReadOnly:             deref(readOnly),
		WriteOnly:            deref(writeOnly),
		Example:              example,
		Deprecated:           deref(deprecated),
		AdditionalProperties: additionalProperties,
	}

	// Update the placeholder in cache with the actual result
	// This handles circular references: if something referenced this schema
	// while we were building it, it got the placeholder, which we now update
	if key != "" {
		if placeholder, exists := ctx.cache[key]; exists {
			// Update the placeholder in-place so any references to it get the real data
			*placeholder = *res
		} else {
			// Shouldn't happen, but just in case
			ctx.cache[key] = res
		}
	}

	return res
}

func promoteProperties(schema *schema.Schema, properties map[string]*schema.Schema) {
	if schema == nil {
		return
	}

	for k, v := range schema.Properties {
		properties[k] = v
	}
}

// mergeRequired merges two slices of required field names, removing duplicates.
func mergeRequired(base, additional []string) []string {
	if len(additional) == 0 {
		return base
	}
	if len(base) == 0 {
		return additional
	}

	// Use a map to track unique values
	seen := make(map[string]bool, len(base)+len(additional))
	result := make([]string, 0, len(base)+len(additional))

	for _, r := range base {
		if !seen[r] {
			seen[r] = true
			result = append(result, r)
		}
	}
	for _, r := range additional {
		if !seen[r] {
			seen[r] = true
			result = append(result, r)
		}
	}

	return result
}

// deref removes pointer from value.
func deref[T any](v *T) T {
	var zero T
	if v == nil {
		return zero
	}

	return *v
}

// convertEnumValue converts an enum value string to the appropriate type based on the schema type.
// For integer/number types, it parses the string as a number.
// For string types, it ensures the value is returned as a string (even if YAML parsed it as a number).
// For other types, it returns the value as-is.
func convertEnumValue(value string, schemaType string) any {
	switch schemaType {
	case types.TypeInteger:
		// Try to parse as int64
		if i, err := strconv.ParseInt(value, 10, 64); err == nil {
			return i
		}
		// Handle enum values like "0 (User)" or "101 (EastAsia)" - extract the integer prefix
		if numStr := extractLeadingNumber(value); numStr != "" {
			if i, err := strconv.ParseInt(numStr, 10, 64); err == nil {
				return i
			}
		}
		// If parsing fails, return the string (might be a reference or invalid)
		return value
	case types.TypeNumber:
		// Try to parse as float64
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
		// Handle enum values like "0 (User)" - extract the number prefix
		if numStr := extractLeadingNumber(value); numStr != "" {
			if f, err := strconv.ParseFloat(numStr, 64); err == nil {
				return f
			}
		}
		// If parsing fails, return the string
		return value
	case types.TypeBoolean:
		// Try to parse as bool
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
		// If parsing fails, return the string
		return value
	case types.TypeString:
		// For string types, always return as string
		// This ensures that enum values like '0', '1' are kept as strings
		// even if the YAML parser converted them to integers
		return value
	default:
		// For other types, return as-is
		return value
	}
}

// extractLeadingNumber extracts the leading number from a string like "101 (EastAsia)" -> "101"
func extractLeadingNumber(s string) string {
	var numStr string
	for _, r := range s {
		if r >= '0' && r <= '9' || r == '-' || r == '.' {
			numStr += string(r)
		} else {
			break
		}
	}
	return numStr
}

func schemaCacheKey(s *codegen.GoSchema) string {
	// If this is a reference without properties, use RefType as the key
	// This allows caching of simple references like "Sitegroup"
	// But if the schema has properties (i.e., it's the actual type definition),
	// we should use a different key to avoid conflicts with references
	if s.RefType != "" && len(s.Properties) == 0 {
		return s.RefType
	}

	// Skip primitives - don't cache
	// Primitives should not be cached because they may have different constraints
	// (e.g., two int32 fields with different min/max values)
	switch s.GoType {
	case "string", "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64", "bool", "any":
		return ""
	}

	// Skip external refs (e.g., uuid.UUID, time.Time) - don't cache
	// External refs may have different readOnly/writeOnly constraints in different contexts
	// Check both RefType and GoType for external package references (contains ".")
	if s.IsExternalRef() || strings.Contains(s.GoType, ".") {
		return ""
	}

	// For inline types (type:, struct, []), use the pointer address as cache key
	// This enables recursion tracking for inline schemas to prevent stack overflow
	// Each unique inline schema gets its own cache entry based on its memory address
	if strings.HasPrefix(s.GoType, "type:") || strings.HasPrefix(s.GoType, "struct ") || strings.HasPrefix(s.GoType, "[]") {
		return strconv.FormatUint(uint64(uintptr(unsafe.Pointer(s))), 10)
	}

	return s.GoType
}

func inferType(goSchema *codegen.GoSchema) string {
	if goSchema.OpenAPISchema != nil && len(goSchema.OpenAPISchema.Type) > 0 {
		for _, t := range goSchema.OpenAPISchema.Type {
			if strings.ToLower(t) != "null" {
				return t
			}
		}
	}
	return types.TypeObject
}

// findDiscriminatorValue finds the discriminator value that maps to the given type name.
// Returns empty string if not found.
func findDiscriminatorValue(discriminator *codegen.Discriminator, typeName string) string {
	if discriminator == nil || discriminator.Mapping == nil {
		return ""
	}
	for value, goType := range discriminator.Mapping {
		if goType == typeName {
			return value
		}
	}
	return ""
}

// findUnionSchema follows a reference chain to find the ultimate union schema.
// It handles cases like: allOf wrapper -> OpponentID -> OpponentID_AnyOf
// where the actual union (with UnionElements) is nested inside wrapper types.
// Returns nil if no union is found in the chain.
func findUnionSchema(refType string, tdLookUp map[string]*codegen.TypeDefinition) *codegen.GoSchema {
	visited := make(map[string]bool)
	return findUnionSchemaRecursive(refType, tdLookUp, visited)
}

func findUnionSchemaRecursive(refType string, tdLookUp map[string]*codegen.TypeDefinition, visited map[string]bool) *codegen.GoSchema {
	if refType == "" || visited[refType] {
		return nil
	}
	visited[refType] = true

	refTd, ok := tdLookUp[refType]
	if !ok {
		return nil
	}

	// If this type has union elements, we found it
	if len(refTd.Schema.UnionElements) > 0 || refTd.Schema.IsUnionWrapper {
		return &refTd.Schema
	}

	// If this type has a single embedded property with a reference, follow it
	if len(refTd.Schema.Properties) == 1 && len(refTd.Schema.UnionElements) == 0 {
		prop := refTd.Schema.Properties[0]
		if prop.JsonFieldName == "" && prop.Schema.RefType != "" {
			return findUnionSchemaRecursive(prop.Schema.RefType, tdLookUp, visited)
		}
	}

	return nil
}

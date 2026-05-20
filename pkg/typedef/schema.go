package typedef

import (
	"log/slog"
	"strconv"
	"strings"
	"unsafe"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"go.yaml.in/yaml/v4"
)

type schemaContext struct {
	cache             map[string]*schema.Schema
	depthTrack        map[string]int
	maxRecursionDepth int

	// Tracks union types mid-expansion so circular references through allOf can be detected.
	expandingUnions map[string]bool

	// Reverse lookup from schema pointer to type name; avoids O(n) tdLookUp scans.
	schemaToTypeName map[uintptr]string
}

func newSchemaFromGoSchema(goSchema *codegen.GoSchema, tdLookUp map[string]*codegen.TypeDefinition, maxRecursionDepth int) *schema.Schema {
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

	// Check depth before cache so we don't hand out incomplete placeholders past the limit.
	isReference := (goSchema.RefType != "" || goSchema.GoType != "") && len(goSchema.Properties) == 0 && len(goSchema.UnionElements) == 0
	slog.Debug("schema processing",
		"key", key, "isReference", isReference, "goType", goSchema.GoType, "refType", goSchema.RefType,
		"numProps", len(goSchema.Properties))

	isTypeDefinition := false
	typeDefName := ""

	ptr := uintptr(unsafe.Pointer(goSchema))
	if name, ok := ctx.schemaToTypeName[ptr]; ok {
		isTypeDefinition = true
		typeDefName = name
	}

	if !isTypeDefinition && goSchema.GoType != "" && len(goSchema.Properties) > 0 {
		if _, ok := tdLookUp[goSchema.GoType]; ok {
			isTypeDefinition = true
			typeDefName = goSchema.GoType
		}
	}

	// "processing:" prefix distinguishes type-definition tracking from "ref:" lookup depth.
	if isTypeDefinition && typeDefName != "" {
		processingKey := "processing:" + typeDefName
		if ctx.depthTrack[processingKey] == 0 {
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
		if currentDepth >= ctx.maxRecursionDepth+1 {
			// Don't cache: the original schema is still being built and would overwrite this placeholder.
			slog.Debug("returning recursive placeholder", "key", key)
			return &schema.Schema{Recursive: true}
		}

		ctx.depthTrack[key]++
		defer func() {
			ctx.depthTrack[key]--
		}()
	}

	// Cache check must follow the depth check so partial placeholders aren't returned.
	if key != "" && !isReference && !isTypeDefinition {
		if cached, exists := ctx.cache[key]; exists {
			return cached
		}
	}

	inner := goSchema.OpenAPISchema

	// For unions: pick the first element. Primitive unions become Either[A,B] in
	// codegen, so generating any branch's value unmarshals. Skip for arrays (union
	// applies to items) and map[string]any (codegen collapses `[string, object, null]`).
	isArrayType := goSchema.ArrayType != nil || (goSchema.GoType != "" && strings.HasPrefix(goSchema.GoType, "[]"))
	isMapStringAny := goSchema.GoType == "map[string]any" || goSchema.GoType == "map[string]interface{}"
	if len(goSchema.UnionElements) > 0 && !isArrayType && !isMapStringAny {
		firstElement := goSchema.UnionElements[0]
		if firstElement.TypeName != "" {
			if td, ok := tdLookUp[firstElement.TypeName]; ok {
				// Up to 3 nested levels to handle nested unions.
				if ctx.depthTrack[firstElement.TypeName] < 3 {
					expandedSchema := newSchemaFromGoSchemaWithContext(&td.Schema, tdLookUp, ctx)

					if goSchema.Discriminator != nil && expandedSchema != nil {
						discriminatorValue := findDiscriminatorValue(goSchema.Discriminator, firstElement.TypeName)
						if discriminatorValue != "" {
							discProp := goSchema.Discriminator.Property
							if expandedSchema.Properties == nil {
								expandedSchema.Properties = make(map[string]*schema.Schema)
							}
							if propSchema, ok := expandedSchema.Properties[discProp]; ok && propSchema != nil {
								// If a property's existing enum already excludes the discriminator
								// value, the spec is internally inconsistent; leave the enum alone.
								if enumContains(propSchema.Enum, discriminatorValue) || len(propSchema.Enum) == 0 {
									propSchema.Enum = []any{discriminatorValue}
								}
							} else {
								expandedSchema.Properties[discProp] = &schema.Schema{
									Type: types.TypeString,
									Enum: []any{discriminatorValue},
								}
							}
						}
					}

					return expandedSchema
				}
				goSchema = &td.Schema
			} else {
				expanded := newSchemaFromGoSchemaWithContext(&firstElement.Schema, tdLookUp, ctx)
				if expanded != nil {
					return expanded
				}
			}
		}
	}

	// Unwrap oneOf/anyOf wrappers (single embedded property pointing at the union type).
	if len(goSchema.Properties) == 1 && len(goSchema.UnionElements) == 0 && !isArrayType {
		prop := goSchema.Properties[0]
		if prop.JsonFieldName == "" && prop.Schema.RefType != "" {
			refType := prop.Schema.RefType

			// Circular reference through allOf: break the cycle with an empty object.
			if ctx.expandingUnions[refType] {
				slog.Debug("breaking circular union reference through allOf", "type", refType)
				return &schema.Schema{
					Type:       types.TypeObject,
					Properties: make(map[string]*schema.Schema),
				}
			}

			if unionSchema := findUnionSchema(refType, tdLookUp); unionSchema != nil {
				refKey := "ref:" + refType
				refDepth := ctx.depthTrack[refKey]
				if refDepth > ctx.maxRecursionDepth {
					slog.Debug("returning recursive placeholder for union reference", "type", refType)
					return &schema.Schema{Recursive: true}
				}
				ctx.depthTrack[refKey]++

				// Track the parent type too; otherwise a cycle back to it goes undetected.
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

	typeToLookup := goSchema.RefType
	if typeToLookup == "" {
		typeToLookup = goSchema.GoType
	}

	if typeToLookup != "" && len(goSchema.UnionElements) == 0 && len(goSchema.Properties) == 0 {
		isPrimitive := false
		switch typeToLookup {
		case "string", "int", "int32", "int64", "float32", "float64", "bool", "any", "interface{}":
			isPrimitive = true
		}

		if !isPrimitive {
			if td, ok := tdLookUp[typeToLookup]; ok {
				if ctx.expandingUnions[typeToLookup] {
					slog.Debug("breaking circular reference through allOf (type lookup)", "type", typeToLookup)
					return &schema.Schema{
						Type:       types.TypeObject,
						Properties: make(map[string]*schema.Schema),
					}
				}

				// Reference schemas skip the pointer-keyed depth tracking, so a
				// dedicated "ref:" counter prevents A -> B -> A from looping forever.
				refKey := "ref:" + typeToLookup
				refDepth := ctx.depthTrack[refKey]

				processingKey := "processing:" + typeToLookup
				isRecursion := ctx.depthTrack[processingKey] > 0

				// First ref lookup back to a type already being processed counts as depth 1.
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

				// Skip caching recursive placeholders; the in-progress build will overwrite them.
				if expanded != nil && key != "" && !expanded.Recursive {
					ctx.cache[key] = expanded
				}
				return expanded
			}
		}
	}

	// Cache a placeholder before processing so A <-> B cycles resolve to the in-progress entry.
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

	isNull := false
	if inner != nil {
		if len(inner.Type) > 0 {
			for _, t := range inner.Type {
				if strings.ToLower(t) != "null" {
					typ = t
					break
				}
			}
			if typ == "" {
				isNull = true
			}
		}

		// Enum-only allOf overrides leave inner.Type empty; infer it from the YAML tags.
		if typ == "" && len(inner.Enum) > 0 {
			typ = inferTypeFromYAMLNodes(inner.Enum)
		}

		// Lower `const: value` to a single-valued enum.
		if inner.Const != nil && len(inner.Enum) == 0 {
			inner.Enum = []*yaml.Node{inner.Const}
			if typ == "" {
				typ = inferTypeFromYAMLNodes(inner.Enum)
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

		// Walk branches so `required: [X]` inside any allOf[i] is honored,
		// including the malformed case where X isn't declared as a property.
		required = mergeRequired(required, allOfRequired(inner))
		nullable = inner.Nullable
		readOnly = inner.ReadOnly
		writeOnly = inner.WriteOnly
		deprecated = inner.Deprecated
		if inner.Enum != nil {
			for _, e := range inner.Enum {
				enums = append(enums, convertEnumNode(e, typ))
			}
		}

		// oneOf/anyOf alongside a primitive type is the "constrain values" pattern;
		// codegen drops the union, so recover enum values from a type-compatible branch.
		// If the outer type disagrees with the branch types, adopt the branch's type to
		// satisfy the validator's strict oneOf check.
		if len(enums) == 0 && typ != "" {
			if branchEnums, branchType := enumsFromUnionBranches(inner.OneOf, typ); branchEnums != nil {
				enums = branchEnums
				typ = branchType
			} else if branchEnums, branchType := enumsFromUnionBranches(inner.AnyOf, typ); branchEnums != nil {
				enums = branchEnums
				typ = branchType
			}
		}

		// anyOf/oneOf of sibling string patterns: codegen collapses them so the outer
		// schema has no pattern. Pick the first branch's pattern so the generated value
		// satisfies at least that branch (enough for the validator).
		if pattern == "" && typ == types.TypeString {
			if p := firstPatternFromBranches(inner.AnyOf); p != "" {
				pattern = p
			} else if p := firstPatternFromBranches(inner.OneOf); p != "" {
				pattern = p
			}
		}
	}

	// Prefer goSchema.Constraints; component $refs leave them unset so fall back to inner.
	minimum = goSchema.Constraints.Min
	maximum = goSchema.Constraints.Max
	if minimum == nil && inner != nil && inner.Minimum != nil {
		minimum = inner.Minimum
	}
	if maximum == nil && inner != nil && inner.Maximum != nil {
		maximum = inner.Maximum
	}
	if minLength == nil && goSchema.Constraints.MinLength != nil {
		minLength = goSchema.Constraints.MinLength
	}
	if maxLength == nil && goSchema.Constraints.MaxLength != nil {
		maxLength = goSchema.Constraints.MaxLength
	}
	if pattern == "" && goSchema.Constraints.Pattern != nil {
		pattern = *goSchema.Constraints.Pattern
	}
	if minItems == nil && goSchema.Constraints.MinItems != nil {
		minItems = goSchema.Constraints.MinItems
	}
	if maxItems == nil && goSchema.Constraints.MaxItems != nil {
		maxItems = goSchema.Constraints.MaxItems
	}
	if minProperties == nil && goSchema.Constraints.MinProperties != nil {
		minProperties = goSchema.Constraints.MinProperties
	}
	if maxProperties == nil && goSchema.Constraints.MaxProperties != nil {
		maxProperties = goSchema.Constraints.MaxProperties
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
		additionalProperties = newSchemaFromGoSchemaWithContext(goSchema.AdditionalPropertiesType, tdLookUp, ctx)
		// Codegen emits an empty string-typed placeholder when the spec's value schema
		// has an unresolvable proxy (e.g. a $ref whose dotted component name libopenapi
		// can't dereference). Clear it so the generator falls back to an empty object.
		if additionalProperties != nil && additionalPropertiesIsPlaceholder(additionalProperties) &&
			inner != nil && inner.AdditionalProperties != nil && inner.AdditionalProperties.IsA() &&
			(inner.AdditionalProperties.A == nil || inner.AdditionalProperties.A.Schema() == nil) {
			additionalProperties = nil
		}
	}

	var embeddedRequired []string

	if len(goSchema.Properties) > 0 {
		// For array-typed schemas, properties belong on the items, not the outer wrapper.
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
			// Nil items here means the recursion limit was hit while building them.
			if propSchema.Type == types.TypeArray && propSchema.Items == nil {
				continue
			}

			// Codegen flattens an object property with an explicit additionalProperties
			// schema to `map[string]any` when the value schema can't be dereferenced.
			// Recover the value schema from inner, falling back to tdLookUp.
			if propSchema.Type == types.TypeObject && p.JsonFieldName != "" && inner != nil &&
				(propSchema.AdditionalProperties == nil || additionalPropertiesIsPlaceholder(propSchema.AdditionalProperties)) {
				apSchema, apRef := findPropertyAdditionalPropertiesWithRef(inner, p.JsonFieldName)
				if apSchema != nil {
					propSchema.AdditionalProperties = newSchemaFromGoSchemaWithContext(
						&codegen.GoSchema{OpenAPISchema: apSchema}, tdLookUp, ctx)
				} else if apRef != "" {
					if typeName := componentNameFromRef(apRef); typeName != "" {
						if td, ok := tdLookUp[typeName]; ok {
							propSchema.AdditionalProperties = newSchemaFromGoSchemaWithContext(&td.Schema, tdLookUp, ctx)
						}
					}
				}
			}

			// Property-level ReadOnly/WriteOnly override the referenced schema's values.
			if p.Constraints.ReadOnly != nil && *p.Constraints.ReadOnly {
				propSchema.ReadOnly = true
				propSchema.WriteOnly = false
			}
			if p.Constraints.WriteOnly != nil && *p.Constraints.WriteOnly {
				propSchema.WriteOnly = true
				propSchema.ReadOnly = false
			}

			if p.JsonFieldName != "" && p.JsonFieldName != "-" {
				// For array-typed parents, the allOf lives on the items' schema, not the array's.
				allOfSource := inner
				if items != nil && strings.HasPrefix(goSchema.GoType, "[]") &&
					goSchema.ArrayType != nil && goSchema.ArrayType.OpenAPISchema != nil {
					allOfSource = goSchema.ArrayType.OpenAPISchema
				}
				if allOfSource != nil {
					applyAllOfEnumIntersection(propSchema, p.JsonFieldName, allOfSource.AllOf)
				}
			}

			if p.JsonFieldName == "" || p.JsonFieldName == "-" {
				promoteProperties(propSchema, targetProperties)
				embeddedRequired = append(embeddedRequired, propSchema.Required...)
			} else {
				targetProperties[p.JsonFieldName] = propSchema
			}
		}
	}

	if len(embeddedRequired) > 0 {
		required = mergeRequired(required, embeddedRequired)
	}

	// Recover null-only properties: codegen drops them (no Go type), but the spec still
	// requires them in the JSON body, so emit a null at runtime.
	if inner != nil && inner.Properties != nil {
		for k, proxy := range inner.Properties.FromOldest() {
			if _, ok := properties[k]; ok {
				continue
			}
			sub := proxy.Schema()
			if sub == nil || len(sub.Type) == 0 {
				continue
			}
			allNull := true
			for _, t := range sub.Type {
				if strings.ToLower(t) != "null" {
					allNull = false
					break
				}
			}
			if allNull {
				properties[k] = &schema.Schema{IsNull: true}
			}
		}
	}

	// Surface properties and required from a discriminator union nested in allOf.
	// Codegen doesn't merge across the union boundary, so picking the first oneOf
	// branch (matching top-level union expansion) restores the branch-specific fields.
	if inner != nil && len(inner.AllOf) > 0 {
		merged, mergedReq := mergeAllOfUnionProperties(inner, tdLookUp, ctx)
		for k, v := range merged {
			if _, ok := properties[k]; ok {
				continue
			}
			properties[k] = v
		}
		if len(mergedReq) > 0 {
			required = mergeRequired(required, mergedReq)
		}
	}

	if inner != nil && len(inner.Type) > 0 {
		for _, t := range inner.Type {
			if strings.ToLower(t) != "null" {
				typ = t
				break
			}
		}
	}

	// oapi-codegen-dd emits struct{} for empty schemas; "any" lets the generator
	// produce {} which can be unmarshaled. Skip when a type was already inferred.
	if goSchema.GoType == "struct{}" && typ == "" {
		typ = "any"
	}

	// oapi-codegen-dd emits map[string]any for multi-typed schemas and for objects
	// with additionalProperties but no explicit properties; treat as object unless
	// `items` indicates the spec meant an array. Explicit `type: object` wins.
	if goSchema.GoType == "map[string]any" || goSchema.GoType == "map[string]interface{}" {
		explicitObject := inner != nil && len(inner.Type) > 0 && strings.EqualFold(inner.Type[0], types.TypeObject)
		itemsSchema, hasItems := findItemsInSchema(goSchema.OpenAPISchema)
		if items != nil {
			hasItems = true
		}
		if hasItems && !explicitObject {
			typ = types.TypeArray
			if items == nil && itemsSchema != nil {
				items = newSchemaFromGoSchemaWithContext(&codegen.GoSchema{OpenAPISchema: itemsSchema}, tdLookUp, ctx)
			}
		} else {
			typ = types.TypeObject
			items = nil
		}
	}

	// Infer type from GoType when OpenAPISchema is missing (primitive union elements).
	if typ == "" && goSchema.GoType != "" {
		inferredType := types.GoTypeToOpenAPIType(goSchema.GoType)
		if inferredType != types.TypeObject {
			typ = inferredType
		}
	}

	// Infer or correct type; oapi-codegen-dd sometimes sets type=array for object schemas.
	if typ == "" || (typ == types.TypeArray && items == nil && len(properties) > 0) {
		switch {
		case items != nil:
			typ = types.TypeArray
		case len(properties) > 0:
			typ = types.TypeObject
		case additionalProperties != nil:
			typ = types.TypeObject
		default:
			if typ == "" {
				typ = types.TypeString
			}
		}
	}

	// allOf branch with `items` and no outer type: the validator treats it as array.
	if typ != types.TypeArray && items == nil && inner != nil {
		if itemsSchema := findItemsInAllOf(inner); itemsSchema != nil && !innerHasObjectShape(inner) {
			items = newSchemaFromGoSchemaWithContext(&codegen.GoSchema{OpenAPISchema: itemsSchema}, tdLookUp, ctx)
			typ = types.TypeArray
		}
	}

	// Conflicting `type: array` + object-only allOf branches: escape via null when nullable.
	if typ == types.TypeArray && inner != nil && itemsKeywordIsEmpty(inner.Items) &&
		len(inner.AllOf) > 0 && allOfDeclaresOnlyObjects(inner) && deref(nullable) {
		isNull = true
	}

	// `type: array` + `enum: [primitives]` + `items: {}` is unsatisfiable: arrays can
	// never equal scalar enum values. Drop items so the field is omitted.
	if typ == types.TypeArray && len(enums) > 0 && items != nil {
		items = nil
		enums = nil
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

		// oapi-codegen-dd conflates `additionalProperties: false` with "not specified";
		// read the libopenapi DynamicValue so the generator can refuse stray properties.
		AdditionalPropertiesForbidden: hasExplicitAdditionalPropertiesFalse(inner),

		// oapi-codegen-dd only fills Discriminator inside the union pipeline; this
		// fallback covers plain-object schemas with a discriminator and no wrapper.
		Discriminator: discriminatorFromInner(goSchema.Discriminator, inner),

		IsNull: isNull,
	}

	// In-place update of the placeholder so anything that grabbed it mid-build sees the real data.
	if key != "" {
		if placeholder, exists := ctx.cache[key]; exists {
			*placeholder = *res
		} else {
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

func itemsKeywordIsEmpty(items *base.DynamicValue[*base.SchemaProxy, bool]) bool {
	if items == nil {
		return true
	}
	if items.A == nil {
		return true
	}
	sub := items.A.Schema()
	if sub == nil {
		return true
	}
	propsEmpty := sub.Properties == nil || sub.Properties.Len() == 0
	return len(sub.Type) == 0 && propsEmpty &&
		sub.Items == nil && len(sub.AllOf) == 0 && len(sub.OneOf) == 0 && len(sub.AnyOf) == 0
}

// componentNameFromRef extracts the type name from `#/components/schemas/<Name>`,
// stripping dots to match oapi-codegen-dd's tdLookUp keys.
func componentNameFromRef(ref string) string {
	const prefix = "#/components/schemas/"
	if !strings.HasPrefix(ref, prefix) {
		return ""
	}
	name := ref[len(prefix):]
	return strings.ReplaceAll(name, ".", "")
}

func innerHasObjectShape(s *base.Schema) bool {
	if s == nil {
		return false
	}
	for _, t := range s.Type {
		if strings.EqualFold(t, types.TypeObject) {
			return true
		}
	}
	if s.Properties != nil && s.Properties.Len() > 0 {
		return true
	}
	return false
}

func deref[T any](v *T) T {
	var zero T
	if v == nil {
		return zero
	}

	return *v
}

func schemaCacheKey(s *codegen.GoSchema) string {
	if s.RefType != "" && len(s.Properties) == 0 {
		return s.RefType
	}

	// Don't cache primitives: distinct fields can carry distinct constraints.
	switch s.GoType {
	case "string", "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64", "bool", "any":
		return ""
	}

	// Don't cache external refs (uuid.UUID, time.Time, etc.); they can vary by call site.
	if s.IsExternalRef() || strings.Contains(s.GoType, ".") {
		return ""
	}

	// Pointer key for inline types so each unique inline schema tracks recursion on its own.
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

	// Specs that omit `type` next to `items` mean array; recover the intent.
	if goSchema.OpenAPISchema != nil && goSchema.OpenAPISchema.Items != nil {
		return types.TypeArray
	}
	return types.TypeObject
}

// findItemsInSchema returns the items schema, descending into allOf branches when
// the parent omits items (OpenAPI 3.0 `allOf: [{ items: ... }]` without `type: array`).
func findItemsInSchema(s *base.Schema) (*base.Schema, bool) {
	if s == nil {
		return nil, false
	}
	if s.Items != nil && s.Items.A != nil {
		if sub := s.Items.A.Schema(); sub != nil {
			return sub, true
		}
	}
	for _, branch := range s.AllOf {
		if branch == nil {
			continue
		}
		if found, ok := findItemsInSchema(branch.Schema()); ok {
			return found, true
		}
	}
	return nil, false
}

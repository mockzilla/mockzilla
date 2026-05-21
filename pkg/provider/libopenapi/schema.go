package libopenapi

import (
	"fmt"
	"strings"

	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// propertiesMap is libopenapi's ordered properties map shape, aliased
// for brevity at the call sites below.
type propertiesMap = orderedmap.Map[string, *base.SchemaProxy]

// defaultMaxRecursionDepth bounds A->B->A cycle expansion. Mirrors the
// effective bound used by the codegen-based converter.
const defaultMaxRecursionDepth = 3

// convertCtx carries per-conversion state. Two cache keys are needed
// because libopenapi resolves each *base.SchemaProxy to its own copied
// *base.Schema (cached locally on the proxy), so two distinct $ref
// proxies pointing at the same component schema yield distinct
// *base.Schema pointers. To dedupe and to detect cycles, $ref proxies
// are keyed by the $ref string; inline proxies fall back to the
// *base.Schema pointer (which is stable for an inline schema).
type convertCtx struct {
	cache    map[string]*schema.Schema
	depth    map[string]int
	maxDepth int
}

func newConvertCtx() *convertCtx {
	return &convertCtx{
		cache:    map[string]*schema.Schema{},
		depth:    map[string]int{},
		maxDepth: defaultMaxRecursionDepth,
	}
}

// schemaIdentity returns a stable cache/depth key for a SchemaProxy +
// its resolved Schema. $ref proxies use the reference string; inline
// proxies use the *base.Schema pointer formatted as a string. Two
// distinct $ref proxies sharing the same target yield the same key.
func schemaIdentity(proxy *base.SchemaProxy, s *base.Schema) string {
	if proxy != nil && proxy.IsReference() {
		if ref := proxy.GetReference(); ref != "" {
			return "ref:" + ref
		}
	}
	if s == nil {
		return ""
	}
	return fmt.Sprintf("ptr:%p", s)
}

// convertProxy resolves a SchemaProxy and converts it. Returns nil when
// the proxy or its schema is nil. Cycles (A->B->A) are broken by
// returning a Recursive marker on re-entry to a key that is still in
// progress, so the generator sees a finite tree instead of looping.
func convertProxy(proxy *base.SchemaProxy, ctx *convertCtx) *schema.Schema {
	if proxy == nil {
		return nil
	}
	s := proxy.Schema()
	if s == nil {
		return nil
	}

	key := schemaIdentity(proxy, s)

	if cached, ok := ctx.cache[key]; ok {
		return cached
	}

	if ctx.depth[key] > 0 {
		return &schema.Schema{Recursive: true}
	}

	ctx.depth[key]++
	defer func() { ctx.depth[key]-- }()

	res := convertSchema(s, ctx)
	if res == nil {
		return nil
	}
	if key != "" {
		ctx.cache[key] = res
	}
	return res
}

// convertSchema builds a schema.Schema from a libopenapi base.Schema. The
// caller is responsible for the proxy-level cache; this function only
// walks the schema's own structure.
func convertSchema(s *base.Schema, ctx *convertCtx) *schema.Schema {
	if s == nil {
		return nil
	}

	typ, isNull := chooseType(s.Type)

	enums := convertEnumNodes(s.Enum, typ)

	if s.Const != nil && len(enums) == 0 {
		enums = []any{convertScalarNode(s.Const, typ)}
		if typ == "" {
			typ = inferTypeFromNode(s.Const)
		}
	}

	var items *schema.Schema
	if s.Items != nil && s.Items.IsA() {
		items = convertProxy(s.Items.A, ctx)
	}

	properties, embeddedRequired := convertProperties(s.Properties, ctx)
	required := mergeStringLists(s.Required, embeddedRequired)

	var additionalProperties *schema.Schema
	additionalPropertiesForbidden := false
	if s.AdditionalProperties != nil {
		if s.AdditionalProperties.IsB() {
			if !s.AdditionalProperties.B {
				additionalPropertiesForbidden = true
			}
		} else if s.AdditionalProperties.IsA() {
			additionalProperties = convertProxy(s.AdditionalProperties.A, ctx)
		}
	}

	composed := composeSchema(s, ctx)
	if composed != nil {
		// Unsatisfiable spec: outer declares one type, an allOf branch
		// demands another. When the schema is nullable AND the composed
		// branch offered no enum to choose from, the only value that
		// can satisfy everything is JSON null. uploadcare /group's
		// `files` field is the canonical case (type: array + allOf:
		// [{type: object}, ...] + nullable, no enum). Skip the IsNull
		// switch when an enum is present — the generator can pick a
		// branch-typed value that satisfies at least one oneOf path
		// (procurify CreditCard.status, integer + oneOf of string
		// enums, would otherwise emit null and fail every oneOf).
		if typ != "" && composed.typ != "" && typ != composed.typ &&
			derefBool(s.Nullable) && len(composed.enums) == 0 {
			isNull = true
		}
		mergeComposed(&typ, &items, properties, &required, &enums, composed)
	}

	applyAllOfEnumIntersection(properties, s.AllOf)
	applyDiscriminator(properties, s, composed)

	if typ == "" && !isNull {
		typ = inferType(items, properties, additionalProperties, enums)
	}

	if typ == types.TypeString {
		if p := firstPatternFromBranches(s.AnyOf); p != "" && s.Pattern == "" {
			s = withPattern(s, p)
		} else if p := firstPatternFromBranches(s.OneOf); p != "" && s.Pattern == "" {
			s = withPattern(s, p)
		}
	}

	out := &schema.Schema{
		Type:                          typ,
		Items:                         items,
		Properties:                    properties,
		Required:                      required,
		Enum:                          enums,
		AdditionalProperties:          additionalProperties,
		AdditionalPropertiesForbidden: additionalPropertiesForbidden,
		MultipleOf:                    s.MultipleOf,
		Maximum:                       s.Maximum,
		Minimum:                       s.Minimum,
		MaxLength:                     s.MaxLength,
		MinLength:                     s.MinLength,
		Pattern:                       s.Pattern,
		Format:                        s.Format,
		MaxItems:                      s.MaxItems,
		MinItems:                      s.MinItems,
		MaxProperties:                 s.MaxProperties,
		MinProperties:                 s.MinProperties,
		Nullable:                      derefBool(s.Nullable),
		ReadOnly:                      derefBool(s.ReadOnly),
		WriteOnly:                     derefBool(s.WriteOnly),
		Deprecated:                    derefBool(s.Deprecated),
		Default:                       decodeNode(s.Default),
		Example:                       decodeNode(s.Example),
		Examples:                      decodeNodes(s.Examples),
		Discriminator:                 convertDiscriminator(s.Discriminator),
		IsNull:                        isNull,
	}
	applyComposedConstraints(out, composed)

	return out
}

// chooseType picks the first non-null OpenAPI type from the (possibly
// multi-valued) Type field. A type list of just "null" yields IsNull.
func chooseType(t []string) (typ string, isNull bool) {
	if len(t) == 0 {
		return "", false
	}
	for _, v := range t {
		if strings.EqualFold(v, "null") {
			continue
		}
		return v, false
	}
	return "", true
}

// inferType supplies a default type when the spec omits it. Falls back
// to string when nothing else is inferable so the generator has
// something concrete to emit (an unset Type leaves callers with no
// value to produce, which propagates as empty arrays or missing
// properties that fail validation).
func inferType(items *schema.Schema, properties map[string]*schema.Schema, additionalProperties *schema.Schema, enums []any) string {
	switch {
	case items != nil:
		return types.TypeArray
	case len(properties) > 0:
		return types.TypeObject
	case additionalProperties != nil:
		return types.TypeObject
	case len(enums) > 0:
		return inferTypeFromEnum(enums)
	default:
		return types.TypeString
	}
}

func inferTypeFromEnum(enums []any) string {
	for _, e := range enums {
		switch e.(type) {
		case string:
			return types.TypeString
		case int, int64:
			return types.TypeInteger
		case float64:
			return types.TypeNumber
		case bool:
			return types.TypeBoolean
		}
	}
	return types.TypeString
}

// convertProperties walks an ordered properties map and returns the
// converted entries plus any names whose underlying schema declared only
// `type: null` (so the body still emits the key with a JSON null).
func convertProperties(props *propertiesMap, ctx *convertCtx) (map[string]*schema.Schema, []string) {
	if props == nil || props.Len() == 0 {
		return map[string]*schema.Schema{}, nil
	}
	out := make(map[string]*schema.Schema, props.Len())
	var nullOnly []string
	for k, proxy := range props.FromOldest() {
		sub := convertProxy(proxy, ctx)
		if sub == nil {
			if proxy != nil {
				if raw := proxy.Schema(); raw != nil && isAllNullType(raw.Type) {
					out[k] = &schema.Schema{IsNull: true}
					nullOnly = append(nullOnly, k)
				}
			}
			continue
		}
		out[k] = sub
	}
	return out, nullOnly
}

func isAllNullType(t []string) bool {
	if len(t) == 0 {
		return false
	}
	for _, v := range t {
		if !strings.EqualFold(v, "null") {
			return false
		}
	}
	return true
}

func mergeStringLists(a, b []string) []string {
	if len(b) == 0 {
		return a
	}
	if len(a) == 0 {
		return b
	}
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, v := range a {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	for _, v := range b {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func convertDiscriminator(d *base.Discriminator) *schema.Discriminator {
	if d == nil || d.PropertyName == "" {
		return nil
	}
	out := &schema.Discriminator{PropertyName: d.PropertyName}
	if d.Mapping != nil && d.Mapping.Len() > 0 {
		out.Mapping = make(map[string]string, d.Mapping.Len())
		for k, v := range d.Mapping.FromOldest() {
			out.Mapping[k] = v
		}
	}
	return out
}

func derefBool(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}

func decodeNode(n *yaml.Node) any {
	if n == nil {
		return nil
	}
	var v any
	if err := n.Decode(&v); err != nil {
		return nil
	}
	return v
}

func decodeNodes(nodes []*yaml.Node) []any {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]any, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, decodeNode(n))
	}
	return out
}

// withPattern returns a shallow copy of s with Pattern set. Used so we
// can synthesise a string pattern from a oneOf/anyOf branch without
// mutating the libopenapi-owned object.
func withPattern(s *base.Schema, pattern string) *base.Schema {
	copy := *s
	copy.Pattern = pattern
	return &copy
}

package libopenapi

import (
	"strings"

	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/pb33f/libopenapi/datamodel/high/base"
)

// composedShape carries the fields a compose pass needs to merge back
// into the surrounding schema. The fields are additive: nil entries
// mean "compose pass didn't contribute this", letting the caller keep
// whatever the outer schema already declared.
type composedShape struct {
	typ        string
	items      *schema.Schema
	properties map[string]*schema.Schema
	required   []string
	enums      []any

	// Scalar constraints inherited from the picked union branch when
	// the outer schema didn't declare them. Critical for specs whose
	// only constraint lives inside `oneOf` (e.g. `abi_version: oneOf:
	// [{type: integer, maximum: 65535}, {type: string}]`): without
	// this the generator emits an unconstrained int that fails
	// validation against the picked branch.
	minimum    *float64
	maximum    *float64
	multipleOf *float64
	minLength  *int64
	maxLength  *int64
	pattern    string
	format     string
	minItems   *int64
	maxItems   *int64
}

// composeSchema walks allOf, oneOf, and anyOf branches and returns a
// merged shape. Policy:
//   - allOf: walk every branch, union properties + required, recurse.
//   - oneOf / anyOf: pick the first non-null branch (codegen does the
//     same; the generator only emits one variant at runtime).
func composeSchema(s *base.Schema, ctx *convertCtx) *composedShape {
	if s == nil {
		return nil
	}
	if len(s.AllOf) == 0 && len(s.OneOf) == 0 && len(s.AnyOf) == 0 {
		return nil
	}

	out := &composedShape{
		properties: map[string]*schema.Schema{},
	}

	for _, p := range s.AllOf {
		mergeAllOfBranch(p, out, ctx)
	}

	if branch := firstNonNullBranch(s.OneOf); branch != nil {
		mergeUnionBranch(branch, out, ctx)
	} else if branch := firstNonNullBranch(s.AnyOf); branch != nil {
		mergeUnionBranch(branch, out, ctx)
	}

	if len(out.properties) == 0 && len(out.required) == 0 && len(out.enums) == 0 &&
		out.typ == "" && out.items == nil {
		return nil
	}
	return out
}

func mergeAllOfBranch(proxy *base.SchemaProxy, out *composedShape, ctx *convertCtx) {
	if proxy == nil {
		return
	}
	sub := proxy.Schema()
	if sub == nil {
		return
	}
	key := schemaIdentity(proxy, sub)
	if ctx.depth[key] > 0 {
		return
	}
	ctx.depth[key]++
	defer func() { ctx.depth[key]-- }()

	for _, name := range sub.Required {
		out.required = appendUnique(out.required, name)
	}

	if sub.Properties != nil {
		for k, propProxy := range sub.Properties.FromOldest() {
			converted := convertProxy(propProxy, ctx)
			if converted == nil {
				continue
			}

			if existing, exists := out.properties[k]; exists {
				// Both branches declared this property. allOf
				// semantics require the value to satisfy both, so the
				// merged property is the more constraining of the two.
				// Picking the one with more properties + required keys.
				// branch 0 narrows to a base $ref like
				// `{self}` while branch 1 redeclares the same property
				// as a richer $ref like `{self, history, harvest, ...}`
				// whose allOf already contains the base. First-wins
				// loses the richer branch's keys and the generator can
				// no longer satisfy the required list.
				if propertyMoreSpecific(converted, existing) {
					out.properties[k] = converted
				}
				continue
			}
			out.properties[k] = converted
		}
	}

	// A branch like `allOf: [{type: string, enum: [...]}, {nullable: true}]`
	// applies to the surrounding schema itself, not a property of it.
	// Propagate the branch's type and enum when the outer schema and
	// preceding branches left them empty so the generator picks a value
	// the validator will accept.
	if out.typ == "" {
		out.typ = firstNonNullType(sub.Type)
	}
	if len(out.enums) == 0 && len(sub.Enum) > 0 {
		out.enums = convertEnumNodes(sub.Enum, out.typ)
	}

	// Scalar constraint accumulation. allOf semantics require every
	// branch to be satisfied, so when multiple branches declare the
	// same constraint we take the tightest: min of *Length/maximum
	// upper bounds, max of *Length/minimum lower bounds. Pattern and
	// format can't be safely intersected; first non-empty wins, which
	// matches the no-op regex engine policy in
	// internal/portable/runner.go (patterns aren't validated anyway).
	if v := sub.MaxLength; v != nil && (out.maxLength == nil || *v < *out.maxLength) {
		out.maxLength = v
	}
	if v := sub.MinLength; v != nil && (out.minLength == nil || *v > *out.minLength) {
		out.minLength = v
	}
	if v := sub.Maximum; v != nil && (out.maximum == nil || *v < *out.maximum) {
		out.maximum = v
	}
	if v := sub.Minimum; v != nil && (out.minimum == nil || *v > *out.minimum) {
		out.minimum = v
	}
	if v := sub.MaxItems; v != nil && (out.maxItems == nil || *v < *out.maxItems) {
		out.maxItems = v
	}
	if v := sub.MinItems; v != nil && (out.minItems == nil || *v > *out.minItems) {
		out.minItems = v
	}
	if out.multipleOf == nil {
		out.multipleOf = sub.MultipleOf
	}
	if out.pattern == "" {
		out.pattern = sub.Pattern
	}
	if out.format == "" {
		out.format = sub.Format
	}

	for _, nested := range sub.AllOf {
		mergeAllOfBranch(nested, out, ctx)
	}

	// Pure-composition allOf branches like `allOf: [{oneOf: [A, B, C]},
	// {description: ...}]` carry their shape inside their own oneOf/anyOf
	// rather than directly. Without descending into them the branch
	// contributes nothing, and the merged shape ends up with no type or
	// properties; the generator then falls back to a bare string and
	// response validation fails with "got string, want object". Pick the
	// first non-null union branch (matching the policy in composeSchema's
	// own oneOf/anyOf handling).
	hasOwnShape := (sub.Properties != nil && sub.Properties.Len() > 0) ||
		firstNonNullType(sub.Type) != "" || len(sub.Enum) > 0
	if !hasOwnShape {
		if branch := firstNonNullBranch(sub.OneOf); branch != nil {
			mergeUnionBranch(branch, out, ctx)
		} else if branch := firstNonNullBranch(sub.AnyOf); branch != nil {
			mergeUnionBranch(branch, out, ctx)
		}
	}

	if sub.Items != nil && sub.Items.IsA() && out.items == nil {
		if items := convertProxy(sub.Items.A, ctx); items != nil {
			out.items = items
			// Items signal an array; if a prior branch typed this as
			// something else (typical "allOf: [{type: object}, {type:
			// array, items: ...}]" mismatch in real specs), array wins
			// because the items themselves are what the generator needs
			// to produce a satisfiable value. Without this override the
			// generator emits an object and response validation rejects
			// it as "got object, want array".
			if out.typ != types.TypeArray {
				out.typ = types.TypeArray
			}
		}
	}
}

func mergeUnionBranch(proxy *base.SchemaProxy, out *composedShape, ctx *convertCtx) {
	if proxy == nil {
		return
	}
	sub := proxy.Schema()
	if sub == nil {
		return
	}

	for _, name := range sub.Required {
		out.required = appendUnique(out.required, name)
	}

	if sub.Properties != nil {
		for k, propProxy := range sub.Properties.FromOldest() {
			converted := convertProxy(propProxy, ctx)
			if converted == nil {
				continue
			}
			if existing, exists := out.properties[k]; exists {
				// Same merge policy as mergeAllOfBranch: prefer the
				// more specific schema when both branches declare the
				// same property. Crucial for enum narrowing — broad
				// branch declares the property by $ref to a base type,
				// narrow branch redeclares it with a tighter enum;
				// first-wins kept the broad version and generated
				// values were rejected by the narrow enum.
				if propertyMoreSpecific(converted, existing) {
					out.properties[k] = converted
				}
				continue
			}
			out.properties[k] = converted
		}
	}

	if out.typ == "" {
		out.typ = firstNonNullType(sub.Type)
	}

	if len(sub.Enum) > 0 && len(out.enums) == 0 {
		out.enums = convertEnumNodes(sub.Enum, out.typ)
	}

	if out.minimum == nil {
		out.minimum = sub.Minimum
	}
	if out.maximum == nil {
		out.maximum = sub.Maximum
	}
	if out.multipleOf == nil {
		out.multipleOf = sub.MultipleOf
	}
	if out.minLength == nil {
		out.minLength = sub.MinLength
	}
	if out.maxLength == nil {
		out.maxLength = sub.MaxLength
	}
	if out.pattern == "" {
		out.pattern = sub.Pattern
	}
	if out.format == "" {
		out.format = sub.Format
	}
	if out.minItems == nil {
		out.minItems = sub.MinItems
	}
	if out.maxItems == nil {
		out.maxItems = sub.MaxItems
	}

	// Walk the picked branch's own allOf. Real-world specs
	// declare `type: object` and a op-level `required` list at the wrapper schema,
	// but put the actual property definitions inside allOf branches that reference
	// other component schemas. Without descending, the picked branch
	// contributes a type and a required list but zero property
	// definitions; the generator then can't emit the required keys and
	// response validation fails with "missing properties ...". Skip
	// when the branch already declared its own properties or enum so
	// well-formed union branches don't pay the double-walk cost.
	hasOwnProps := sub.Properties != nil && sub.Properties.Len() > 0
	if len(sub.AllOf) > 0 && !hasOwnProps && len(sub.Enum) == 0 {
		for _, nested := range sub.AllOf {
			mergeAllOfBranch(nested, out, ctx)
		}
	}

	if sub.Items != nil && sub.Items.IsA() && out.items == nil {
		if items := convertProxy(sub.Items.A, ctx); items != nil {
			out.items = items
			if out.typ == "" {
				out.typ = types.TypeArray
			}
		}
	}
}

// mergeComposed folds composedShape fields back into the in-progress
// schema. The outer schema's existing values win on conflict; the
// compose pass only fills gaps the outer schema left empty.
func mergeComposed(typ *string, items **schema.Schema, properties map[string]*schema.Schema, required *[]string, enums *[]any, c *composedShape) {
	if c == nil {
		return
	}
	if *typ == "" && c.typ != "" {
		*typ = c.typ
	}
	if *items == nil && c.items != nil {
		*items = c.items
	}
	for k, v := range c.properties {
		if _, exists := properties[k]; exists {
			continue
		}
		properties[k] = v
	}
	for _, name := range c.required {
		*required = appendUnique(*required, name)
	}
	if len(*enums) == 0 && len(c.enums) > 0 {
		*enums = c.enums
	}
}

// applyComposedConstraints transfers scalar constraints from a
// composedShape onto a fully built schema, filling only fields the
// outer schema left empty. Kept separate from mergeComposed so the
// signature there stays a small set of pointer arguments.
func applyComposedConstraints(out *schema.Schema, c *composedShape) {
	if c == nil {
		return
	}
	if out.Minimum == nil {
		out.Minimum = c.minimum
	}
	if out.Maximum == nil {
		out.Maximum = c.maximum
	}
	if out.MultipleOf == nil {
		out.MultipleOf = c.multipleOf
	}
	if out.MinLength == nil {
		out.MinLength = c.minLength
	}
	if out.MaxLength == nil {
		out.MaxLength = c.maxLength
	}
	if out.Pattern == "" {
		out.Pattern = c.pattern
	}
	if out.Format == "" {
		out.Format = c.format
	}
	if out.MinItems == nil {
		out.MinItems = c.minItems
	}
	if out.MaxItems == nil {
		out.MaxItems = c.maxItems
	}
}

// propertyMoreSpecific reports whether candidate carries strictly more
// shape information than incumbent. "More" means a non-empty type when
// incumbent had none, a non-empty enum when incumbent had none, more
// declared properties, or more required keys. It's intentionally
// asymmetric (no equality case) so the merge stays deterministic when
// the two are equivalent.
func propertyMoreSpecific(candidate, incumbent *schema.Schema) bool {
	if candidate == nil {
		return false
	}
	if incumbent == nil {
		return true
	}
	if len(candidate.Properties) > len(incumbent.Properties) {
		return true
	}
	if len(candidate.Required) > len(incumbent.Required) {
		return true
	}
	if incumbent.Type == "" && candidate.Type != "" {
		return true
	}
	if len(incumbent.Enum) == 0 && len(candidate.Enum) > 0 {
		return true
	}
	return false
}

// firstNonNullBranch returns the first non-null oneOf/anyOf branch from
// branches. A branch declaring only `type: null` is treated as null.
func firstNonNullBranch(branches []*base.SchemaProxy) *base.SchemaProxy {
	for _, p := range branches {
		if p == nil {
			continue
		}
		sub := p.Schema()
		if sub == nil {
			continue
		}
		if isAllNullType(sub.Type) {
			continue
		}
		return p
	}
	return nil
}

func firstNonNullType(t []string) string {
	for _, v := range t {
		if strings.EqualFold(v, "null") {
			continue
		}
		return v
	}
	return ""
}

// firstPatternFromBranches returns the first non-empty regex pattern
// declared on any branch. The generator uses it as a last-resort
// fallback when the outer schema has no pattern of its own.
func firstPatternFromBranches(branches []*base.SchemaProxy) string {
	for _, p := range branches {
		if p == nil {
			continue
		}
		sub := p.Schema()
		if sub == nil {
			continue
		}
		if sub.Pattern != "" {
			return sub.Pattern
		}
	}
	return ""
}

// applyAllOfEnumIntersection narrows a property's enum to the
// intersection across all allOf branches that declare it. The validator
// enforces every branch independently, so a value satisfying only one
// branch's enum would fail the others.
func applyAllOfEnumIntersection(properties map[string]*schema.Schema, allOf []*base.SchemaProxy) {
	if len(allOf) < 2 || len(properties) == 0 {
		return
	}

	for propName, propSchema := range properties {
		var intersection map[string]bool

		for _, branch := range allOf {
			if branch == nil {
				continue
			}

			sub := branch.Schema()
			if sub == nil || sub.Properties == nil {
				continue
			}

			proxy, ok := sub.Properties.Get(propName)
			if !ok || proxy == nil {
				continue
			}

			pb := proxy.Schema()
			if pb == nil || len(pb.Enum) == 0 {
				continue
			}
			values := make(map[string]bool, len(pb.Enum))

			for _, e := range pb.Enum {
				if e != nil {
					values[e.Value] = true
				}
			}

			if intersection == nil {
				intersection = values
				continue
			}

			for k := range intersection {
				if !values[k] {
					delete(intersection, k)
				}
			}
		}

		if len(intersection) == 0 {
			continue
		}
		if len(propSchema.Enum) > 0 {
			var filtered []any
			seen := map[string]bool{}
			for _, v := range propSchema.Enum {
				k := scalarKey(v)
				if !intersection[k] || seen[k] {
					continue
				}
				seen[k] = true
				filtered = append(filtered, v)
			}
			if len(filtered) > 0 {
				propSchema.Enum = filtered
			}
			continue
		}

		for k := range intersection {
			propSchema.Enum = append(propSchema.Enum, k)
		}
	}
}

// applyDiscriminator forces the discriminator property's value to the
// mapping entry that points at the picked oneOf branch. Without this
// the generator might emit a value other branches declare, failing the
// validator's "oneOf must match exactly one branch" check.
func applyDiscriminator(properties map[string]*schema.Schema, s *base.Schema, composed *composedShape) {
	if composed == nil || s == nil || s.Discriminator == nil {
		return
	}
	propName := s.Discriminator.PropertyName
	if propName == "" {
		return
	}

	branch := firstNonNullBranch(s.OneOf)
	if branch == nil {
		branch = firstNonNullBranch(s.AnyOf)
	}
	if branch == nil {
		return
	}

	value := discriminatorValueFor(s.Discriminator, branch)
	if value == "" {
		return
	}

	prop := properties[propName]
	if prop == nil {
		properties[propName] = &schema.Schema{
			Type: types.TypeString,
			Enum: []any{value},
		}
		return
	}

	if enumContainsKey(prop.Enum, value) || len(prop.Enum) == 0 {
		prop.Enum = []any{value}
	}
}

// discriminatorValueFor returns the mapping key whose value matches the
// branch (by $ref) or the branch's title. Returns "" when no mapping
// applies.
func discriminatorValueFor(d *base.Discriminator, branch *base.SchemaProxy) string {
	if d == nil || d.Mapping == nil || d.Mapping.Len() == 0 {
		return ""
	}
	branchRef := ""
	if branch != nil {
		if branch.IsReference() {
			branchRef = branch.GetReference()
		}
	}

	for key, mapTarget := range d.Mapping.FromOldest() {
		if branchRef != "" && mapTarget == branchRef {
			return key
		}
		if shortName(mapTarget) != "" && branchRef != "" && shortName(mapTarget) == shortName(branchRef) {
			return key
		}
	}
	return ""
}

func shortName(ref string) string {
	if ref == "" {
		return ""
	}
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

func appendUnique(list []string, v string) []string {
	for _, existing := range list {
		if existing == v {
			return list
		}
	}
	return append(list, v)
}

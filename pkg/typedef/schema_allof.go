package typedef

import (
	"strconv"
	"strings"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/pb33f/libopenapi/datamodel/high/base"
)

// mergeAllOfUnionProperties walks allOf branches that wrap a oneOf, picks the
// first oneOf branch from each, and returns the merged properties and required
// from its full allOf chain. Recovers fields oapi-codegen-dd drops at union boundaries.
func mergeAllOfUnionProperties(s *base.Schema, tdLookUp map[string]*codegen.TypeDefinition, ctx *schemaContext) (map[string]*schema.Schema, []string) {
	if s == nil || len(s.AllOf) == 0 {
		return nil, nil
	}
	out := map[string]*schema.Schema{}
	var req []string

	for _, p := range s.AllOf {
		if p == nil {
			continue
		}
		sub := p.Schema()
		if sub == nil {
			continue
		}
		if len(sub.OneOf) > 0 && sub.OneOf[0] != nil {
			branch := sub.OneOf[0].Schema()
			if branch != nil {
				props, branchReq := collectAllOfBranchProperties(branch, tdLookUp, ctx)
				for k, v := range props {
					if _, exists := out[k]; !exists {
						out[k] = v
					}
				}
				req = append(req, branchReq...)
			}
		}
	}
	return out, req
}

// collectAllOfBranchProperties recursively collects Properties and Required from
// every sub-schema in an allOf chain.
func collectAllOfBranchProperties(s *base.Schema, tdLookUp map[string]*codegen.TypeDefinition, ctx *schemaContext) (map[string]*schema.Schema, []string) {
	if s == nil {
		return nil, nil
	}
	out := map[string]*schema.Schema{}
	req := append([]string(nil), s.Required...)

	if s.Properties != nil {
		for k, proxy := range s.Properties.FromOldest() {
			if proxy == nil {
				continue
			}
			propSubSchema := proxy.Schema()
			if propSubSchema == nil {
				continue
			}
			if _, exists := out[k]; !exists {
				out[k] = newSchemaFromGoSchemaWithContext(&codegen.GoSchema{OpenAPISchema: propSubSchema}, tdLookUp, ctx)
			}
		}
	}

	for _, p := range s.AllOf {
		if p == nil {
			continue
		}
		nested := p.Schema()
		if nested == nil {
			continue
		}
		nestedProps, nestedReq := collectAllOfBranchProperties(nested, tdLookUp, ctx)
		for k, v := range nestedProps {
			if _, exists := out[k]; !exists {
				out[k] = v
			}
		}
		req = append(req, nestedReq...)
	}
	return out, req
}

// allOfRequired collects every `required` name across allOf branches.
// oapi-codegen-dd flattens branch properties into the parent but loses Required lists.
func allOfRequired(s *base.Schema) []string {
	if s == nil || len(s.AllOf) == 0 {
		return nil
	}
	var out []string
	for _, p := range s.AllOf {
		if p == nil {
			continue
		}
		sub := p.Schema()
		if sub == nil {
			continue
		}
		out = append(out, sub.Required...)
		out = append(out, allOfRequired(sub)...)
	}
	return out
}

// applyAllOfEnumIntersection narrows a property's enum to the intersection across
// allOf branches that declare it. The validator checks each branch independently,
// so a value satisfying only one branch's enum would fail the others.
// oapi-codegen-dd applies last-wins, losing the cross-branch constraint.
func applyAllOfEnumIntersection(propSchema *schema.Schema, propName string, branches []*base.SchemaProxy) {
	if propSchema == nil || len(branches) < 2 {
		return
	}
	var intersection map[string]bool

	for _, branch := range branches {
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
		branchValues := make(map[string]bool, len(pb.Enum))
		for _, e := range pb.Enum {
			if e != nil {
				branchValues[e.Value] = true
			}
		}
		if intersection == nil {
			intersection = branchValues
			continue
		}
		for k := range intersection {
			if !branchValues[k] {
				delete(intersection, k)
			}
		}
	}

	if len(intersection) == 0 {
		// No shared value; preserving oapi-codegen-dd's last-wins enum keeps the
		// most specific override (e.g. a base enum narrowed by an inline branch).
		return
	}
	if len(propSchema.Enum) > 0 {
		seen := make(map[string]bool, len(intersection))
		var filtered []any
		for _, v := range propSchema.Enum {
			key := enumValueKey(v)
			if !intersection[key] || seen[key] {
				continue
			}
			seen[key] = true
			filtered = append(filtered, v)
		}
		if len(filtered) > 0 {
			propSchema.Enum = filtered
		}
		return
	}
	for k := range intersection {
		propSchema.Enum = append(propSchema.Enum, k)
	}
}

// enumValueKey returns a string key comparable against raw YAML node text.
func enumValueKey(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	f, _ := types.ToFloat64(v)
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func mergeRequired(base, additional []string) []string {
	if len(additional) == 0 {
		return base
	}
	if len(base) == 0 {
		return additional
	}

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

func findItemsInAllOf(s *base.Schema) *base.Schema {
	if s == nil || len(s.AllOf) == 0 {
		return nil
	}
	for _, p := range s.AllOf {
		if p == nil {
			continue
		}
		sub := p.Schema()
		if sub == nil {
			continue
		}
		if sub.Items != nil && sub.Items.A != nil {
			if inner := sub.Items.A.Schema(); inner != nil {
				return inner
			}
		}
		if nested := findItemsInAllOf(sub); nested != nil {
			return nested
		}
	}
	return nil
}

func allOfDeclaresOnlyObjects(s *base.Schema) bool {
	if s == nil || len(s.AllOf) == 0 {
		return false
	}
	for _, p := range s.AllOf {
		if p == nil {
			return false
		}
		sub := p.Schema()
		if sub == nil {
			return false
		}
		hasObject := false
		for _, t := range sub.Type {
			if strings.EqualFold(t, types.TypeObject) {
				hasObject = true
				break
			}
		}
		if !hasObject {
			return false
		}
	}
	return true
}

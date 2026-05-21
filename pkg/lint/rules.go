package lint

import (
	"fmt"
	"strings"

	"github.com/pb33f/libopenapi/datamodel/high/base"
)

// rules is the registry of defect detectors. Each receives one schema and
// returns the defects found at this exact node (children are walked
// separately by the walker in lint.go). Keep rule functions narrow:
// cross-schema reasoning belongs in the walker.
var rules = []func(*base.Schema, string) []Defect{
	ruleArrayEnumScalars,
	ruleAdditionalPropertiesFalseWithOneOfProperties,
	ruleAllOfNonOverlappingPropertyEnums,
	ruleAllOfAdditionalPropertiesConflictsSibling,
	rulePatternUnicodeCircumflex,
}

// ruleArrayEnumScalars flags `type: array` schemas whose `enum` declares
// scalar values (strings, numbers, bools). The validator interprets enum as
// "value must equal one of these"; an array can never equal a scalar, so
// every array shape (including `[]`) is rejected.
func ruleArrayEnumScalars(s *base.Schema, path string) []Defect {
	if s == nil || len(s.Enum) == 0 {
		return nil
	}
	isArray := false
	for _, t := range s.Type {
		if strings.EqualFold(t, "array") {
			isArray = true
			break
		}
	}
	if !isArray {
		return nil
	}
	scalarEnum := false
	for _, n := range s.Enum {
		if n == nil {
			continue
		}
		switch n.Tag {
		case "!!str", "!!int", "!!float", "!!bool":
			scalarEnum = true
		}
	}
	if !scalarEnum {
		return nil
	}
	return []Defect{{
		Rule:   "array-enum-scalars",
		Path:   path,
		Detail: "type: array with scalar enum: arrays can never equal a scalar, schema is unsatisfiable",
	}}
}

// ruleAdditionalPropertiesFalseWithOneOfProperties flags schemas where
// `additionalProperties: false` is paired with `oneOf` branches that declare
// properties. JSON Schema's additionalProperties only considers sibling
// `properties`; oneOf adds via composition but strict validators don't merge
// those into the sibling set, so every branch's fields become "additional"
// and are rejected.
func ruleAdditionalPropertiesFalseWithOneOfProperties(s *base.Schema, path string) []Defect {
	if s == nil || s.AdditionalProperties == nil || !s.AdditionalProperties.IsB() || s.AdditionalProperties.B {
		return nil
	}
	if len(s.OneOf) == 0 {
		return nil
	}
	for _, p := range s.OneOf {
		if p == nil {
			continue
		}
		sub := p.Schema()
		if sub != nil && sub.Properties != nil && sub.Properties.Len() > 0 {
			return []Defect{{
				Rule:   "additional-props-false-with-oneof",
				Path:   path,
				Detail: "additionalProperties: false paired with oneOf branches that declare properties; strict validators reject every branch's fields as `additional`",
			}}
		}
	}
	return nil
}

// ruleAllOfNonOverlappingPropertyEnums flags allOf branches that constrain
// the same property to disjoint enum sets. The validator must satisfy every
// branch; if the intersection is empty, no concrete value passes.
func ruleAllOfNonOverlappingPropertyEnums(s *base.Schema, path string) []Defect {
	if s == nil || len(s.AllOf) < 2 {
		return nil
	}
	type branchEnum struct {
		values map[string]bool
	}
	perProp := map[string][]branchEnum{}
	for _, branch := range s.AllOf {
		if branch == nil {
			continue
		}
		sub := branch.Schema()
		if sub == nil || sub.Properties == nil {
			continue
		}
		for k, propProxy := range sub.Properties.FromOldest() {
			ps := propProxy.Schema()
			if ps == nil || len(ps.Enum) == 0 {
				continue
			}
			be := branchEnum{values: map[string]bool{}}
			for _, n := range ps.Enum {
				if n != nil {
					be.values[n.Value] = true
				}
			}
			perProp[k] = append(perProp[k], be)
		}
	}
	for prop, branches := range perProp {
		if len(branches) < 2 {
			continue
		}
		intersection := map[string]bool{}
		for k := range branches[0].values {
			intersection[k] = true
		}
		for _, b := range branches[1:] {
			for k := range intersection {
				if !b.values[k] {
					delete(intersection, k)
				}
			}
		}
		if len(intersection) == 0 {
			return []Defect{{
				Rule:   "allof-non-overlapping-enums",
				Path:   path + ".properties." + prop,
				Detail: "allOf branches declare disjoint enum sets for the same property; no value satisfies all branches",
			}}
		}
	}
	return nil
}

// ruleAllOfAdditionalPropertiesConflictsSibling flags allOf composition where
// one branch declares typed `additionalProperties` (e.g. `{type: object}`)
// while another branch (or the schema's own top-level) declares a property
// of a different type. The first branch sees that property as "additional"
// and demands its declared type; the other declares an incompatible one.
// Common in Azure/Atlassian specs where a base schema with
// additionalProperties is `$ref`'d under allOf.
func ruleAllOfAdditionalPropertiesConflictsSibling(s *base.Schema, path string) []Defect {
	if s == nil || len(s.AllOf) == 0 {
		return nil
	}
	type branchInfo struct {
		props  map[string]string // property name -> type (empty when not declared)
		apType string            // additionalProperties.A.Type[0] when schema-form
		hasAP  bool
	}
	collect := func(sub *base.Schema) branchInfo {
		info := branchInfo{props: map[string]string{}}
		if sub == nil {
			return info
		}
		if sub.Properties != nil {
			for k, propProxy := range sub.Properties.FromOldest() {
				ps := propProxy.Schema()
				if ps == nil || len(ps.Type) == 0 {
					info.props[k] = ""
					continue
				}
				info.props[k] = ps.Type[0]
			}
		}
		if sub.AdditionalProperties != nil && sub.AdditionalProperties.IsA() && sub.AdditionalProperties.A != nil {
			if apSub := sub.AdditionalProperties.A.Schema(); apSub != nil && len(apSub.Type) > 0 {
				info.apType = apSub.Type[0]
				info.hasAP = true
			}
		}
		return info
	}
	branches := make([]branchInfo, 0, len(s.AllOf)+1)
	for _, p := range s.AllOf {
		if p == nil {
			continue
		}
		sub := p.Schema()
		if sub == nil {
			continue
		}
		branches = append(branches, collect(sub))
	}
	// The schema's own top-level acts as a sibling allOf branch; JSON
	// Schema merges them when validating.
	branches = append(branches, collect(s))
	if len(branches) < 2 {
		return nil
	}
	for i, b := range branches {
		if !b.hasAP {
			continue
		}
		for j, other := range branches {
			if i == j {
				continue
			}
			for propName, propType := range other.props {
				if _, owned := b.props[propName]; owned {
					continue
				}
				if propType == "" || propType == b.apType {
					continue
				}
				return []Defect{{
					Rule: "allof-additional-props-conflicts-sibling",
					Path: path,
					Detail: fmt.Sprintf("allOf branch defines `%s: %s` while sibling branch's additionalProperties demands %s; the property satisfies neither branch",
						propName, propType, b.apType),
				}}
			}
		}
	}
	return nil
}

// rulePatternUnicodeCircumflex flags patterns containing U+02C6 (ˆ MODIFIER
// LETTER CIRCUMFLEX). It looks like `^` but is a literal char, so patterns
// like `ˆ^\d{4}$` require a literal ˆ followed by a start-of-string anchor;
// the anchor can never match after consuming a character.
func rulePatternUnicodeCircumflex(s *base.Schema, path string) []Defect {
	if s == nil || s.Pattern == "" {
		return nil
	}
	if strings.ContainsRune(s.Pattern, 'ˆ') {
		return []Defect{{
			Rule:   "pattern-unicode-circumflex",
			Path:   path,
			Detail: "pattern contains U+02C6 (ˆ MODIFIER LETTER CIRCUMFLEX), likely a typo for ^; the resulting regex is unsatisfiable",
		}}
	}
	return nil
}

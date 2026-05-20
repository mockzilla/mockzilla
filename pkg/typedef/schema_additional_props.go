package typedef

import (
	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/pb33f/libopenapi/datamodel/high/base"
)

func hasExplicitAdditionalPropertiesFalse(inner *base.Schema) bool {
	if inner == nil || inner.AdditionalProperties == nil {
		return false
	}
	return inner.AdditionalProperties.IsB() && !inner.AdditionalProperties.B
}

// additionalPropertiesIsPlaceholder reports the oapi-codegen-dd `any` fabrication:
// an empty string-typed schema with no constraints, properties, items, or composition.
func additionalPropertiesIsPlaceholder(s *schema.Schema) bool {
	if s == nil {
		return true
	}
	if s.Type != types.TypeString {
		return false
	}
	return s.Pattern == "" && s.Format == "" && s.MinLength == nil && s.MaxLength == nil &&
		len(s.Enum) == 0 && len(s.Properties) == 0 && s.Items == nil &&
		s.AdditionalProperties == nil
}

// findPropertyAdditionalPropertiesWithRef returns the resolved schema (or the
// $ref string when the proxy can't be dereferenced, e.g. dotted component names
// libopenapi's resolver can't follow). Callers can then look up the named type.
func findPropertyAdditionalPropertiesWithRef(s *base.Schema, name string) (*base.Schema, string) {
	if s == nil {
		return nil, ""
	}
	if s.Properties != nil {
		if proxy, ok := s.Properties.Get(name); ok && proxy != nil {
			if sub := proxy.Schema(); sub != nil && sub.AdditionalProperties != nil &&
				sub.AdditionalProperties.IsA() && sub.AdditionalProperties.A != nil {
				ap := sub.AdditionalProperties.A
				if apSchema := ap.Schema(); apSchema != nil {
					return apSchema, ""
				}
				if ap.IsReference() {
					return nil, ap.GetReference()
				}
			}
		}
	}
	for _, p := range s.AllOf {
		if p == nil {
			continue
		}
		if sub := p.Schema(); sub != nil {
			if apSchema, ref := findPropertyAdditionalPropertiesWithRef(sub, name); apSchema != nil || ref != "" {
				return apSchema, ref
			}
		}
	}
	return nil, ""
}

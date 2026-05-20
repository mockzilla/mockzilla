package typedef

import (
	"strings"

	"github.com/doordash-oss/oapi-codegen-dd/v3/pkg/codegen"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/pb33f/libopenapi/datamodel/high/base"
)

func firstPatternFromBranches(branches []*base.SchemaProxy) string {
	for _, proxy := range branches {
		if proxy == nil {
			continue
		}
		s := proxy.Schema()
		if s == nil {
			continue
		}
		if s.Pattern != "" {
			return s.Pattern
		}
	}
	return ""
}

// enumsFromUnionBranches returns enum values from the first branch with an enum,
// preferring branches whose type matches `typ`. Falls back to any non-null branch
// when none match; the returned type may differ from `typ`, signaling adoption.
func enumsFromUnionBranches(branches []*base.SchemaProxy, typ string) ([]any, string) {
	var fallbackEnum []any
	var fallbackType string
	for _, proxy := range branches {
		if proxy == nil {
			continue
		}
		s := proxy.Schema()
		if s == nil || len(s.Enum) == 0 {
			continue
		}
		branchType := ""
		allNull := len(s.Type) > 0
		matchesType := false

		for _, t := range s.Type {
			if strings.ToLower(t) == "null" {
				continue
			}
			allNull = false
			if branchType == "" {
				branchType = t
			}
			if t == typ {
				matchesType = true
			}
		}
		if allNull {
			continue
		}
		if branchType == "" {
			branchType = typ
		}
		var values []any
		for _, e := range s.Enum {
			values = append(values, convertEnumValue(e.Value, branchType))
		}
		if len(values) == 0 {
			continue
		}
		if matchesType || len(s.Type) == 0 {
			return values, typ
		}
		if fallbackEnum == nil {
			fallbackEnum = values
			fallbackType = branchType
		}
	}
	return fallbackEnum, fallbackType
}

func discriminatorFromInner(d *codegen.Discriminator, inner *base.Schema) *schema.Discriminator {
	if d != nil && d.Property != "" {
		out := &schema.Discriminator{PropertyName: d.Property}
		if len(d.Mapping) > 0 {
			out.Mapping = make(map[string]string, len(d.Mapping))
			for k, v := range d.Mapping {
				out.Mapping[k] = v
			}
		}
		return out
	}
	if inner == nil || inner.Discriminator == nil || inner.Discriminator.PropertyName == "" {
		return nil
	}
	return &schema.Discriminator{PropertyName: inner.Discriminator.PropertyName}
}

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

// findUnionSchema follows the reference chain to the union schema, descending
// through wrapper types that hold a single embedded reference.
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

	if len(refTd.Schema.UnionElements) > 0 || refTd.Schema.IsUnionWrapper {
		return &refTd.Schema
	}

	if len(refTd.Schema.Properties) == 1 && len(refTd.Schema.UnionElements) == 0 {
		prop := refTd.Schema.Properties[0]
		if prop.JsonFieldName == "" && prop.Schema.RefType != "" {
			return findUnionSchemaRecursive(prop.Schema.RefType, tdLookUp, visited)
		}
	}

	return nil
}

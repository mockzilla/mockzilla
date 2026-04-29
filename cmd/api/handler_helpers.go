package api

import (
	"fmt"
	"strings"

	"github.com/mockzilla/mockzilla/v2/pkg/schema"
)

const schemaInit = "schema.Schema"

// renderSchema renders a schema.Schema as Go code
func renderSchema(s *schema.Schema) string {
	if s == nil {
		return "nil"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "&%s{\n", schemaInit)

	if s.Type != "" {
		fmt.Fprintf(&sb, "\t\tType: %q,\n", s.Type)
	}

	if s.Format != "" {
		fmt.Fprintf(&sb, "\t\tFormat: %q,\n", s.Format)
	}

	if s.Pattern != "" {
		fmt.Fprintf(&sb, "\t\tPattern: %q,\n", s.Pattern)
	}

	if s.Minimum != nil {
		fmt.Fprintf(&sb, "\t\tMinimum: ptr(%v),\n", *s.Minimum)
	}

	if s.Maximum != nil {
		fmt.Fprintf(&sb, "\t\tMaximum: ptr(%v),\n", *s.Maximum)
	}

	if s.MinLength != nil {
		fmt.Fprintf(&sb, "\t\tMinLength: ptr(int64(%v)),\n", *s.MinLength)
	}

	if s.MaxLength != nil {
		fmt.Fprintf(&sb, "\t\tMaxLength: ptr(int64(%v)),\n", *s.MaxLength)
	}

	if s.MinItems != nil {
		fmt.Fprintf(&sb, "\t\tMinItems: ptr(int64(%v)),\n", *s.MinItems)
	}

	if s.MaxItems != nil {
		fmt.Fprintf(&sb, "\t\tMaxItems: ptr(int64(%v)),\n", *s.MaxItems)
	}

	if len(s.Enum) > 0 {
		sb.WriteString("\t\tEnum: []any{")
		for i, e := range s.Enum {
			if i > 0 {
				sb.WriteString(", ")
			}
			// Handle different types of enum values
			switch v := e.(type) {
			case string:
				fmt.Fprintf(&sb, "%q", v)
			case bool, int, int64, float64:
				fmt.Fprintf(&sb, "%v", v)
			default:
				fmt.Fprintf(&sb, "%q", fmt.Sprint(v))
			}
		}
		sb.WriteString("},\n")
	}

	if s.Items != nil {
		fmt.Fprintf(&sb, "\t\tItems: %s,\n", renderSchema(s.Items))
	}

	if len(s.Properties) > 0 {
		fmt.Fprintf(&sb, "\t\tProperties: map[string]*%s{\n", schemaInit)
		for name, prop := range s.Properties {
			fmt.Fprintf(&sb, "\t\t\t%q: %s,\n", name, renderSchema(prop))
		}
		sb.WriteString("\t\t},\n")
	}

	if len(s.Required) > 0 {
		sb.WriteString("\t\tRequired: []string{")
		for i, r := range s.Required {
			if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(&sb, "%q", r)
		}
		sb.WriteString("},\n")
	}

	if s.AdditionalProperties != nil {
		fmt.Fprintf(&sb, "\t\tAdditionalProperties: %s,\n", renderSchema(s.AdditionalProperties))
	}

	if s.Nullable {
		sb.WriteString("\t\tNullable: true,\n")
	}

	if s.ReadOnly {
		sb.WriteString("\t\tReadOnly: true,\n")
	}

	if s.WriteOnly {
		sb.WriteString("\t\tWriteOnly: true,\n")
	}

	if s.Deprecated {
		sb.WriteString("\t\tDeprecated: true,\n")
	}

	sb.WriteString("\t\t}")
	return sb.String()
}

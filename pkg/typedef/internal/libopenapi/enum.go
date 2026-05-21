package libopenapi

import (
	"strconv"

	"github.com/mockzilla/mockzilla/v2/internal/types"
	"go.yaml.in/yaml/v4"
)

// convertEnumNodes converts a libopenapi enum (slice of *yaml.Node) to
// the typed []any the generator expects. The node's natural type tag
// wins over the declared schema type: a `type: string` schema with
// `enum: [1, 2]` produces int64 values, mirroring validator behaviour.
func convertEnumNodes(nodes []*yaml.Node, schemaType string) []any {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]any, 0, len(nodes))
	for _, n := range nodes {
		v := convertScalarNode(n, schemaType)
		out = append(out, v)
	}
	return out
}

// convertScalarNode converts one enum/const yaml.Node. Returns nil for
// `!!null` so the generator can route the value as JSON null.
func convertScalarNode(n *yaml.Node, schemaType string) any {
	if n == nil {
		return nil
	}
	if n.Tag == "!!null" {
		return nil
	}
	if n.Tag != "" && n.Tag != "!!str" && n.Tag != "!!int" && n.Tag != "!!float" && n.Tag != "!!bool" && n.Value == "" {
		return nil
	}
	if schemaType == types.TypeString {
		switch n.Tag {
		case "!!int":
			if i, err := strconv.ParseInt(n.Value, 10, 64); err == nil {
				return i
			}
		case "!!float":
			if f, err := strconv.ParseFloat(n.Value, 64); err == nil {
				return f
			}
		case "!!bool":
			if b, err := strconv.ParseBool(n.Value); err == nil {
				return b
			}
		}
	}
	return parseTypedValue(n.Value, schemaType)
}

// parseTypedValue parses a scalar string into the schema's declared type.
// For TypeString it returns the raw value so YAML-quoted numerics like
// '0', '1' stay strings.
func parseTypedValue(value, schemaType string) any {
	switch schemaType {
	case types.TypeInteger:
		if i, err := strconv.ParseInt(value, 10, 64); err == nil {
			return i
		}
		if num := extractLeadingNumber(value); num != "" {
			if i, err := strconv.ParseInt(num, 10, 64); err == nil {
				return i
			}
		}
		return value
	case types.TypeNumber:
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
		if num := extractLeadingNumber(value); num != "" {
			if f, err := strconv.ParseFloat(num, 64); err == nil {
				return f
			}
		}
		return value
	case types.TypeBoolean:
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
		return value
	case types.TypeString:
		return value
	default:
		return value
	}
}

// inferTypeFromNode returns the OpenAPI type implied by a yaml.Node's
// natural tag. Used to recover a type for const-driven enums when the
// surrounding schema omits one.
func inferTypeFromNode(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	switch n.Tag {
	case "!!str":
		return types.TypeString
	case "!!int":
		return types.TypeInteger
	case "!!float":
		return types.TypeNumber
	case "!!bool":
		return types.TypeBoolean
	}
	return ""
}

func extractLeadingNumber(s string) string {
	var out string
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '-' || r == '.' {
			out += string(r)
			continue
		}
		break
	}
	return out
}

// enumContainsKey reports whether the converted enum already contains v
// as a string-comparable value.
func enumContainsKey(enum []any, v string) bool {
	for _, e := range enum {
		if scalarKey(e) == v {
			return true
		}
	}
	return false
}

// scalarKey renders a converted enum value back to a string for
// comparison with yaml.Node.Value text.
func scalarKey(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

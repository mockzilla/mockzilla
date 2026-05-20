package typedef

import (
	"fmt"
	"strconv"

	"github.com/mockzilla/mockzilla/v2/internal/types"
	"go.yaml.in/yaml/v4"
)

func inferTypeFromYAMLNodes(nodes []*yaml.Node) string {
	for _, n := range nodes {
		if n == nil {
			continue
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
	}
	return ""
}

// convertEnumNode converts a YAML enum entry, preferring the node's natural
// type tag over the declared schema type. Validators enforce the natural type,
// so `type: string` paired with `enum: [1, 2]` produces int64 values, not strings.
func convertEnumNode(e *yaml.Node, schemaType string) any {
	if e == nil {
		return nil
	}
	// `!!null` means JSON null (distinct from ""); preserve nil so the runtime routes it.
	if e.Tag == "!!null" {
		return nil
	}
	// Malformed entries (e.g. a map where a scalar belongs) have empty .Value; drop them.
	if e.Tag != "" && e.Tag != "!!str" && e.Tag != "!!int" && e.Tag != "!!float" && e.Tag != "!!bool" && e.Value == "" {
		return nil
	}
	if schemaType == types.TypeString {
		switch e.Tag {
		case "!!int":
			if i, err := strconv.ParseInt(e.Value, 10, 64); err == nil {
				return i
			}
		case "!!float":
			if f, err := strconv.ParseFloat(e.Value, 64); err == nil {
				return f
			}
		case "!!bool":
			if b, err := strconv.ParseBool(e.Value); err == nil {
				return b
			}
		}
	}
	return convertEnumValue(e.Value, schemaType)
}

// convertEnumValue parses an enum value string to the schema's type.
// String types are returned verbatim so YAML-parsed numerics stay strings.
func convertEnumValue(value string, schemaType string) any {
	switch schemaType {
	case types.TypeInteger:
		if i, err := strconv.ParseInt(value, 10, 64); err == nil {
			return i
		}
		// Tolerate annotated enums like "0 (User)" by extracting the leading number.
		if numStr := extractLeadingNumber(value); numStr != "" {
			if i, err := strconv.ParseInt(numStr, 10, 64); err == nil {
				return i
			}
		}
		return value
	case types.TypeNumber:
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
		if numStr := extractLeadingNumber(value); numStr != "" {
			if f, err := strconv.ParseFloat(numStr, 64); err == nil {
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
		// Keep as string so YAML-parsed numerics like '0', '1' don't become ints.
		return value
	default:
		return value
	}
}

// extractLeadingNumber returns the leading numeric prefix (e.g. "101 (label)" -> "101").
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

func enumContains(enum []any, v string) bool {
	for _, e := range enum {
		if fmt.Sprintf("%v", e) == v {
			return true
		}
	}
	return false
}

package schema

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strings"
)

// BuildSchemaFromContent creates a Schema from static content.
// It infers the schema structure from the content based on the content type.
// The original content is stored in StaticContent for direct return.
func BuildSchemaFromContent(content []byte, contentType string) (*Schema, error) {
	trimmedContent := strings.TrimSpace(string(content))

	// Determine how to parse based on content type
	switch {
	case strings.Contains(contentType, "application/json"), strings.Contains(contentType, "+json"):
		return buildSchemaFromJSON([]byte(trimmedContent))
	case strings.Contains(contentType, "application/xml"), strings.Contains(contentType, "text/xml"):
		return buildSchemaFromXML([]byte(trimmedContent))
	case strings.Contains(contentType, "text/html"), strings.Contains(contentType, "text/plain"),
		strings.Contains(contentType, "text/css"), strings.Contains(contentType, "application/javascript"),
		strings.Contains(contentType, "application/yaml"), strings.Contains(contentType, "text/csv"):
		// For text-based content, just store as string
		return &Schema{
			Type:          "string",
			StaticContent: trimmedContent,
		}, nil
	default:
		// For binary or unknown content types, store as string
		return &Schema{
			Type:          "string",
			Format:        "binary",
			StaticContent: trimmedContent,
		}, nil
	}
}

// buildSchemaFromJSON parses JSON content and infers schema structure.
func buildSchemaFromJSON(content []byte) (*Schema, error) {
	var data any
	if err := json.Unmarshal(content, &data); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	schema := inferSchemaFromValue(data)
	// Store the original formatted content
	schema.StaticContent = string(content)

	return schema, nil
}

// buildSchemaFromXML parses XML content and creates a basic schema.
func buildSchemaFromXML(content []byte) (*Schema, error) {
	// For XML, we'll do basic validation and store as string
	// Full XML schema inference would be complex
	var data any
	if err := xml.Unmarshal(content, &data); err != nil {
		return nil, fmt.Errorf("failed to parse XML: %w", err)
	}

	return &Schema{
		Type:          "string",
		Format:        "xml",
		StaticContent: string(content),
	}, nil
}

// inferSchemaFromValue recursively infers schema from a parsed value.
func inferSchemaFromValue(val any) *Schema {
	schema := &Schema{}

	switch v := val.(type) {
	case map[string]any:
		schema.Type = "object"
		schema.Properties = make(map[string]*Schema)
		for key, propVal := range v {
			schema.Properties[key] = inferSchemaFromValue(propVal)
		}

	case []any:
		schema.Type = "array"
		if len(v) > 0 {
			// Infer items schema from first element
			schema.Items = inferSchemaFromValue(v[0])
		} else {
			// Empty array - default to string items
			schema.Items = &Schema{Type: "string"}
		}

	case string:
		schema.Type = "string"

	case float64:
		// JSON numbers are always float64
		// Check if it's actually an integer
		if v == float64(int64(v)) {
			schema.Type = "integer"
		} else {
			schema.Type = "number"
		}

	case bool:
		schema.Type = "boolean"

	case nil:
		schema.Type = "null"
		schema.Nullable = true

	default:
		// Fallback to string for unknown types
		schema.Type = "string"
	}

	return schema
}

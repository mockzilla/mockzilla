package generator

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/mockzilla/mockzilla/v2/internal/replacer"
	"github.com/mockzilla/mockzilla/v2/internal/types"
	"github.com/mockzilla/mockzilla/v2/pkg/schema"
)

// generatePath generates Path from the given path and parameters.
func generatePath(op *schema.Operation, valueReplacer replacer.ValueReplacer) string {
	path := op.Path

	// Ensure all path placeholders have corresponding parameter definitions
	pathParams := ensurePathParams(op.Path, op.PathParams)

	if pathParams != nil {
		// Path params don't use WithWriteOnly - they're URL segments, not body content,
		// so readOnly/writeOnly semantics don't apply.
		state := replacer.NewReplaceState(replacer.WithPath())
		data := generateContentFromSchema(pathParams, valueReplacer, state)
		if data != nil {
			for k, v := range data.(map[string]any) {
				path = strings.ReplaceAll(path, "{"+k+"}", fmt.Sprintf("%v", v))
			}
		}
	}

	if len(op.Query) > 0 {
		// Build query schema from QueryParameters
		properties := make(map[string]*schema.Schema)
		var required []string
		for name, param := range op.Query {
			properties[name] = param.Schema
			if param.Required {
				required = append(required, name)
			}
		}
		querySchema := &schema.Schema{
			Type:       "object",
			Properties: properties,
			Required:   required,
		}
		state := replacer.NewReplaceState(replacer.WithWriteOnly())
		queryData := generateContentFromSchema(querySchema, valueReplacer, state)
		if queryData != nil {
			query := types.MapToURLEncodedForm(queryData.(map[string]any))
			if query != "" {
				path += "?" + query
			}
		}
	}

	return path
}

// ensurePathParams checks if all path placeholders have corresponding parameter definitions.
// If a placeholder is missing, it adds a string type schema for it.
// Also normalizes existing path param schemas - if they have "any" or empty type,
// they are changed to "string" since path params must be scalar values.
func ensurePathParams(path string, pathParams *schema.Schema) *schema.Schema {
	placeholders := types.ExtractPlaceholders(path)
	if len(placeholders) == 0 {
		return pathParams
	}

	// Build set of existing properties and check if any need normalization
	existingProps := make(map[string]*schema.Schema)
	needsNormalization := false
	if pathParams != nil && pathParams.Properties != nil {
		for name, s := range pathParams.Properties {
			existingProps[name] = s
			// Check if this param needs type normalization
			if s != nil && (s.Type == "" || s.Type == "any") {
				needsNormalization = true
			}
		}
	}

	// Check for missing placeholders
	var missing []string
	for _, placeholder := range placeholders {
		name := strings.Trim(placeholder, "{}")
		if _, exists := existingProps[name]; !exists {
			missing = append(missing, name)
		}
	}

	if len(missing) == 0 && !needsNormalization {
		return pathParams
	}

	// Log warning about missing path parameters
	if len(missing) > 0 {
		slog.Debug("Path has undefined parameters, adding string type", "path", path, "missing", missing)
	}

	// Create new schema with missing params added and types normalized
	result := &schema.Schema{
		Type:       "object",
		Properties: make(map[string]*schema.Schema),
	}

	// Copy existing properties, normalizing types as needed
	for name, s := range existingProps {
		if s != nil && (s.Type == "" || s.Type == "any") {
			// Create a copy with string type
			normalized := *s
			normalized.Type = "string"
			result.Properties[name] = &normalized
		} else {
			result.Properties[name] = s
		}
	}

	// Add missing params as string type
	for _, name := range missing {
		result.Properties[name] = &schema.Schema{Type: "string"}
	}

	return result
}

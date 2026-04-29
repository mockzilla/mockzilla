package api

import (
	"strings"
	"testing"

	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/stretchr/testify/assert"
)

func TestRenderSchema(t *testing.T) {
	t.Run("renders enum with different types", func(t *testing.T) {
		s := &schema.Schema{
			Type: "string",
			Enum: []any{"value1", "value2", true, 123, 45.67},
		}

		result := renderSchema(s)

		// Should handle string values with quotes
		assert.Contains(t, result, `"value1"`)
		assert.Contains(t, result, `"value2"`)

		// Should handle boolean without quotes
		assert.Contains(t, result, `true`)

		// Should handle integers without quotes
		assert.Contains(t, result, `123`)

		// Should handle floats without quotes
		assert.Contains(t, result, `45.67`)

		// Should not have formatting errors like %!q
		assert.NotContains(t, result, "%!")
	})

	t.Run("renders boolean enum", func(t *testing.T) {
		s := &schema.Schema{
			Type: "boolean",
			Enum: []any{true},
		}

		result := renderSchema(s)

		assert.Contains(t, result, `Enum: []any{true}`)
		assert.NotContains(t, result, `"true"`) // Should not be quoted
		assert.NotContains(t, result, "%!")
	})

	t.Run("renders string enum", func(t *testing.T) {
		s := &schema.Schema{
			Type: "string",
			Enum: []any{"active", "inactive"},
		}

		result := renderSchema(s)

		assert.Contains(t, result, `"active"`)
		assert.Contains(t, result, `"inactive"`)
	})

	t.Run("renders integer enum", func(t *testing.T) {
		s := &schema.Schema{
			Type: "integer",
			Enum: []any{1, 2, 3},
		}

		result := renderSchema(s)

		assert.Contains(t, result, `Enum: []any{1, 2, 3}`)
		assert.NotContains(t, result, `"1"`) // Should not be quoted
	})

	t.Run("renders nil schema", func(t *testing.T) {
		result := renderSchema(nil)
		assert.Equal(t, "nil", result)
	})

	t.Run("renders nested schema", func(t *testing.T) {
		s := &schema.Schema{
			Type: "object",
			Properties: map[string]*schema.Schema{
				"name": {
					Type: "string",
				},
				"age": {
					Type: "integer",
				},
			},
		}

		result := renderSchema(s)

		assert.Contains(t, result, `Type: "object"`)
		assert.Contains(t, result, `Properties:`)
		assert.Contains(t, result, `"name"`)
		assert.Contains(t, result, `"age"`)
	})

	t.Run("renders array schema", func(t *testing.T) {
		s := &schema.Schema{
			Type: "array",
			Items: &schema.Schema{
				Type: "string",
			},
		}

		result := renderSchema(s)

		assert.Contains(t, result, `Type: "array"`)
		assert.Contains(t, result, `Items:`)
		assert.Contains(t, result, `Type: "string"`)
	})

	t.Run("renders schema with constraints", func(t *testing.T) {
		minLen := int64(5)
		maxLen := int64(100)
		s := &schema.Schema{
			Type:      "string",
			MinLength: &minLen,
			MaxLength: &maxLen,
			Pattern:   "^[a-z]+$",
		}

		result := renderSchema(s)

		assert.Contains(t, result, `MinLength: ptr(int64(5))`)
		assert.Contains(t, result, `MaxLength: ptr(int64(100))`)
		assert.Contains(t, result, `Pattern: "^[a-z]+$"`)
	})

	t.Run("renders schema with nullable", func(t *testing.T) {
		s := &schema.Schema{
			Type:     "string",
			Nullable: true,
		}

		result := renderSchema(s)

		assert.Contains(t, result, `Nullable: true`)
	})

	t.Run("renders valid Go code", func(t *testing.T) {
		s := &schema.Schema{
			Type: "object",
			Properties: map[string]*schema.Schema{
				"status": {
					Type: "string",
					Enum: []any{"active", "inactive"},
				},
				"deleted": {
					Type: "boolean",
					Enum: []any{true},
				},
			},
		}

		result := renderSchema(s)

		// The result should be valid Go code (no syntax errors)
		// Check for common syntax error patterns
		assert.NotContains(t, result, "%!")
		assert.NotContains(t, result, "MISSING")

		// Should have proper structure
		assert.True(t, strings.HasPrefix(result, "&schema.Schema{"))
		assert.True(t, strings.HasSuffix(strings.TrimSpace(result), "}"))
	})
}

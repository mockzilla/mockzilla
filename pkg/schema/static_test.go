package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildSchemaFromContent(t *testing.T) {
	t.Run("JSON object", func(t *testing.T) {
		content := []byte(`{
  "id": "123",
  "name": "John Doe",
  "age": 30,
  "active": true
}`)
		schema, err := BuildSchemaFromContent(content, "application/json")
		assert.NoError(t, err)

		assert.Equal(t, "object", schema.Type)
		assert.NotEmpty(t, schema.StaticContent)
		assert.Contains(t, schema.Properties, "id")
		assert.Contains(t, schema.Properties, "name")
		assert.Contains(t, schema.Properties, "age")
		assert.Contains(t, schema.Properties, "active")

		assert.Equal(t, "string", schema.Properties["id"].Type)
		assert.Equal(t, "string", schema.Properties["name"].Type)
		assert.Equal(t, "integer", schema.Properties["age"].Type)
		assert.Equal(t, "boolean", schema.Properties["active"].Type)
	})

	t.Run("JSON array", func(t *testing.T) {
		content := []byte(`[
  {"id": "1", "name": "Alice"},
  {"id": "2", "name": "Bob"}
]`)
		schema, err := BuildSchemaFromContent(content, "application/json")
		assert.NoError(t, err)

		assert.Equal(t, "array", schema.Type)
		assert.NotNil(t, schema.Items)
		assert.Equal(t, "object", schema.Items.Type)
		assert.Contains(t, schema.Items.Properties, "id")
		assert.Contains(t, schema.Items.Properties, "name")
	})

	t.Run("JSON suffix media type", func(t *testing.T) {
		schema, err := BuildSchemaFromContent([]byte(`{"title": "missing"}`), "application/problem+json")
		assert.NoError(t, err)

		assert.Equal(t, "object", schema.Type)
		assert.Contains(t, schema.Properties, "title")
	})

	t.Run("JSON with nested object", func(t *testing.T) {
		content := []byte(`{
  "user": {
    "name": "John",
    "address": {
      "city": "NYC"
    }
  }
}`)
		schema, err := BuildSchemaFromContent(content, "application/json")
		assert.NoError(t, err)

		assert.Equal(t, "object", schema.Type)
		assert.Contains(t, schema.Properties, "user")

		userSchema := schema.Properties["user"]
		assert.Equal(t, "object", userSchema.Type)
		assert.Contains(t, userSchema.Properties, "name")
		assert.Contains(t, userSchema.Properties, "address")

		addressSchema := userSchema.Properties["address"]
		assert.Equal(t, "object", addressSchema.Type)
		assert.Contains(t, addressSchema.Properties, "city")
	})

	t.Run("JSON with number types", func(t *testing.T) {
		content := []byte(`{
  "count": 42,
  "price": 19.99,
  "zero": 0
}`)
		schema, err := BuildSchemaFromContent(content, "application/json")
		assert.NoError(t, err)

		assert.Equal(t, "integer", schema.Properties["count"].Type)
		assert.Equal(t, "number", schema.Properties["price"].Type)
		assert.Equal(t, "integer", schema.Properties["zero"].Type)
	})

	t.Run("XML content", func(t *testing.T) {
		content := []byte(`<user><name>John</name></user>`)
		schema, err := BuildSchemaFromContent(content, "application/xml")
		assert.NoError(t, err)

		assert.Equal(t, "string", schema.Type)
		assert.Equal(t, "xml", schema.Format)
		assert.NotEmpty(t, schema.StaticContent)
	})

	t.Run("HTML content", func(t *testing.T) {
		content := []byte(`<html><body>Hello</body></html>`)
		schema, err := BuildSchemaFromContent(content, "text/html")
		assert.NoError(t, err)

		assert.Equal(t, "string", schema.Type)
		assert.NotEmpty(t, schema.StaticContent)
	})

	t.Run("Plain text content", func(t *testing.T) {
		content := []byte(`Hello, World!`)
		schema, err := BuildSchemaFromContent(content, "text/plain")
		assert.NoError(t, err)

		assert.Equal(t, "string", schema.Type)
		assert.Equal(t, "Hello, World!", schema.StaticContent)
	})

	t.Run("Binary content", func(t *testing.T) {
		content := []byte("binary data")
		schema, err := BuildSchemaFromContent(content, "application/pdf")
		assert.NoError(t, err)

		assert.Equal(t, "string", schema.Type)
		assert.Equal(t, "binary", schema.Format)
		assert.NotEmpty(t, schema.StaticContent)
	})

	t.Run("Invalid JSON", func(t *testing.T) {
		content := []byte(`{invalid json}`)
		_, err := BuildSchemaFromContent(content, "application/json")
		assert.Error(t, err)
	})

	t.Run("Empty array", func(t *testing.T) {
		content := []byte(`[]`)
		schema, err := BuildSchemaFromContent(content, "application/json")
		assert.NoError(t, err)

		assert.Equal(t, "array", schema.Type)
		assert.NotNil(t, schema.Items)
		assert.Equal(t, "string", schema.Items.Type)
	})

	t.Run("Invalid XML", func(t *testing.T) {
		content := []byte(`<invalid><unclosed>`)
		_, err := BuildSchemaFromContent(content, "application/xml")
		assert.Error(t, err)
	})

	t.Run("JSON with null value", func(t *testing.T) {
		content := []byte(`{"value": null}`)
		schema, err := BuildSchemaFromContent(content, "application/json")
		assert.NoError(t, err)

		assert.Equal(t, "object", schema.Type)
		assert.Contains(t, schema.Properties, "value")
		assert.Equal(t, "null", schema.Properties["value"].Type)
		assert.True(t, schema.Properties["value"].Nullable)
	})
}

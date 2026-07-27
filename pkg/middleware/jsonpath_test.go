package middleware

import (
	"testing"

	assert2 "github.com/stretchr/testify/assert"
)

func TestExtractJSONPath(t *testing.T) {
	assert := assert2.New(t)

	body := []byte(`{
		"data": {
			"name": "Jane",
			"address": {
				"zip": "12345",
				"city": "Springfield"
			},
			"items": [
				{"id": 1, "label": "first"},
				{"id": 2, "label": "second"}
			],
			"count": 42,
			"active": true
		}
	}`)

	tests := []struct {
		name     string
		path     string
		expected any
	}{
		{"simple nested", "data.name", "Jane"},
		{"deep nested", "data.address.zip", "12345"},
		{"array index 0", "data.items[0].label", "first"},
		{"array index 1", "data.items[1].id", float64(2)},
		{"array wildcard", "data.items.label", "first"},
		{"number value", "data.count", float64(42)},
		{"bool value", "data.active", true},
		{"top-level key", "data", map[string]any{
			"name": "Jane",
			"address": map[string]any{
				"zip":  "12345",
				"city": "Springfield",
			},
			"items": []any{
				map[string]any{"id": float64(1), "label": "first"},
				map[string]any{"id": float64(2), "label": "second"},
			},
			"count":  float64(42),
			"active": true,
		}},
		{"non-existent key", "data.missing", nil},
		{"non-existent nested", "data.address.state", nil},
		{"non-existent top", "missing", nil},
		{"array out of bounds", "data.items[5].label", nil},
		{"non-existent deep path", "data.address.state.code", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJSONPath(body, tt.path)
			assert.Equal(tt.expected, result)
		})
	}

	t.Run("top-level array index", func(t *testing.T) {
		arrBody := []byte(`[{"name":"doggie","tag":"fundamental-window","id":9134332231560706000}]`)
		result := extractJSONPath(arrBody, "[0].name")
		assert.Equal("doggie", result)
	})

	t.Run("top-level array nested field", func(t *testing.T) {
		arrBody := []byte(`[{"name":"doggie","tag":"fundamental-window","id":9134332231560706000},{"name":"kitty","tag":"other","id":2}]`)
		result := extractJSONPath(arrBody, "[1].name")
		assert.Equal("kitty", result)
	})

	t.Run("top-level array out of bounds", func(t *testing.T) {
		arrBody := []byte(`[{"name":"doggie"}]`)
		result := extractJSONPath(arrBody, "[5].name")
		assert.Nil(result)
	})

	t.Run("top-level array wildcard", func(t *testing.T) {
		arrBody := []byte(`[{"name":"doggie"},{"name":"kitty"}]`)
		result := extractJSONPath(arrBody, "name")
		assert.Equal("doggie", result)
	})

	t.Run("top-level array wildcard no match", func(t *testing.T) {
		arrBody := []byte(`[{"name":"doggie"},{"name":"kitty"}]`)
		result := extractJSONPath(arrBody, "missing")
		assert.Nil(result)
	})

	t.Run("nested array wildcard no match", func(t *testing.T) {
		result := extractJSONPath(body, "data.items.missing")
		assert.Nil(result)
	})

	t.Run("path through scalar value", func(t *testing.T) {
		result := extractJSONPath(body, "data.name.deeper")
		assert.Nil(result)
	})

	t.Run("path through null value", func(t *testing.T) {
		nullBody := []byte(`{"data":null}`)
		result := extractJSONPath(nullBody, "data.name")
		assert.Nil(result)
	})

	t.Run("invalid JSON returns nil", func(t *testing.T) {
		result := extractJSONPath([]byte("not json"), "data.name")
		assert.Nil(result)
	})

	t.Run("nil body returns nil", func(t *testing.T) {
		result := extractJSONPath(nil, "data.name")
		assert.Nil(result)
	})
}

func TestExtractBodyValue(t *testing.T) {
	assert := assert2.New(t)

	t.Run("JSON body extraction", func(t *testing.T) {
		body := []byte(`{"data":{"name":"Jane"}}`)
		val := extractBodyValue(body, "application/json", "data.name")
		assert.Equal("Jane", val)
	})

	t.Run("form-encoded body extraction", func(t *testing.T) {
		body := []byte("amount=50&biller=BLR0001&reference=REF123")
		val := extractBodyValue(body, "application/x-www-form-urlencoded", "biller")
		assert.Equal("BLR0001", val)
	})

	t.Run("form-encoded with charset", func(t *testing.T) {
		body := []byte("name=Jane&zip=12345")
		val := extractBodyValue(body, "application/x-www-form-urlencoded; charset=UTF-8", "zip")
		assert.Equal("12345", val)
	})

	t.Run("missing field everywhere returns nil", func(t *testing.T) {
		body := []byte(`{"other":"value"}`)
		val := extractBodyValue(body, "application/json", "missing")
		assert.Nil(val)
	})

	t.Run("form-encoded missing field returns nil", func(t *testing.T) {
		body := []byte("name=Jane&zip=12345")
		val := extractBodyValue(body, "application/x-www-form-urlencoded", "missing")
		assert.Nil(val)
	})

	t.Run("form-encoded empty value is returned", func(t *testing.T) {
		body := []byte("name=&zip=12345")
		val := extractBodyValue(body, "application/x-www-form-urlencoded", "name")
		assert.Equal("", val)
	})
}

func TestFormatValue(t *testing.T) {
	assert := assert2.New(t)

	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{"string", "hello", "hello"},
		{"integer float", float64(42), "42"},
		{"decimal float", 3.14, "3.14"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"nil", nil, "<nil>"},
		{"slice", []int{1, 2}, "[1 2]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(tt.expected, formatValue(tt.input))
		})
	}
}

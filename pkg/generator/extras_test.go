package generator

import (
	"testing"

	"github.com/mockzilla/mockzilla/v2/pkg/schema"
	"github.com/stretchr/testify/assert"
)

func TestEncodeNDJSON(t *testing.T) {
	b, err := encodeNDJSON(map[string]any{"a": 1})
	assert.NoError(t, err)
	assert.Equal(t, `{"a":1}`+"\n", string(b))
}

func TestEncodeNDJSONUnserializable(t *testing.T) {
	_, err := encodeNDJSON(make(chan int))
	assert.Error(t, err)
}

func TestPlaceholderForSchema(t *testing.T) {
	assert.Equal(t, "field", placeholderForSchema(nil, "field"))
	assert.Equal(t, 0, placeholderForSchema(&schema.Schema{Type: "integer"}, "n"))
	assert.Equal(t, 0, placeholderForSchema(&schema.Schema{Type: "number"}, "n"))
	assert.Equal(t, false, placeholderForSchema(&schema.Schema{Type: "boolean"}, "b"))
	assert.Equal(t, []any{}, placeholderForSchema(&schema.Schema{Type: "array"}, "a"))
	assert.Equal(t, map[string]any{}, placeholderForSchema(&schema.Schema{Type: "object"}, "o"))
	assert.Equal(t, "", placeholderForSchema(&schema.Schema{Type: "string"}, "s"))
	assert.Equal(t, "ok", placeholderForSchema(&schema.Schema{Type: "string", Enum: []any{nil, "ok"}}, "s"))
}

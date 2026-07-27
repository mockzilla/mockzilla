package types

import (
	"encoding/json"
	"testing"

	assert2 "github.com/stretchr/testify/assert"
)

func TestParseDottedPath(t *testing.T) {
	assert := assert2.New(t)

	t.Run("simple path", func(t *testing.T) {
		segments := ParseDottedPath("data.name")
		assert.Len(segments, 2)
		assert.Equal("data", segments[0].Key)
		assert.Equal(-1, segments[0].Index)
		assert.Equal("name", segments[1].Key)
	})

	t.Run("array index", func(t *testing.T) {
		segments := ParseDottedPath("data.items[0].name")
		assert.Len(segments, 3)
		assert.Equal("items", segments[1].Key)
		assert.Equal(0, segments[1].Index)
		assert.True(segments[1].IsArr)
	})

	t.Run("invalid array index treated as key", func(t *testing.T) {
		segments := ParseDottedPath("data.items[abc].name")
		assert.Len(segments, 3)
		assert.Equal("items[abc]", segments[1].Key)
		assert.Equal(-1, segments[1].Index)
		assert.False(segments[1].IsArr)
	})

	t.Run("bare array index", func(t *testing.T) {
		segments := ParseDottedPath("[0].name")
		assert.Len(segments, 2)
		assert.Equal("", segments[0].Key)
		assert.Equal(0, segments[0].Index)
		assert.True(segments[0].IsArr)
		assert.Equal("name", segments[1].Key)
	})

	t.Run("empty path", func(t *testing.T) {
		segments := ParseDottedPath("")
		assert.Empty(segments)
	})
}

func TestGetValueByJSONPath(t *testing.T) {
	assert := assert2.New(t)

	decode := func(src string) any {
		var res any
		if err := json.Unmarshal([]byte(src), &res); err != nil {
			t.Fatalf("invalid test json: %v", err)
		}
		return res
	}

	data := decode(`{
		"order": {
			"payment": {"amount": {"currency": "EUR", "value": 10.5}},
			"amounts": [
				{"currency": "USD"},
				{"currency": "GBP"}
			],
			"paid": true
		}
	}`)

	tests := []struct {
		name     string
		path     string
		expected any
	}{
		{"nested object", "order.payment.amount.currency", "EUR"},
		{"number value", "order.payment.amount.value", 10.5},
		{"bool value", "order.paid", true},
		{"array index", "order.amounts[0].currency", "USD"},
		{"array second index", "order.amounts[1].currency", "GBP"},
		{"array wildcard", "order.amounts.currency", "USD"},
		{"array out of bounds", "order.amounts[5].currency", nil},
		{"missing key", "order.missing.currency", nil},
		{"path through scalar", "order.paid.currency", nil},
		{"empty path", "", data},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(tt.expected, GetValueByJSONPath(data, tt.path))
		})
	}

	t.Run("top-level array", func(t *testing.T) {
		arr := decode(`[{"currency":"USD"},{"currency":"GBP"}]`)
		assert.Equal("GBP", GetValueByJSONPath(arr, "[1].currency"))
		assert.Equal("USD", GetValueByJSONPath(arr, "currency"))
	})

	t.Run("nil data", func(t *testing.T) {
		assert.Nil(GetValueByJSONPath(nil, "order.currency"))
	})
}

package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type decodeTarget struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestDecodeValue(t *testing.T) {
	t.Run("nil value", func(t *testing.T) {
		_, ok := DecodeValue[decodeTarget](nil)
		assert.False(t, ok)
	})

	t.Run("direct pointer passes through", func(t *testing.T) {
		want := &decodeTarget{Name: "a", Count: 1}

		got, ok := DecodeValue[decodeTarget](want)
		assert.True(t, ok)
		assert.Same(t, want, got)
	})

	t.Run("json shape is re-parsed", func(t *testing.T) {
		got, ok := DecodeValue[decodeTarget](map[string]any{"name": "b", "count": float64(2)})
		assert.True(t, ok)
		assert.Equal(t, &decodeTarget{Name: "b", Count: 2}, got)
	})

	t.Run("mismatched shape fails", func(t *testing.T) {
		_, ok := DecodeValue[decodeTarget]("not-an-object")
		assert.False(t, ok)
	})

	t.Run("unmarshalable value fails", func(t *testing.T) {
		_, ok := DecodeValue[decodeTarget](make(chan int))
		assert.False(t, ok)
	})
}

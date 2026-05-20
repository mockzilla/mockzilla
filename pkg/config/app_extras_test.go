package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedisConfigGetAddress(t *testing.T) {
	t.Run("host wins over address", func(t *testing.T) {
		r := &RedisConfig{Host: "h", Port: "1234", Address: "ignored:0"}
		assert.Equal(t, "h:1234", r.GetAddress())
	})

	t.Run("address fallback", func(t *testing.T) {
		r := &RedisConfig{Address: "addr:6379"}
		assert.Equal(t, "addr:6379", r.GetAddress())
	})

	t.Run("empty", func(t *testing.T) {
		r := &RedisConfig{}
		assert.Equal(t, "", r.GetAddress())
	})
}

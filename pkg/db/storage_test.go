package db

import (
	"testing"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/stretchr/testify/assert"
)

func TestNewStorage(t *testing.T) {
	t.Run("nil config returns memory storage", func(t *testing.T) {
		storage := NewStorage(nil)
		assert.NotNil(t, storage)
		_, isMemory := storage.(*memoryStorage)
		assert.True(t, isMemory)
		storage.Close()
	})

	t.Run("empty type returns memory storage", func(t *testing.T) {
		cfg := &config.StorageConfig{Type: ""}
		storage := NewStorage(cfg)
		assert.NotNil(t, storage)
		_, isMemory := storage.(*memoryStorage)
		assert.True(t, isMemory)
		storage.Close()
	})

	t.Run("memory type returns memory storage", func(t *testing.T) {
		cfg := &config.StorageConfig{Type: config.StorageTypeMemory}
		storage := NewStorage(cfg)
		assert.NotNil(t, storage)
		_, isMemory := storage.(*memoryStorage)
		assert.True(t, isMemory)
		storage.Close()
	})

	t.Run("unknown type falls back to memory", func(t *testing.T) {
		cfg := &config.StorageConfig{Type: "unknown"}
		storage := NewStorage(cfg)
		assert.NotNil(t, storage)
		_, isMemory := storage.(*memoryStorage)
		assert.True(t, isMemory)
		storage.Close()
	})

	t.Run("redis with invalid config falls back to memory", func(t *testing.T) {
		cfg := &config.StorageConfig{
			Type:  config.StorageTypeRedis,
			Redis: &config.RedisConfig{Address: "invalid:99999"},
		}
		storage := NewStorage(cfg)
		assert.NotNil(t, storage)
		_, isMemory := storage.(*memoryStorage)
		assert.True(t, isMemory)
		storage.Close()
	})

	t.Run("redis via driver config with invalid config falls back to memory", func(t *testing.T) {
		cfg := &config.StorageConfig{
			Type: config.StorageTypeRedis,
			DriverConfig: map[string]any{
				"address": "invalid:99999",
			},
		}
		storage := NewStorage(cfg)
		assert.NotNil(t, storage)
		_, isMemory := storage.(*memoryStorage)
		assert.True(t, isMemory)
		storage.Close()
	})
}

func TestStorageInterface(t *testing.T) {
	var _ Storage = (*memoryStorage)(nil)
	var _ Storage = (*redisStorage)(nil)
}

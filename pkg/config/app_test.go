package config

import (
	"testing"
	"time"

	assert2 "github.com/stretchr/testify/assert"
)

func TestNewDefaultAppConfig(t *testing.T) {
	assert := assert2.New(t)

	t.Run("creates default config with correct values", func(t *testing.T) {
		baseDir := "/test/base"
		cfg := NewDefaultAppConfig(baseDir)

		assert.NotNil(cfg)
		assert.Equal("API Explorer", cfg.Title)
		assert.Equal(2200, cfg.Port)
		assert.Equal("/", cfg.HomeURL)
		assert.Equal("/.services", cfg.ServiceURL)
		assert.Equal("in-", cfg.ContextAreaPrefix)
		assert.NotNil(cfg.History)
		assert.Equal(DefaultHistoryDuration, cfg.History.Duration)
		assert.Equal("/.history", cfg.History.URL)
		assert.False(cfg.DisableUI)

		// Check paths
		assert.NotNil(cfg.Paths)
		assert.Equal(baseDir, cfg.Paths.Base)

		// Check editor config
		assert.NotNil(cfg.Editor)
		assert.Equal("chrome", cfg.Editor.Theme)
		assert.Equal(14, cfg.Editor.FontSize)
	})
}

func TestStorageType(t *testing.T) {
	assert := assert2.New(t)

	t.Run("storage type constants", func(t *testing.T) {
		assert.Equal(StorageType("memory"), StorageTypeMemory)
		assert.Equal(StorageType("redis"), StorageTypeRedis)
	})
}

func TestStorageConfig(t *testing.T) {
	assert := assert2.New(t)

	t.Run("empty storage config", func(t *testing.T) {
		cfg := &StorageConfig{}
		assert.Empty(cfg.Type)
		assert.Nil(cfg.Redis)
	})

	t.Run("memory storage config", func(t *testing.T) {
		cfg := &StorageConfig{
			Type: StorageTypeMemory,
		}
		assert.Equal(StorageTypeMemory, cfg.Type)
		assert.Nil(cfg.Redis)
	})

	t.Run("redis storage config", func(t *testing.T) {
		cfg := &StorageConfig{
			Type: StorageTypeRedis,
			Redis: &RedisConfig{
				Address:  "localhost:6379",
				Password: "secret",
				DB:       1,
			},
		}
		assert.Equal(StorageTypeRedis, cfg.Type)
		assert.NotNil(cfg.Redis)
		assert.Equal("localhost:6379", cfg.Redis.Address)
		assert.Equal("secret", cfg.Redis.Password)
		assert.Equal(1, cfg.Redis.DB)
	})
}

func TestStorageConfig_DriverOptions(t *testing.T) {
	assert := assert2.New(t)

	t.Run("returns DriverConfig when set", func(t *testing.T) {
		cfg := &StorageConfig{
			Type:         StorageTypeRedis,
			DriverConfig: map[string]any{"address": "myhost:6379"},
			Redis:        &RedisConfig{Address: "other:6379"},
		}
		opts := cfg.DriverOptions()
		assert.Equal("myhost:6379", opts["address"])
	})

	t.Run("falls back to redis typed config", func(t *testing.T) {
		cfg := &StorageConfig{
			Type: StorageTypeRedis,
			Redis: &RedisConfig{
				Address:  "localhost:6379",
				Password: "secret",
				DB:       2,
			},
		}
		opts := cfg.DriverOptions()
		assert.Equal("localhost:6379", opts["address"])
		assert.Equal("secret", opts["password"])
		assert.Equal(2, opts["db"])
	})

	t.Run("returns nil for redis without typed config", func(t *testing.T) {
		cfg := &StorageConfig{Type: StorageTypeRedis}
		opts := cfg.DriverOptions()
		assert.Nil(opts)
	})

	t.Run("returns nil for unknown type without DriverConfig", func(t *testing.T) {
		cfg := &StorageConfig{Type: "custom"}
		opts := cfg.DriverOptions()
		assert.Nil(opts)
	})

	t.Run("returns nil for memory type", func(t *testing.T) {
		cfg := &StorageConfig{Type: StorageTypeMemory}
		opts := cfg.DriverOptions()
		assert.Nil(opts)
	})
}

func TestRedisConfig(t *testing.T) {
	assert := assert2.New(t)

	t.Run("zero values", func(t *testing.T) {
		cfg := &RedisConfig{}
		assert.Empty(cfg.Address)
		assert.Empty(cfg.Password)
		assert.Zero(cfg.DB)
	})

	t.Run("all fields set", func(t *testing.T) {
		cfg := &RedisConfig{
			Address:  "redis.example.com:6379",
			Password: "mypassword",
			DB:       5,
		}
		assert.Equal("redis.example.com:6379", cfg.Address)
		assert.Equal("mypassword", cfg.Password)
		assert.Equal(5, cfg.DB)
	})
}

func TestAppConfig_WithStorage(t *testing.T) {
	assert := assert2.New(t)

	t.Run("app config without storage", func(t *testing.T) {
		cfg := NewDefaultAppConfig("/test")
		assert.Nil(cfg.Storage)
	})

	t.Run("app config with storage", func(t *testing.T) {
		cfg := NewDefaultAppConfig("/test")
		cfg.Storage = &StorageConfig{
			Type: StorageTypeRedis,
			Redis: &RedisConfig{
				Address: "localhost:6379",
			},
		}
		assert.NotNil(cfg.Storage)
		assert.Equal(StorageTypeRedis, cfg.Storage.Type)
		assert.NotNil(cfg.Storage.Redis)
	})
}

func TestNewAppConfigFromBytes(t *testing.T) {
	assert := assert2.New(t)

	t.Run("parses valid yaml", func(t *testing.T) {
		yaml := []byte(`
title: My App
port: 3000
homeURL: /home
serviceURL: /api
contextAreaPrefix: ctx-
disableUI: true
history:
  duration: 10m
editor:
  theme: monokai
  fontSize: 14
`)
		cfg, err := NewAppConfigFromBytes(yaml, "/base")
		assert.NoError(err)
		assert.Equal("My App", cfg.Title)
		assert.Equal(3000, cfg.Port)
		assert.Equal("/home", cfg.HomeURL)
		assert.Equal("/api", cfg.ServiceURL)
		assert.Equal("ctx-", cfg.ContextAreaPrefix)
		assert.True(cfg.DisableUI)
		assert.Equal(10*time.Minute, cfg.History.Duration)
		assert.Equal("monokai", cfg.Editor.Theme)
		assert.Equal(14, cfg.Editor.FontSize)
		assert.Equal("/base", cfg.Paths.Base)
	})

	t.Run("uses defaults for missing fields", func(t *testing.T) {
		yaml := []byte(`
title: Custom Title
`)
		cfg, err := NewAppConfigFromBytes(yaml, "/test")
		assert.NoError(err)
		assert.Equal("Custom Title", cfg.Title)
		assert.Equal(2200, cfg.Port)                               // default
		assert.Equal("/", cfg.HomeURL)                             // default
		assert.Equal(DefaultHistoryDuration, cfg.History.Duration) // default
	})

	t.Run("parses storage config", func(t *testing.T) {
		yaml := []byte(`
storage:
  type: redis
  redis:
    address: localhost:6379
    password: secret
    db: 1
`)
		cfg, err := NewAppConfigFromBytes(yaml, "/test")
		assert.NoError(err)
		assert.NotNil(cfg.Storage)
		assert.Equal(StorageTypeRedis, cfg.Storage.Type)
		assert.Equal("localhost:6379", cfg.Storage.Redis.Address)
		assert.Equal("secret", cfg.Storage.Redis.Password)
		assert.Equal(1, cfg.Storage.Redis.DB)
	})

	t.Run("extracts driver config from type-named section", func(t *testing.T) {
		yaml := []byte(`
storage:
  type: dynamodb
  dynamodb:
    table: mockzilla
    region: us-east-1
`)
		cfg, err := NewAppConfigFromBytes(yaml, "/test")
		assert.NoError(err)
		assert.NotNil(cfg.Storage)
		assert.Equal(StorageType("dynamodb"), cfg.Storage.Type)
		assert.Equal("mockzilla", cfg.Storage.DriverConfig["table"])
		assert.Equal("us-east-1", cfg.Storage.DriverConfig["region"])
	})

	t.Run("extracts redis driver config", func(t *testing.T) {
		yaml := []byte(`
storage:
  type: redis
  redis:
    address: localhost:6379
    password: secret
`)
		cfg, err := NewAppConfigFromBytes(yaml, "/test")
		assert.NoError(err)
		assert.NotNil(cfg.Storage.DriverConfig)
		assert.Equal("localhost:6379", cfg.Storage.DriverConfig["address"])
		assert.Equal("secret", cfg.Storage.DriverConfig["password"])
	})

	t.Run("parses baseURL and internalURL", func(t *testing.T) {
		yaml := []byte(`
baseURL: https://api.example.com
internalURL: http://localhost:2200
`)
		cfg, err := NewAppConfigFromBytes(yaml, "/test")
		assert.NoError(err)
		assert.Equal("https://api.example.com", cfg.BaseURL)
		assert.Equal("http://localhost:2200", cfg.InternalURL)
	})

	t.Run("parses extra config map", func(t *testing.T) {
		yaml := []byte(`
extra:
  apiKey: secret123
  maxRetries: 3
  nested:
    foo: bar
`)
		cfg, err := NewAppConfigFromBytes(yaml, "/test")
		assert.NoError(err)
		assert.NotNil(cfg.Extra)
		assert.Equal("secret123", cfg.Extra["apiKey"])
		assert.Equal(3, cfg.Extra["maxRetries"])
		nested, ok := cfg.Extra["nested"].(map[string]any)
		assert.True(ok)
		assert.Equal("bar", nested["foo"])
	})

	t.Run("returns error for invalid yaml", func(t *testing.T) {
		yaml := []byte(`
invalid: [yaml
`)
		_, err := NewAppConfigFromBytes(yaml, "/test")
		assert.Error(err)
		assert.Contains(err.Error(), "unmarshalling app config")
	})

	t.Run("sets paths from baseDir", func(t *testing.T) {
		yaml := []byte(`title: Test`)
		cfg, err := NewAppConfigFromBytes(yaml, "/my/base")
		assert.NoError(err)
		assert.Equal("/my/base", cfg.Paths.Base)
		assert.Equal("/my/base/resources/data", cfg.Paths.Data)
	})
}

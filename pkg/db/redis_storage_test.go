package db

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const redisAddressEnv = "REDIS_TEST_ADDRESS"

func TestRedisStorage_NewRedisStorage(t *testing.T) {
	t.Run("nil config returns error", func(t *testing.T) {
		storage, err := newRedisStorage(nil)
		assert.Nil(t, storage)
		assert.Error(t, err)
	})

	t.Run("invalid address returns error", func(t *testing.T) {
		cfg := &config.RedisConfig{Address: "invalid:99999"}
		storage, err := newRedisStorage(cfg)
		assert.Nil(t, storage)
		assert.Error(t, err)
	})

	t.Run("successful connection", func(t *testing.T) {
		cfg := &config.RedisConfig{Address: redisAddress(t)}

		storage, err := newRedisStorage(cfg)
		assert.NoError(t, err)
		assert.NotNil(t, storage)
		storage.Close()
	})
}

func TestRedisStorage_NewDB(t *testing.T) {
	ctx := context.Background()
	cfg := &config.RedisConfig{Address: redisAddress(t)}

	storage, err := newRedisStorage(cfg)
	assert.NoError(t, err)
	defer storage.Close()

	db1 := storage.NewDB("service1", 5*time.Minute)
	db2 := storage.NewDB("service2", 5*time.Minute)

	assert.NotNil(t, db1)
	assert.NotNil(t, db2)

	// Verify they have separate tables (via key prefixing)
	db1.Table("users").Set(ctx, "key", "value1", 0)
	db2.Table("users").Set(ctx, "key", "value2", 0)

	val1, _ := db1.Table("users").Get(ctx, "key")
	val2, _ := db2.Table("users").Get(ctx, "key")

	assert.Equal(t, "value1", val1)
	assert.Equal(t, "value2", val2)
}

func TestRedisStorage_SharedBackend(t *testing.T) {
	ctx := context.Background()
	cfg := &config.RedisConfig{Address: redisAddress(t)}

	storage, err := newRedisStorage(cfg)
	assert.NoError(t, err)
	defer storage.Close()

	// Get the same service twice - should return same DB instance
	db1 := storage.NewDB("myservice", 5*time.Minute)
	db2 := storage.NewDB("myservice", 5*time.Minute)

	db1.Table("data").Set(ctx, "key", "shared_value", 0)

	// Both should see the same data
	val1, ok1 := db1.Table("data").Get(ctx, "key")
	val2, ok2 := db2.Table("data").Get(ctx, "key")

	assert.True(t, ok1)
	assert.True(t, ok2)
	assert.Equal(t, "shared_value", val1)
	assert.Equal(t, "shared_value", val2)
}

func TestRedisStorage_History(t *testing.T) {
	ctx := context.Background()
	cfg := &config.RedisConfig{Address: redisAddress(t)}

	storage, err := newRedisStorage(cfg)
	assert.NoError(t, err)
	defer storage.Close()

	db := storage.NewDB("testservice", 0)
	history := db.History()

	assert.NotNil(t, history)

	result := history.Set(ctx, "/test", &HistoryRequest{Method: "GET", URL: "/test"}, nil)

	assert.Equal(t, "/test", result.Resource)
}

func TestRedisStorage_Close(t *testing.T) {
	cfg := &config.RedisConfig{Address: redisAddress(t)}

	storage, err := newRedisStorage(cfg)
	assert.NoError(t, err)

	// Create some service DBs
	db1 := storage.NewDB("service1", 5*time.Minute)
	db2 := storage.NewDB("service2", 5*time.Minute)

	// Close individual service DBs (no-op for Redis)
	db1.Close()
	db2.Close()

	// Close storage
	storage.Close()
}

// redisAddress is a real server when the environment names one, and a fake otherwise, so the
// same tests cover an ordinary pull request and the version matrix without being written twice.
func redisAddress(t *testing.T) string {
	t.Helper()

	address := os.Getenv(redisAddressEnv)
	if address == "" {
		return miniredis.RunT(t).Addr()
	}

	// A real server is reused by every test in the run, so each one starts from empty.
	client := redis.NewClient(&redis.Options{Addr: address})
	defer func() { _ = client.Close() }()

	require.NoError(t, client.FlushDB(t.Context()).Err())

	return address
}

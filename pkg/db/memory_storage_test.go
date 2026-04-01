package db

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMemoryStorage_NewDB(t *testing.T) {
	ctx := context.Background()
	storage := newMemoryStorage()
	defer storage.Close()

	db1 := storage.NewDB("service1", 5*time.Minute)
	db2 := storage.NewDB("service2", 5*time.Minute)

	assert.NotNil(t, db1)
	assert.NotNil(t, db2)

	// Verify they have separate tables
	db1.Table("users").Set(ctx, "key", "value1", 0)
	db2.Table("users").Set(ctx, "key", "value2", 0)

	val1, _ := db1.Table("users").Get(ctx, "key")
	val2, _ := db2.Table("users").Get(ctx, "key")

	assert.Equal(t, "value1", val1)
	assert.Equal(t, "value2", val2)
}

func TestMemoryStorage_SharedBackend(t *testing.T) {
	ctx := context.Background()
	storage := newMemoryStorage()
	defer storage.Close()

	// Get the same service twice - should share underlying tables
	db1 := storage.NewDB("myservice", 5*time.Minute)
	db2 := storage.NewDB("myservice", 5*time.Minute)

	db1.Table("data").Set(ctx, "key", "shared_value", 0)

	// Both should see the same data since they share the storage
	val1, ok1 := db1.Table("data").Get(ctx, "key")
	val2, ok2 := db2.Table("data").Get(ctx, "key")

	assert.True(t, ok1)
	assert.True(t, ok2)
	assert.Equal(t, "shared_value", val1)
	assert.Equal(t, "shared_value", val2)
}

func TestMemoryStorage_History(t *testing.T) {
	ctx := context.Background()
	storage := newMemoryStorage()
	defer storage.Close()

	db := storage.NewDB("testservice", 0)
	history := db.History()

	assert.NotNil(t, history)

	result := history.Set(ctx, "/test", &HistoryRequest{Method: "GET", URL: "/test"}, nil)

	assert.Equal(t, "/test", result.Resource)
}

func TestMemoryStorage_Close(t *testing.T) {
	ctx := context.Background()
	storage := newMemoryStorage()

	// Create some service DBs
	db1 := storage.NewDB("service1", 50*time.Millisecond)
	db2 := storage.NewDB("service2", 50*time.Millisecond)

	// Add some data
	histReq := &HistoryRequest{Method: "GET", URL: "/test"}
	db1.History().Set(ctx, "/test1", histReq, nil)
	db2.History().Set(ctx, "/test2", histReq, nil)

	// Close individual service DBs
	db1.Close()
	db2.Close()

	// Close storage (should be safe even after service DBs are closed)
	storage.Close()
}

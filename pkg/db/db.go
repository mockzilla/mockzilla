// Package db provides a shared storage abstraction with support for
// multiple backends (memory, redis) and per-service isolated views.
package db

import (
	"context"
	"time"
)

// DB is a per-service database that provides access to named tables.
// Each service gets its own isolated DB instance.
type DB interface {
	// History returns the history table with typed methods for request/response tracking.
	History() HistoryTable

	// Table returns a generic key-value table by name.
	// Tables are created lazily on first access.
	Table(name string) Table

	// Close releases any resources held by the database.
	Close()
}

// Table is a generic key-value store with optional TTL support.
type Table interface {
	// Get retrieves a value by key.
	Get(ctx context.Context, key string) (any, bool)

	// Set stores a value with the given key.
	// If ttl is 0, the value never expires.
	// If ttl > 0, the value expires after the given duration.
	Set(ctx context.Context, key string, value any, ttl time.Duration)

	// Delete removes a value by key.
	Delete(ctx context.Context, key string)

	// Data returns a copy of all data in the table.
	Data(ctx context.Context) map[string]any

	// Clear removes all data from the table.
	Clear(ctx context.Context)
}

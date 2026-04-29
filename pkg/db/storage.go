// Package db provides a shared storage abstraction with support for
// multiple backends (memory, redis, and external drivers) and per-service isolated views.
package db

import (
	"log/slog"
	"time"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
)

// Storage is the shared storage backend that can provide per-service DB instances.
// There should be only one Storage instance per application.
type Storage interface {
	// NewDB returns a DB scoped to a specific service.
	// The returned DB shares the underlying storage but isolates data via key prefixing.
	NewDB(serviceName string, historyDuration time.Duration) DB

	// Close releases any resources held by the storage backend.
	Close()
}

// NewStorage creates a shared storage backend based on configuration.
// If storageCfg is nil or type is memory, returns an in-memory storage.
// For other types, the corresponding driver must be registered via Register.
func NewStorage(storageCfg *config.StorageConfig) Storage {
	if storageCfg == nil || storageCfg.Type == "" || storageCfg.Type == config.StorageTypeMemory {
		return newMemoryStorage()
	}

	factory := lookupDriver(string(storageCfg.Type))
	if factory == nil {
		slog.Warn("Unknown storage type, falling back to memory", "type", storageCfg.Type)
		return newMemoryStorage()
	}

	options := storageCfg.DriverOptions()
	storage, err := factory(options)
	if err != nil {
		slog.Error("Failed to create storage, falling back to memory", "type", storageCfg.Type, "error", err)
		return newMemoryStorage()
	}

	return storage
}

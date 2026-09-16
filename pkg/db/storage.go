// Package db provides a shared storage abstraction with support for
// multiple backends (memory, redis, and external drivers) and per-service isolated views.
package db

import (
	"errors"
	"fmt"
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
// When the backend cannot be opened it falls back to memory, unless the config
// is strict, in which case it panics so the process does not start without it.
func NewStorage(storageCfg *config.StorageConfig) Storage {
	storage, err := OpenStorage(storageCfg)
	if err == nil {
		return storage
	}

	if storageCfg.Strict {
		panic(fmt.Sprintf("storage is strict and could not be opened: %v", err))
	}

	if errors.Is(err, ErrUnknownStorageType) {
		slog.Warn("Unknown storage type, falling back to memory", "type", storageCfg.Type)
	} else {
		slog.Error("Failed to create storage, falling back to memory", "type", storageCfg.Type, "error", err)
	}
	return newMemoryStorage()
}

// OpenStorage creates the configured storage backend and returns an error
// instead of falling back to memory when it cannot be opened.
func OpenStorage(storageCfg *config.StorageConfig) (Storage, error) {
	if storageCfg == nil || storageCfg.Type == "" || storageCfg.Type == config.StorageTypeMemory {
		return newMemoryStorage(), nil
	}

	factory := lookupDriver(string(storageCfg.Type))
	if factory == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknownStorageType, storageCfg.Type)
	}

	storage, err := factory(storageCfg.DriverOptions())
	if err != nil {
		return nil, fmt.Errorf("opening %s storage: %w", storageCfg.Type, err)
	}
	return storage, nil
}

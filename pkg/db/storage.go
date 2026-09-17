// Package db provides a shared storage abstraction with support for
// multiple backends (memory, redis, and external drivers) and per-service isolated views.
package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
)

const installTimeout = time.Minute

// Storage is the shared storage backend that can provide per-service DB instances.
// There should be only one Storage instance per application.
type Storage interface {
	// NewDB returns a DB scoped to a specific service.
	// The returned DB shares the underlying storage but isolates data via key prefixing.
	NewDB(serviceName string, historyDuration time.Duration) DB

	// Close releases any resources held by the storage backend.
	Close()
}

// Installer is an optional capability of a Storage that needs a schema.
// OpenStorage calls Install once the backend is open. When storage.install is
// false it calls VerifyInstall instead, so a missing schema still fails the open.
type Installer interface {
	// Install creates or migrates the schema. It must be safe to call on every
	// start and from several replicas at once.
	Install(ctx context.Context) error

	// VerifyInstall changes nothing. It returns an error when the schema is
	// missing or older than the driver needs.
	VerifyInstall(ctx context.Context) error
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
// A backend that is an Installer gets its schema installed or verified first.
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

	if err := install(storage, storageCfg); err != nil {
		storage.Close()
		return nil, err
	}
	return storage, nil
}

// install runs the backend's install step, or only verifies it when
// storage.install is off. A backend that is not an Installer is left alone.
func install(storage Storage, storageCfg *config.StorageConfig) error {
	installer, ok := storage.(Installer)
	if !ok {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()

	if !storageCfg.InstallEnabled() {
		if err := installer.VerifyInstall(ctx); err != nil {
			return fmt.Errorf("verifying %s storage install: %w", storageCfg.Type, err)
		}
		return nil
	}

	if err := installer.Install(ctx); err != nil {
		return fmt.Errorf("installing %s storage: %w", storageCfg.Type, err)
	}
	return nil
}

package db

import (
	"fmt"
	"sync"

	"go.yaml.in/yaml/v4"
)

// StorageFactory creates a Storage backend from raw driver options.
// Options are typically parsed from the "options" key in storage config YAML.
// Use ParseOptions to decode them into a typed struct.
type StorageFactory func(options map[string]any) (Storage, error)

var (
	driversMu sync.RWMutex
	drivers   = make(map[string]StorageFactory)
)

// Register makes a storage backend available by the provided name.
// If Register is called twice with the same name or if factory is nil, it panics.
//
// Built-in drivers register themselves via init().
// External drivers can register in their own init() — users activate them
// with a blank import:
//
//	import _ "github.com/someone/mockzilla-db-dynamodb"
func Register(name string, factory StorageFactory) {
	driversMu.Lock()
	defer driversMu.Unlock()

	if factory == nil {
		panic("db: Register factory is nil")
	}
	if _, dup := drivers[name]; dup {
		panic("db: Register called twice for driver " + name)
	}
	drivers[name] = factory
}

// lookupDriver returns the factory for name, or nil if not registered.
func lookupDriver(name string) StorageFactory {
	driversMu.RLock()
	defer driversMu.RUnlock()
	return drivers[name]
}

// Drivers returns a sorted list of registered driver names.
func Drivers() []string {
	driversMu.RLock()
	defer driversMu.RUnlock()

	names := make([]string, 0, len(drivers))
	for name := range drivers {
		names = append(names, name)
	}
	return names
}

// ParseOptions decodes a raw options map into a typed configuration struct.
// The target type T should have `yaml` struct tags matching the option keys.
//
// Example:
//
//	type MyDriverConfig struct {
//	    Host string `yaml:"host"`
//	    Port int    `yaml:"port"`
//	}
//
//	cfg, err := db.ParseOptions[MyDriverConfig](options)
func ParseOptions[T any](options map[string]any) (*T, error) {
	data, err := yaml.Marshal(options)
	if err != nil {
		return nil, fmt.Errorf("marshaling options: %w", err)
	}
	var cfg T
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing options: %w", err)
	}
	return &cfg, nil
}

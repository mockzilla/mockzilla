package db

import (
	"cmp"
	"fmt"
	"slices"
	"sync"

	"go.yaml.in/yaml/v4"
)

// StorageFactory creates a Storage backend from raw driver options.
// Options are typically parsed from the "options" key in storage config YAML.
// Use ParseOptions to decode them into a typed struct.
type StorageFactory func(options map[string]any) (Storage, error)

// Driver is a storage backend and what it says about itself.
type Driver struct {
	Name    string
	Factory StorageFactory

	// Compat is optional. A driver that leaves it empty still works; it just has
	// nothing to tell an operator about where it runs.
	Compat Compat
}

var (
	driversMu sync.RWMutex
	drivers   = make(map[string]Driver)
)

// Register makes a storage backend available by the provided name.
// Panics on duplicate name or nil factory. External drivers register via init()
// and are activated by blank import: `import _ "github.com/x/mockzilla-db-y"`.
func Register(name string, factory StorageFactory) {
	RegisterDriver(Driver{Name: name, Factory: factory})
}

// RegisterDriver is Register for a backend that also describes where it runs.
func RegisterDriver(driver Driver) {
	driversMu.Lock()
	defer driversMu.Unlock()

	if driver.Factory == nil {
		panic("db: Register factory is nil")
	}
	if _, dup := drivers[driver.Name]; dup {
		panic("db: Register called twice for driver " + driver.Name)
	}
	drivers[driver.Name] = driver
}

// lookupDriver returns the factory for name, or nil if not registered.
func lookupDriver(name string) StorageFactory {
	driversMu.RLock()
	defer driversMu.RUnlock()
	return drivers[name].Factory
}

// Drivers returns a sorted list of registered driver names.
func Drivers() []string {
	driversMu.RLock()
	defer driversMu.RUnlock()

	names := make([]string, 0, len(drivers))
	for name := range drivers {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}

// Registered is every driver compiled into this build, by name, with what each one
// says about itself. It is how a caller lists what this build can store into without
// opening a connection to anything.
func Registered() []Driver {
	driversMu.RLock()
	defer driversMu.RUnlock()

	all := make([]Driver, 0, len(drivers))
	for _, driver := range drivers {
		all = append(all, driver)
	}

	slices.SortFunc(all, func(a, b Driver) int { return cmp.Compare(a.Name, b.Name) })

	return all
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

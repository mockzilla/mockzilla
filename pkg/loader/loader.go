package loader

import (
	"log/slog"
	"os"
	"strconv"
	"sync"

	"github.com/mockzilla/mockzilla/v2/pkg/api"
)

const (
	// DefaultLoadConcurrency is the default number of concurrent service loads
	DefaultLoadConcurrency = 10
)

// RegisterFunc is a function that registers a service with the router
type RegisterFunc func(router *api.Router)

// Registry holds all registeredServices services
type Registry struct {
	mu       sync.RWMutex
	services map[string]RegisterFunc
}

var (
	// DefaultRegistry is the global service registry
	DefaultRegistry = NewRegistry()
)

// NewRegistry creates a new service registry
func NewRegistry() *Registry {
	return &Registry{
		services: make(map[string]RegisterFunc),
	}
}

// Register adds a service to the registry
// This should be called from init() functions in service packages
func (r *Registry) Register(name string, registerFunc RegisterFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.services[name]; exists {
		slog.Warn("Service already registeredServices, overwriting", "name", name)
	}

	r.services[name] = registerFunc
}

// Get retrieves a service registration function by name
func (r *Registry) Get(name string) (RegisterFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	fn, ok := r.services[name]
	return fn, ok
}

// List returns all registeredServices service names
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.services))
	for name := range r.services {
		names = append(names, name)
	}
	return names
}

// LoadAll loads all registeredServices services into the router concurrently
// with a configurable concurrency limit via LOAD_CONCURRENCY env var (default: 10)
func (r *Registry) LoadAll(router *api.Router) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.services) == 0 {
		slog.Warn("No services registeredServices in discovery registry")
		return
	}

	maxConcurrency := getLoadConcurrency()

	semaphore := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for name, registerFunc := range r.services {
		semaphore <- struct{}{} // Acquire
		wg.Add(1)
		go func(name string, fn RegisterFunc) {
			defer wg.Done()
			defer func() { <-semaphore }() // Release
			fn(router)
		}(name, registerFunc)
	}

	wg.Wait()
	slog.Info("All services loaded")
}

// getLoadConcurrency returns the concurrency limit from LOAD_CONCURRENCY env var or default
func getLoadConcurrency() int {
	if val := os.Getenv("LOAD_CONCURRENCY"); val != "" {
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			return n
		}
	}
	return DefaultLoadConcurrency
}

// Register is a convenience function to register a service with the default registry
func Register(name string, registerFunc RegisterFunc) {
	DefaultRegistry.Register(name, registerFunc)
}

// LoadAll is a convenience function to load all services from the default registry concurrently
func LoadAll(router *api.Router) {
	DefaultRegistry.LoadAll(router)
}

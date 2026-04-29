package loader

import (
	"fmt"
	"sync"
	"testing"

	"github.com/mockzilla/mockzilla/v2/pkg/api"
	assert2 "github.com/stretchr/testify/assert"
)

func TestNewRegistry(t *testing.T) {
	assert := assert2.New(t)

	registry := NewRegistry()

	assert.NotNil(registry)
	assert.NotNil(registry.services)
	assert.Equal(0, len(registry.services))
}

func TestRegistry_Register(t *testing.T) {
	assert := assert2.New(t)

	t.Run("register new service", func(t *testing.T) {
		registry := NewRegistry()
		called := false

		registerFunc := func(router *api.Router) {
			called = true
		}

		registry.Register("test-service", registerFunc)

		assert.Equal(1, len(registry.services))
		fn, exists := registry.services["test-service"]
		assert.True(exists)
		assert.NotNil(fn)

		// Verify the function works
		fn(nil)
		assert.True(called)
	})

	t.Run("register duplicate service overwrites", func(t *testing.T) {
		registry := NewRegistry()

		called1 := false
		called2 := false

		registerFunc1 := func(router *api.Router) {
			called1 = true
		}
		registerFunc2 := func(router *api.Router) {
			called2 = true
		}

		registry.Register("duplicate", registerFunc1)
		registry.Register("duplicate", registerFunc2)

		// Should still have only 1 service (overwritten)
		assert.Equal(1, len(registry.services))

		// Verify the second function is registeredServices
		fn, exists := registry.Get("duplicate")
		assert.True(exists)
		fn(nil)
		assert.False(called1, "First function should not be called")
		assert.True(called2, "Second function should be called")
	})

	t.Run("register multiple services", func(t *testing.T) {
		registry := NewRegistry()

		registry.Register("service1", func(router *api.Router) {})
		registry.Register("service2", func(router *api.Router) {})
		registry.Register("service3", func(router *api.Router) {})

		assert.Equal(3, len(registry.services))
	})
}

func TestRegistry_Get(t *testing.T) {
	assert := assert2.New(t)

	t.Run("get existing service", func(t *testing.T) {
		registry := NewRegistry()
		called := false

		registerFunc := func(router *api.Router) {
			called = true
		}

		registry.Register("test-service", registerFunc)

		fn, exists := registry.Get("test-service")
		assert.True(exists)
		assert.NotNil(fn)

		// Verify it's the same function
		fn(nil)
		assert.True(called)
	})

	t.Run("get non-existing service", func(t *testing.T) {
		registry := NewRegistry()

		fn, exists := registry.Get("non-existing")
		assert.False(exists)
		assert.Nil(fn)
	})
}

func TestRegistry_List(t *testing.T) {
	assert := assert2.New(t)

	t.Run("list empty registry", func(t *testing.T) {
		registry := NewRegistry()

		names := registry.List()
		assert.NotNil(names)
		assert.Equal(0, len(names))
	})

	t.Run("list services", func(t *testing.T) {
		registry := NewRegistry()

		registry.Register("service1", func(router *api.Router) {})
		registry.Register("service2", func(router *api.Router) {})
		registry.Register("service3", func(router *api.Router) {})

		names := registry.List()
		assert.Equal(3, len(names))
		assert.Contains(names, "service1")
		assert.Contains(names, "service2")
		assert.Contains(names, "service3")
	})
}

func TestRegistry_LoadAll(t *testing.T) {
	assert := assert2.New(t)

	t.Run("load all services concurrently", func(t *testing.T) {
		registry := NewRegistry()
		router := api.NewRouter()

		callCount := 0
		mu := &sync.Mutex{}

		registry.Register("service1", func(r *api.Router) {
			mu.Lock()
			callCount++
			mu.Unlock()
		})
		registry.Register("service2", func(r *api.Router) {
			mu.Lock()
			callCount++
			mu.Unlock()
		})
		registry.Register("service3", func(r *api.Router) {
			mu.Lock()
			callCount++
			mu.Unlock()
		})

		registry.LoadAll(router)

		// All services should be called
		assert.Equal(3, callCount)
	})

	t.Run("load empty registry does nothing", func(t *testing.T) {
		registry := NewRegistry()
		router := api.NewRouter()

		// Should not panic or error
		registry.LoadAll(router)

		assert.Equal(0, len(registry.services))
	})

	t.Run("services receive router", func(t *testing.T) {
		registry := NewRegistry()
		router := api.NewRouter()

		var receivedRouter *api.Router
		mu := &sync.Mutex{}

		registry.Register("test", func(r *api.Router) {
			mu.Lock()
			receivedRouter = r
			mu.Unlock()
		})

		registry.LoadAll(router)

		assert.Equal(router, receivedRouter)
	})

	t.Run("concurrent loading is safe", func(t *testing.T) {
		registry := NewRegistry()
		router := api.NewRouter()

		callCount := 0
		mu := &sync.Mutex{}

		// Register many services with unique names
		numServices := 100
		for i := 0; i < numServices; i++ {
			serviceName := fmt.Sprintf("service-%d", i)
			registry.Register(serviceName, func(r *api.Router) {
				mu.Lock()
				callCount++
				mu.Unlock()
			})
		}

		registry.LoadAll(router)

		// All services should be called
		assert.Equal(numServices, callCount)
	})
}

func TestConvenienceFunctions(t *testing.T) {
	assert := assert2.New(t)

	t.Run("Register convenience function", func(t *testing.T) {
		// Save and restore the default registry
		oldRegistry := DefaultRegistry
		defer func() { DefaultRegistry = oldRegistry }()

		// Create a new registry for testing
		DefaultRegistry = NewRegistry()

		called := false
		Register("test-service", func(router *api.Router) {
			called = true
		})

		fn, exists := DefaultRegistry.Get("test-service")
		assert.True(exists)
		assert.NotNil(fn)

		fn(nil)
		assert.True(called)
	})

	t.Run("LoadAll convenience function", func(t *testing.T) {
		// Save and restore the default registry
		oldRegistry := DefaultRegistry
		defer func() { DefaultRegistry = oldRegistry }()

		// Create a new registry for testing
		DefaultRegistry = NewRegistry()
		router := api.NewRouter()

		callCount := 0
		mu := &sync.Mutex{}
		DefaultRegistry.Register("test", func(r *api.Router) {
			mu.Lock()
			callCount++
			mu.Unlock()
		})

		LoadAll(router)

		assert.Equal(1, callCount)
	})
}

func TestRegistry_Concurrency(t *testing.T) {
	assert := assert2.New(t)

	t.Run("concurrent register and get", func(t *testing.T) {
		registry := NewRegistry()

		done := make(chan bool)

		// Register services concurrently
		for i := 0; i < 10; i++ {
			go func(n int) {
				serviceName := "service-" + string(rune('0'+n))
				registry.Register(serviceName, func(router *api.Router) {})
				done <- true
			}(i)
		}

		// Wait for all registrations
		for i := 0; i < 10; i++ {
			<-done
		}

		// Get services concurrently
		for i := 0; i < 10; i++ {
			go func(n int) {
				serviceName := "service-" + string(rune('0'+n))
				_, exists := registry.Get(serviceName)
				assert.True(exists)
				done <- true
			}(i)
		}

		// Wait for all gets
		for i := 0; i < 10; i++ {
			<-done
		}

		names := registry.List()
		assert.Equal(10, len(names))
	})
}

func TestGetLoadConcurrency(t *testing.T) {
	assert := assert2.New(t)

	t.Run("returns default when env not set", func(t *testing.T) {
		t.Setenv("LOAD_CONCURRENCY", "")
		result := getLoadConcurrency()
		assert.Equal(DefaultLoadConcurrency, result)
	})

	t.Run("returns env value when valid", func(t *testing.T) {
		t.Setenv("LOAD_CONCURRENCY", "8")
		result := getLoadConcurrency()
		assert.Equal(8, result)
	})

	t.Run("returns default when env is invalid", func(t *testing.T) {
		t.Setenv("LOAD_CONCURRENCY", "invalid")
		result := getLoadConcurrency()
		assert.Equal(DefaultLoadConcurrency, result)
	})

	t.Run("returns default when env is zero", func(t *testing.T) {
		t.Setenv("LOAD_CONCURRENCY", "0")
		result := getLoadConcurrency()
		assert.Equal(DefaultLoadConcurrency, result)
	})

	t.Run("returns default when env is negative", func(t *testing.T) {
		t.Setenv("LOAD_CONCURRENCY", "-1")
		result := getLoadConcurrency()
		assert.Equal(DefaultLoadConcurrency, result)
	})
}

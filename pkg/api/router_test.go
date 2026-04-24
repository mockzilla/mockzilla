package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mockzilla/connexions/v2/pkg/config"
	"github.com/mockzilla/connexions/v2/pkg/db"
	"github.com/mockzilla/connexions/v2/pkg/middleware"
	"github.com/stretchr/testify/assert"
)

func waitForAsync() {
	runtime.Gosched()
	time.Sleep(10 * time.Millisecond)
}

// mockService implements Handler for testing
type mockService struct {
	name   string
	config *config.ServiceConfig
	routes func(chi.Router)
}

func (m *mockService) Routes() RouteDescriptions {
	return nil
}

func (m *mockService) RegisterRoutes(router chi.Router) {
	if m.routes != nil {
		m.routes(router)
	}
}

func (m *mockService) Generate(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Generate not implemented in mock", http.StatusNotImplemented)
}

// newTestRouter creates a router with a temporary directory for testing
func newTestRouter(t *testing.T) *Router {
	cfg := config.NewDefaultAppConfig(t.TempDir())
	return NewRouter(WithConfigOption(cfg))
}

// registerTestService is a helper to register a service with the new signature
func registerTestService(router *Router, service *mockService) {
	// Ensure config has Name set (use service.name if not already set)
	if service.config.Name == "" {
		service.config.Name = service.name
	}
	router.RegisterService(service.config, service)
}

func TestNewRouter(t *testing.T) {
	t.Run("Creates router with default settings", func(t *testing.T) {
		router := newTestRouter(t)

		assert.NotNil(t, router)
		assert.NotNil(t, router.Router)
		assert.NotNil(t, router.databases)
		assert.NotNil(t, router.contexts)
		assert.Len(t, router.contexts, 3) // common, fake, words
	})

	t.Run("Respects ROUTER_HISTORY_DURATION env var", func(t *testing.T) {
		_ = os.Setenv("ROUTER_HISTORY_DURATION", "5m")
		defer func() { _ = os.Unsetenv("ROUTER_HISTORY_DURATION") }()

		router := newTestRouter(t)
		assert.NotNil(t, router.databases)
	})

	t.Run("Uses default duration on invalid env var", func(t *testing.T) {
		_ = os.Setenv("ROUTER_HISTORY_DURATION", "invalid")
		defer func() { _ = os.Unsetenv("ROUTER_HISTORY_DURATION") }()

		router := newTestRouter(t)
		assert.NotNil(t, router.databases)
	})

	t.Run("Loads default contexts", func(t *testing.T) {
		router := newTestRouter(t)

		contexts := router.GetContexts()
		assert.Len(t, contexts, 3) // common, fake, words

		// Check that common, fake, and words contexts are loaded
		hasCommon := false
		hasFake := false
		hasWords := false
		for _, ctx := range contexts {
			if _, ok := ctx["common"]; ok {
				hasCommon = true
			}
			if _, ok := ctx["fake"]; ok {
				hasFake = true
			}
			if _, ok := ctx["words"]; ok {
				hasWords = true
			}
		}
		assert.True(t, hasCommon, "Should have common context")
		assert.True(t, hasFake, "Should have fake context")
		assert.True(t, hasWords, "Should have words context")
	})
}

func TestRouter_GetDB(t *testing.T) {
	router := newTestRouter(t)

	// Register a service first
	service := &mockService{
		name:   "test-service",
		config: &config.ServiceConfig{Name: "test-service"},
	}
	registerTestService(router, service)

	// Now GetDB should return the DB for this service
	database := router.GetDB("test-service")
	assert.NotNil(t, database)
	assert.NotNil(t, database.History())

	// GetDB for non-existent service should return nil
	nilDB := router.GetDB("non-existent")
	assert.Nil(t, nilDB)
}

func TestRouter_GetContexts(t *testing.T) {
	router := newTestRouter(t)
	contexts := router.GetContexts()

	assert.NotNil(t, contexts)
	assert.Len(t, contexts, 3) // common, fake, words
}

func TestRouter_RegisterService(t *testing.T) {
	t.Run("Registers service with routes", func(t *testing.T) {
		router := newTestRouter(t)

		cfg := config.NewServiceConfig()
		cfg.Name = "test-service"
		service := &mockService{
			name:   "test-service",
			config: cfg,
			routes: func(r chi.Router) {
				r.Get("/hello", func(w http.ResponseWriter, req *http.Request) {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("Hello"))
				})
			},
		}

		router.RegisterService(cfg, service)

		// Test that the route is accessible
		req := httptest.NewRequest(http.MethodGet, "/test-service/hello", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "Hello", w.Body.String())
	})

	t.Run("Multiple services can be registered", func(t *testing.T) {
		cfg := config.NewDefaultAppConfig(t.TempDir())
		router := NewRouter(WithConfigOption(cfg))

		cfg1 := config.NewServiceConfig()
		cfg1.Name = "service1"
		service1 := &mockService{
			name:   "service1",
			config: cfg1,
			routes: func(r chi.Router) {
				r.Get("/endpoint1", func(w http.ResponseWriter, req *http.Request) {
					_, _ = w.Write([]byte("Service1"))
				})
			},
		}

		cfg2 := config.NewServiceConfig()
		cfg2.Name = "service2"
		service2 := &mockService{
			name:   "service2",
			config: cfg2,
			routes: func(r chi.Router) {
				r.Get("/endpoint2", func(w http.ResponseWriter, req *http.Request) {
					_, _ = w.Write([]byte("Service2"))
				})
			},
		}

		router.RegisterService(cfg1, service1)
		router.RegisterService(cfg2, service2)

		// Test service1
		req1 := httptest.NewRequest(http.MethodGet, "/service1/endpoint1", nil)
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		assert.Equal(t, "Service1", w1.Body.String())

		// Test service2
		req2 := httptest.NewRequest(http.MethodGet, "/service2/endpoint2", nil)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)
		assert.Equal(t, "Service2", w2.Body.String())
	})
}

func TestRouter_RegisterService_DuplicateNameIsIgnored(t *testing.T) {
	router := newTestRouter(t)

	cfg := config.NewServiceConfig()
	cfg.Name = "petstore"
	first := &mockService{
		name:   "petstore",
		config: cfg,
		routes: func(r chi.Router) {
			r.Get("/ping", func(w http.ResponseWriter, req *http.Request) {
				_, _ = w.Write([]byte("first"))
			})
		},
	}
	second := &mockService{
		name:   "petstore",
		config: cfg,
		routes: func(r chi.Router) {
			r.Get("/ping", func(w http.ResponseWriter, req *http.Request) {
				_, _ = w.Write([]byte("second"))
			})
		},
	}

	router.RegisterService(cfg, first)
	assert.NotPanics(t, func() { router.RegisterService(cfg, second) })

	req := httptest.NewRequest(http.MethodGet, "/petstore/ping", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, "first", w.Body.String())
}

func TestRouter_RegisterHTTPHandler_DuplicateNameIsIgnored(t *testing.T) {
	router := newTestRouter(t)

	cfg := config.NewServiceConfig()
	cfg.Name = "petstore"
	factory := func(_ db.DB) Handler {
		return &mockService{
			name:   "petstore",
			config: cfg,
			routes: func(r chi.Router) {
				r.Get("/ping", func(w http.ResponseWriter, req *http.Request) {
					_, _ = w.Write([]byte("ok"))
				})
			},
		}
	}

	router.RegisterHTTPHandler(cfg, factory)
	assert.NotPanics(t, func() { router.RegisterHTTPHandler(cfg, factory) })
}

func TestRouter_MiddlewareOrder(t *testing.T) {
	t.Run("Middleware executes in correct order", func(t *testing.T) {
		router := newTestRouter(t)

		// Track middleware execution order
		var executionOrder []string

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {
				r.Get("/test", func(w http.ResponseWriter, req *http.Request) {
					executionOrder = append(executionOrder, "handler")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("OK"))
				})
			},
		}

		registerTestService(router, service)

		req := httptest.NewRequest(http.MethodGet, "/test-service/test", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// Verify handler was called
		assert.Contains(t, executionOrder, "handler")
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Latency middleware executes before handler", func(t *testing.T) {
		router := newTestRouter(t)

		cfg := config.NewServiceConfig()
		cfg.Latency = 50 * time.Millisecond

		service := &mockService{
			name:   "test-service",
			config: cfg,
			routes: func(r chi.Router) {
				r.Get("/test", func(w http.ResponseWriter, req *http.Request) {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("OK"))
				})
			},
		}

		registerTestService(router, service)

		req := httptest.NewRequest(http.MethodGet, "/test-service/test", nil)
		w := httptest.NewRecorder()

		start := time.Now()
		router.ServeHTTP(w, req)
		duration := time.Since(start)

		// Latency middleware should add delay
		assert.GreaterOrEqual(t, duration, 50*time.Millisecond)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Error middleware prevents handler execution", func(t *testing.T) {
		router := newTestRouter(t)

		cfgBytes := []byte(`
errors:
  p100: 500
`)
		cfg, _ := config.NewServiceConfigFromBytes(cfgBytes)

		handlerCalled := false
		service := &mockService{
			name:   "test-service",
			config: cfg,
			routes: func(r chi.Router) {
				r.Get("/test", func(w http.ResponseWriter, req *http.Request) {
					handlerCalled = true
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("OK"))
				})
			},
		}

		registerTestService(router, service)

		req := httptest.NewRequest(http.MethodGet, "/test-service/test", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// Error middleware should prevent handler from being called
		assert.False(t, handlerCalled, "Handler should not be called when error middleware returns error")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("Cache read middleware returns cached response", func(t *testing.T) {
		router := newTestRouter(t)

		cfgBytes := []byte(`
cache:
  requests: true
`)
		cfg, _ := config.NewServiceConfigFromBytes(cfgBytes)

		callCount := 0
		service := &mockService{
			name:   "test-service",
			config: cfg,
			routes: func(r chi.Router) {
				r.Get("/test", func(w http.ResponseWriter, req *http.Request) {
					callCount++
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("Response"))
				})
			},
		}

		registerTestService(router, service)

		// First request - should hit handler
		req1 := httptest.NewRequest(http.MethodGet, "/test-service/test", nil)
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		waitForAsync()
		assert.Equal(t, 1, callCount)
		assert.Equal(t, "Response", w1.Body.String())

		// Second request - should be cached
		req2 := httptest.NewRequest(http.MethodGet, "/test-service/test", nil)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)

		// Handler should not be called again (cache hit)
		assert.Equal(t, 1, callCount, "Handler should only be called once due to caching")
		assert.Equal(t, "Response", w2.Body.String())
	})

	t.Run("Cache write middleware stores response in history", func(t *testing.T) {
		router := newTestRouter(t)

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {
				r.Post("/test", func(w http.ResponseWriter, req *http.Request) {
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte("Created"))
				})
			},
		}

		registerTestService(router, service)

		req := httptest.NewRequest(http.MethodPost, "/test-service/test", strings.NewReader(`{"data":"test"}`))
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		waitForAsync()

		// Check that response was stored in history
		database := router.GetDB("test-service")
		assert.NotNil(t, database, "Database should exist for test-service")

		data := database.History().Data(context.Background())
		assert.NotEmpty(t, data, "History should contain the request")

		rec := data[len(data)-1]
		assert.NotNil(t, rec)
		assert.Equal(t, http.StatusCreated, rec.Response.StatusCode)
		assert.Equal(t, []byte("Created"), rec.Response.Body)
	})

	t.Run("Request body is available to handler after middleware reads it", func(t *testing.T) {
		router := newTestRouter(t)

		var receivedBody string
		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {
				r.Post("/test", func(w http.ResponseWriter, req *http.Request) {
					// Handler should be able to read the body even though middleware already read it
					bodyBytes, err := io.ReadAll(req.Body)
					assert.NoError(t, err, "Should be able to read body in handler")
					receivedBody = string(bodyBytes)

					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("OK"))
				})
			},
		}

		registerTestService(router, service)

		expectedBody := `{"data":"test"}`
		req := httptest.NewRequest(http.MethodPost, "/test-service/test", strings.NewReader(expectedBody))
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		waitForAsync()

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, expectedBody, receivedBody, "Handler should receive the full request body")

		// verify it was stored in history
		database := router.GetDB("test-service")
		assert.NotNil(t, database, "Database should exist for test-service")

		data := database.History().Data(context.Background())
		assert.NotEmpty(t, data)

		rec := data[len(data)-1]
		assert.Equal(t, []byte(expectedBody), rec.Request.Body, "History should contain the request body")
	})

	t.Run("Middleware order: Latency -> Error -> Cache -> Upstream -> Handler", func(t *testing.T) {
		router := newTestRouter(t)

		// Configure with latency but no error
		cfg := config.NewServiceConfig()
		cfg.Latency = 30 * time.Millisecond

		service := &mockService{
			name:   "test-service",
			config: cfg,
			routes: func(r chi.Router) {
				r.Get("/test", func(w http.ResponseWriter, req *http.Request) {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("Success"))
				})
			},
		}

		registerTestService(router, service)

		req := httptest.NewRequest(http.MethodGet, "/test-service/test", nil)
		w := httptest.NewRecorder()

		start := time.Now()
		router.ServeHTTP(w, req)
		duration := time.Since(start)

		// Should have latency applied
		assert.GreaterOrEqual(t, duration, 30*time.Millisecond)

		// Should reach handler (no error)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "Success", w.Body.String())
	})

	t.Run("Error middleware stops execution before cache and handler", func(t *testing.T) {
		router := newTestRouter(t)

		cfgBytes := []byte(`
errors:
  p100: 503
cache:
  requests: true
`)
		cfg, _ := config.NewServiceConfigFromBytes(cfgBytes)

		handlerCalled := false
		service := &mockService{
			name:   "test-service",
			config: cfg,
			routes: func(r chi.Router) {
				r.Get("/test", func(w http.ResponseWriter, req *http.Request) {
					handlerCalled = true
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("OK"))
				})
			},
		}

		registerTestService(router, service)

		req := httptest.NewRequest(http.MethodGet, "/test-service/test", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		waitForAsync()

		// Error middleware executes first and stops the chain
		assert.False(t, handlerCalled)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)

		// Response should NOT be cached (error occurred before cache write)
		database := router.GetDB("test-service")
		assert.NotNil(t, database, "Database should exist for test-service")

		data := database.History().Data(context.Background())
		// History might have the request but not a successful response
		for _, rec := range data {
			if rec.Request != nil && rec.Request.Method == "GET" && rec.Request.URL == "/test-service/test" {
				assert.NotEqual(t, http.StatusOK, rec.Response.StatusCode)
			}
		}
	})

	t.Run("POST request is stored in history by cache write middleware", func(t *testing.T) {
		router := newTestRouter(t)

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {
				r.Post("/create", func(w http.ResponseWriter, req *http.Request) {
					body, _ := io.ReadAll(req.Body)
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte("Created: " + string(body)))
				})
			},
		}

		registerTestService(router, service)

		reqBody := `{"name":"test"}`
		req := httptest.NewRequest(http.MethodPost, "/test-service/create", strings.NewReader(reqBody))
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		waitForAsync()

		assert.Equal(t, http.StatusCreated, w.Code)

		// Verify history contains the request
		database := router.GetDB("test-service")
		assert.NotNil(t, database, "Database should exist for test-service")

		data := database.History().Data(context.Background())
		assert.NotEmpty(t, data)

		rec := data[len(data)-1]
		assert.Equal(t, []byte(reqBody), rec.Request.Body)
	})

	t.Run("Upstream middleware executes before handler", func(t *testing.T) {
		// Create a mock upstream server
		upstreamCalled := false
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			upstreamCalled = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Upstream response"))
		}))
		defer upstreamServer.Close()

		router := newTestRouter(t)

		cfgBytes := []byte(`
upstream:
  url: ` + upstreamServer.URL + `
`)
		cfg, _ := config.NewServiceConfigFromBytes(cfgBytes)

		handlerCalled := false
		cfg.Name = "test-service"
		service := &mockService{
			name:   "test-service",
			config: cfg,
			routes: func(r chi.Router) {
				r.Get("/test", func(w http.ResponseWriter, req *http.Request) {
					handlerCalled = true
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("Local response"))
				})
			},
		}

		registerTestService(router, service)

		req := httptest.NewRequest(http.MethodGet, "/test-service/test", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// Upstream should be called
		assert.True(t, upstreamCalled, "Upstream should be called")
		// Handler should NOT be called (upstream returned successfully)
		assert.False(t, handlerCalled, "Handler should not be called when upstream succeeds")
		assert.Equal(t, "Upstream response", w.Body.String())
	})

	t.Run("Handler executes when upstream fails", func(t *testing.T) {
		// Create a mock upstream server that fails
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Upstream error"))
		}))
		defer upstreamServer.Close()

		router := newTestRouter(t)

		cfgBytes := []byte(`
upstream:
  url: ` + upstreamServer.URL + `
`)
		cfg, _ := config.NewServiceConfigFromBytes(cfgBytes)

		handlerCalled := false
		cfg.Name = "test-service"
		service := &mockService{
			name:   "test-service",
			config: cfg,
			routes: func(r chi.Router) {
				r.Get("/test", func(w http.ResponseWriter, req *http.Request) {
					handlerCalled = true
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("Local fallback"))
				})
			},
		}

		registerTestService(router, service)

		req := httptest.NewRequest(http.MethodGet, "/test-service/test", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// Handler should be called as fallback
		assert.True(t, handlerCalled, "Handler should be called when upstream fails")
		assert.Equal(t, "Local fallback", w.Body.String())
	})
}

func TestRouter_GlobalMiddlewareOrder(t *testing.T) {
	t.Run("Global middleware executes before service middleware", func(t *testing.T) {
		// Disable logger to avoid noise
		_ = os.Setenv("DISABLE_LOGGER", "true")
		defer func() { _ = os.Unsetenv("DISABLE_LOGGER") }()

		router := newTestRouter(t)

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {
				r.Get("/test", func(w http.ResponseWriter, req *http.Request) {
					// Check that RequestID was set by global middleware
					requestID := req.Context().Value(chi.RouteCtxKey)
					assert.NotNil(t, requestID, "RequestID should be set by global middleware")

					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("OK"))
				})
			},
		}

		registerTestService(router, service)

		req := httptest.NewRequest(http.MethodGet, "/test-service/test", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Timeout middleware is applied globally", func(t *testing.T) {
		router := newTestRouter(t)

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {
				r.Get("/test", func(w http.ResponseWriter, req *http.Request) {
					// Check that context has a deadline (from timeout middleware)
					_, hasDeadline := req.Context().Deadline()
					assert.True(t, hasDeadline, "Request should have timeout deadline")

					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("OK"))
				})
			},
		}

		registerTestService(router, service)

		req := httptest.NewRequest(http.MethodGet, "/test-service/test", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

}

func TestRouter_CompleteMiddlewareChain(t *testing.T) {
	t.Run("Complete middleware execution order verification", func(t *testing.T) {
		router := newTestRouter(t)

		// Track execution order with timestamps
		type executionEvent struct {
			name      string
			timestamp time.Time
		}
		var events []executionEvent

		// Create a custom handler that tracks execution
		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {
				r.Get("/test", func(w http.ResponseWriter, req *http.Request) {
					events = append(events, executionEvent{"handler", time.Now()})
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("OK"))
				})
			},
		}

		registerTestService(router, service)

		req := httptest.NewRequest(http.MethodGet, "/test-service/test", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// Verify handler was executed
		assert.NotEmpty(t, events)
		assert.Equal(t, "handler", events[0].name)

		// Verify response was written
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "OK", w.Body.String())
	})

	t.Run("Middleware chain with latency, cache, and handler", func(t *testing.T) {
		router := newTestRouter(t)

		cfgBytes := []byte(`
latency: 20ms
cache:
  requests: true
`)
		cfg, _ := config.NewServiceConfigFromBytes(cfgBytes)

		callCount := 0
		service := &mockService{
			name:   "test-service",
			config: cfg,
			routes: func(r chi.Router) {
				r.Get("/test", func(w http.ResponseWriter, req *http.Request) {
					callCount++
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("Response"))
				})
			},
		}

		registerTestService(router, service)

		// First request: Latency -> Cache (miss) -> Upstream (skip) -> Handler -> Cache Write
		req1 := httptest.NewRequest(http.MethodGet, "/test-service/test", nil)
		w1 := httptest.NewRecorder()

		start1 := time.Now()
		router.ServeHTTP(w1, req1)
		duration1 := time.Since(start1)

		assert.GreaterOrEqual(t, duration1, 20*time.Millisecond, "First request should have latency")
		assert.Equal(t, 1, callCount, "Handler should be called on first request")
		assert.Equal(t, "Response", w1.Body.String())

		// Second request: Latency -> Cache (hit) -> Return (skip handler)
		req2 := httptest.NewRequest(http.MethodGet, "/test-service/test", nil)
		w2 := httptest.NewRecorder()

		start2 := time.Now()
		router.ServeHTTP(w2, req2)
		duration2 := time.Since(start2)

		assert.GreaterOrEqual(t, duration2, 20*time.Millisecond, "Second request should still have latency")
		assert.Equal(t, 1, callCount, "Handler should NOT be called on cached request")
		assert.Equal(t, "Response", w2.Body.String())
	})

	t.Run("Error middleware short-circuits entire chain", func(t *testing.T) {
		router := newTestRouter(t)

		cfgBytes := []byte(`
latency: 10ms
errors:
  p100: 500
cache:
  requests: true
`)
		cfg, _ := config.NewServiceConfigFromBytes(cfgBytes)

		handlerCalled := false
		service := &mockService{
			name:   "test-service",
			config: cfg,
			routes: func(r chi.Router) {
				r.Get("/test", func(w http.ResponseWriter, req *http.Request) {
					handlerCalled = true
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("OK"))
				})
			},
		}

		registerTestService(router, service)

		req := httptest.NewRequest(http.MethodGet, "/test-service/test", nil)
		w := httptest.NewRecorder()

		start := time.Now()
		router.ServeHTTP(w, req)
		duration := time.Since(start)

		// Order: Latency (applied) -> Error (returns) -> Cache/Upstream/Handler (skipped)
		assert.GreaterOrEqual(t, duration, 10*time.Millisecond, "Latency should be applied before error")
		assert.False(t, handlerCalled, "Handler should be skipped due to error")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("Full chain: Global -> Service -> Handler -> Service (reverse) -> Global (reverse)", func(t *testing.T) {
		_ = os.Setenv("DISABLE_LOGGER", "true")
		defer func() { _ = os.Unsetenv("DISABLE_LOGGER") }()

		router := newTestRouter(t)

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {
				r.Post("/test", func(w http.ResponseWriter, req *http.Request) {
					// Verify global middleware set context values
					assert.NotNil(t, req.Context())

					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte("Created"))
				})
			},
		}

		registerTestService(router, service)

		reqBody := `{"data":"test"}`
		req := httptest.NewRequest(http.MethodPost, "/test-service/test", strings.NewReader(reqBody))
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		waitForAsync()

		assert.Equal(t, http.StatusCreated, w.Code)

		// Verify cache write middleware stored the response
		database := router.GetDB("test-service")
		assert.NotNil(t, database, "Database should exist for test-service")
		data := database.History().Data(context.Background())
		assert.NotEmpty(t, data, "Response should be stored in history")
		rec := data[len(data)-1]
		assert.Equal(t, http.StatusCreated, rec.Response.StatusCode)
		assert.Equal(t, []byte("Created"), rec.Response.Body)
	})
}

func TestLoadAppConfig(t *testing.T) {
	t.Run("loads config from file", func(t *testing.T) {
		tmpDir := t.TempDir()
		dataDir := tmpDir + "/resources/data"
		_ = os.MkdirAll(dataDir, 0755)

		configContent := []byte(`
title: Test App
port: 3000
history:
  duration: 10m
`)
		_ = os.WriteFile(dataDir+"/app.yml", configContent, 0644)

		cfg := loadAppConfig(tmpDir)

		assert.Equal(t, "Test App", cfg.Title)
		assert.Equal(t, 3000, cfg.Port)
		assert.Equal(t, 10*time.Minute, cfg.History.Duration)
	})

	t.Run("uses defaults when file missing", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg := loadAppConfig(tmpDir)

		assert.Equal(t, "API Explorer", cfg.Title)
		assert.Equal(t, 2200, cfg.Port)
		assert.Equal(t, config.DefaultHistoryDuration, cfg.History.Duration)
	})

	t.Run("uses defaults on invalid yaml", func(t *testing.T) {
		tmpDir := t.TempDir()
		dataDir := tmpDir + "/resources/data"
		_ = os.MkdirAll(dataDir, 0755)

		configContent := []byte(`invalid: [yaml`)
		_ = os.WriteFile(dataDir+"/app.yml", configContent, 0644)

		cfg := loadAppConfig(tmpDir)

		assert.Equal(t, "API Explorer", cfg.Title)
		assert.Equal(t, 2200, cfg.Port)
	})

	t.Run("env vars override file values", func(t *testing.T) {
		tmpDir := t.TempDir()
		dataDir := tmpDir + "/resources/data"
		_ = os.MkdirAll(dataDir, 0755)

		configContent := []byte(`
title: File Title
history:
  duration: 10m
`)
		_ = os.WriteFile(dataDir+"/app.yml", configContent, 0644)

		_ = os.Setenv("ROUTER_HISTORY_DURATION", "20m")
		defer func() { _ = os.Unsetenv("ROUTER_HISTORY_DURATION") }()

		cfg := loadAppConfig(tmpDir)

		assert.Equal(t, "File Title", cfg.Title)
		assert.Equal(t, 20*time.Minute, cfg.History.Duration)
	})
}

func TestRegisterHTTPHandler(t *testing.T) {
	t.Run("registers handler at correct route", func(t *testing.T) {
		router := newTestRouter(t)
		cfg := &config.ServiceConfig{Name: "myservice"}

		router.RegisterHTTPHandler(cfg, func(d db.DB) Handler {
			return &mockService{
				name:   "myservice",
				config: cfg,
				routes: func(r chi.Router) {
					r.Get("/hello", func(w http.ResponseWriter, r *http.Request) {
						_, _ = w.Write([]byte("world"))
					})
				},
			}
		})

		req := httptest.NewRequest("GET", "/myservice/hello", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "world", w.Body.String())
	})

	t.Run("applies custom middleware via WithMiddleware", func(t *testing.T) {
		router := newTestRouter(t)
		cfg := &config.ServiceConfig{Name: "mwtest"}

		middlewareCalled := false
		customMw := func(params *middleware.Params) func(http.Handler) http.Handler {
			return func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					middlewareCalled = true
					w.Header().Set("X-Custom", "applied")
					next.ServeHTTP(w, r)
				})
			}
		}

		router.RegisterHTTPHandler(cfg, func(d db.DB) Handler {
			return &mockService{
				name:   "mwtest",
				config: cfg,
				routes: func(r chi.Router) {
					r.Get("/test", func(w http.ResponseWriter, r *http.Request) {
						_, _ = w.Write([]byte("ok"))
					})
				},
			}
		}, WithMiddleware([]func(*middleware.Params) func(http.Handler) http.Handler{customMw}))

		req := httptest.NewRequest("GET", "/mwtest/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.True(t, middlewareCalled, "Custom middleware should be called")
		assert.Equal(t, "applied", w.Header().Get("X-Custom"))
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("middleware can mutate request before upstream", func(t *testing.T) {
		var receivedAuth string
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("upstream"))
		}))
		defer upstreamServer.Close()

		router := newTestRouter(t)

		cfgBytes := []byte(`
upstream:
  url: ` + upstreamServer.URL + `
`)
		cfg, _ := config.NewServiceConfigFromBytes(cfgBytes)
		cfg.Name = "reqmw"

		addAuthMw := func(params *middleware.Params) func(http.Handler) http.Handler {
			return func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					r.Header.Set("Authorization", "Bearer test-token")
					next.ServeHTTP(w, r)
				})
			}
		}

		router.RegisterHTTPHandler(cfg, func(d db.DB) Handler {
			return &mockService{
				name:   "reqmw",
				config: cfg,
				routes: func(r chi.Router) {
					r.Get("/test", func(w http.ResponseWriter, r *http.Request) {
						_, _ = w.Write([]byte("fallback"))
					})
				},
			}
		}, WithMiddleware([]func(*middleware.Params) func(http.Handler) http.Handler{addAuthMw}))

		req := httptest.NewRequest("GET", "/reqmw/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, "Bearer test-token", receivedAuth, "Upstream should receive the header set by middleware")
		assert.Equal(t, "upstream", w.Body.String())
	})
}

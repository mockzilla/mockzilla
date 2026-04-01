package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mockzilla/connexions/v2/pkg/config"
	"github.com/stretchr/testify/assert"
	"go.yaml.in/yaml/v4"
)

func TestCreateServiceConfigRoutes(t *testing.T) {
	t.Run("Creates config routes when UI is enabled", func(t *testing.T) {
		router := newTestRouter(t)

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)

		err := CreateServiceConfigRoutes(router)
		assert.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/.config?service=test-service", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Does not create routes when UI is disabled", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.DisableUI = true

		err := CreateServiceConfigRoutes(router)
		assert.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/.config?service=test-service", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestServiceConfigHandler_get(t *testing.T) {
	t.Run("Returns config as YAML", func(t *testing.T) {
		router := newTestRouter(t)

		svcCfg := config.NewServiceConfig()
		svcCfg.Latency = 100 * time.Millisecond

		service := &mockService{
			name:   "test-service",
			config: svcCfg,
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)
		_ = CreateServiceConfigRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.config?service=test-service", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "text/yaml; charset=utf-8", w.Header().Get("Content-Type"))

		// Verify it's valid YAML that round-trips back to a ServiceConfig
		var got config.ServiceConfig
		err := yaml.Unmarshal(w.Body.Bytes(), &got)
		assert.NoError(t, err)
		assert.Equal(t, "test-service", got.Name)
		assert.Equal(t, 100*time.Millisecond, got.Latency)
	})

	t.Run("Returns 404 for unknown service", func(t *testing.T) {
		router := newTestRouter(t)
		_ = CreateServiceConfigRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.config?service=nonexistent", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "Service not found")
	})

	t.Run("Returns default config when service has nil config", func(t *testing.T) {
		router := newTestRouter(t)

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)

		// Set the config to nil after registration
		router.mu.Lock()
		router.services["test-service"].Config = nil
		router.mu.Unlock()

		_ = CreateServiceConfigRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.config?service=test-service", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var got config.ServiceConfig
		err := yaml.Unmarshal(w.Body.Bytes(), &got)
		assert.NoError(t, err)
	})

	t.Run("Returns config for root service via .root name", func(t *testing.T) {
		router := newTestRouter(t)

		svcCfg := config.NewServiceConfig()
		svcCfg.Name = ""

		service := &mockService{
			name:   "",
			config: svcCfg,
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)
		_ = CreateServiceConfigRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.config?service="+RootServiceName, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Returns config for service with slash in name", func(t *testing.T) {
		router := newTestRouter(t)

		svcCfg := config.NewServiceConfig()

		service := &mockService{
			name:   "adyen/v71",
			config: svcCfg,
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)
		_ = CreateServiceConfigRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.config?service=adyen/v71", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "text/yaml; charset=utf-8", w.Header().Get("Content-Type"))

		var got config.ServiceConfig
		err := yaml.Unmarshal(w.Body.Bytes(), &got)
		assert.NoError(t, err)
		assert.Equal(t, "adyen/v71", got.Name)
	})
}

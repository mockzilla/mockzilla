package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/db"
	"github.com/stretchr/testify/assert"
)

func TestCreateHistoryRoutes(t *testing.T) {
	t.Run("Creates history routes when UI is enabled", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.History.URL = "/.history"

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)

		err := CreateHistoryRoutes(router)
		assert.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/.history?service=test-service", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Does not create routes when UI is disabled", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.DisableUI = true
		router.config.History.URL = "/.history"

		err := CreateHistoryRoutes(router)
		assert.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/.history?service=test-service", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("Does not create routes when History URL is empty", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.History.URL = ""

		err := CreateHistoryRoutes(router)
		assert.NoError(t, err)
	})
}

func TestHistoryHandler_list(t *testing.T) {
	t.Run("Returns empty list when no history entries", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.History.URL = "/.history"

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)
		_ = CreateHistoryRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.history?service=test-service", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response HistorySummaryListResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Empty(t, response.Items)
	})

	t.Run("Returns history summaries without bodies", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.History.URL = "/.history"

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)

		database := router.GetDB("test-service")
		database.History().Set(context.Background(), "/users", &db.HistoryRequest{
			Method: "GET",
			URL:    "/test-service/users",
			Body:   []byte(`{"query":"all"}`),
		}, &db.HistoryResponse{
			StatusCode: 200,
			Body:       []byte(`{"ok":true}`),
		})

		_ = CreateHistoryRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.history?service=test-service", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response HistorySummaryListResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Len(t, response.Items, 1)
		assert.Equal(t, "GET", response.Items[0].Request.Method)
		assert.Equal(t, 200, response.Items[0].Response.StatusCode)
		// Bodies should be omitted in list
		assert.Nil(t, response.Items[0].Request.Body)
		assert.Nil(t, response.Items[0].Response.Body)
	})

	t.Run("Returns 404 for unknown service", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.History.URL = "/.history"
		_ = CreateHistoryRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.history?service=nonexistent", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "Service not found")
	})
}

func TestHistoryHandler_getByID(t *testing.T) {
	t.Run("Returns single entry by ID with full bodies", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.History.URL = "/.history"

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)

		database := router.GetDB("test-service")
		entry := database.History().Set(context.Background(), "/users", &db.HistoryRequest{
			Method: "POST",
			URL:    "/test-service/users",
			Body:   []byte(`{"name":"test"}`),
		}, &db.HistoryResponse{
			StatusCode: 201,
			Body:       []byte(`{"id":1}`),
		})

		_ = CreateHistoryRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.history?service=test-service&id="+entry.ID, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var got db.HistoryEntry
		err := json.Unmarshal(w.Body.Bytes(), &got)
		assert.NoError(t, err)
		assert.Equal(t, entry.ID, got.ID)
		assert.Equal(t, "POST", got.Request.Method)
		assert.Equal(t, 201, got.Response.StatusCode)
		// Full entry should include bodies
		assert.NotNil(t, got.Request.Body)
		assert.NotNil(t, got.Response.Body)
	})

	t.Run("Returns 404 for unknown entry ID", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.History.URL = "/.history"

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)
		_ = CreateHistoryRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.history?service=test-service&id=nonexistent-id", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "Entry not found")
	})
}

func TestHistoryHandler_clear(t *testing.T) {
	t.Run("Clears history entries", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.History.URL = "/.history"

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)

		database := router.GetDB("test-service")
		database.History().Set(context.Background(), "/users", &db.HistoryRequest{
			Method: "GET",
			URL:    "/test-service/users",
		}, &db.HistoryResponse{
			StatusCode: 200,
		})

		_ = CreateHistoryRoutes(router)

		// Verify entry exists
		assert.Equal(t, 1, database.History().Len(context.Background()))

		// Clear
		req := httptest.NewRequest(http.MethodDelete, "/.history?service=test-service", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response HistorySummaryListResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Empty(t, response.Items)

		// Verify cleared
		assert.Equal(t, 0, database.History().Len(context.Background()))
	})

	t.Run("Returns 404 for unknown service", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.History.URL = "/.history"
		_ = CreateHistoryRoutes(router)

		req := httptest.NewRequest(http.MethodDelete, "/.history?service=nonexistent", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestHistoryHandler_slashInServiceName(t *testing.T) {
	t.Run("Returns history for service with slash in name", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.History.URL = "/.history"

		service := &mockService{
			name:   "adyen/v71",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)
		_ = CreateHistoryRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.history?service=adyen/v71", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response HistorySummaryListResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Empty(t, response.Items)
	})

	t.Run("Returns single entry by ID for service with slash in name", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.History.URL = "/.history"

		service := &mockService{
			name:   "adyen/v71",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)

		database := router.GetDB("adyen/v71")
		entry := database.History().Set(context.Background(), "/payments", &db.HistoryRequest{
			Method: "POST",
			URL:    "/adyen/v71/payments",
		}, &db.HistoryResponse{
			StatusCode: 200,
			Body:       []byte(`{"ok":true}`),
		})

		_ = CreateHistoryRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.history?service=adyen/v71&id="+entry.ID, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var got db.HistoryEntry
		err := json.Unmarshal(w.Body.Bytes(), &got)
		assert.NoError(t, err)
		assert.Equal(t, entry.ID, got.ID)
	})

	t.Run("Clears history for service with slash in name", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.History.URL = "/.history"

		service := &mockService{
			name:   "adyen/v71",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)

		database := router.GetDB("adyen/v71")
		database.History().Set(context.Background(), "/payments", &db.HistoryRequest{
			Method: "POST",
			URL:    "/adyen/v71/payments",
		}, &db.HistoryResponse{
			StatusCode: 200,
		})

		_ = CreateHistoryRoutes(router)

		req := httptest.NewRequest(http.MethodDelete, "/.history?service=adyen/v71", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 0, database.History().Len(context.Background()))
	})
}

func TestHistoryHandler_rootService(t *testing.T) {
	t.Run("Returns history for root service via .root name", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.History.URL = "/.history"

		service := &mockService{
			name:   "",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)

		database := router.GetDB("")
		database.History().Set(context.Background(), "/health", &db.HistoryRequest{
			Method: "GET",
			URL:    "/health",
		}, &db.HistoryResponse{
			StatusCode: 200,
		})

		_ = CreateHistoryRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.history?service="+RootServiceName, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response HistorySummaryListResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Len(t, response.Items, 1)
	})
}

func TestHistoryHandler_serviceHistoryDisabled(t *testing.T) {
	t.Run("Returns 404 when service has history disabled", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.History.URL = "/.history"

		disabled := false
		svcCfg := config.NewServiceConfig()
		svcCfg.History.Enabled = &disabled

		service := &mockService{
			name:   "no-history",
			config: svcCfg,
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)
		_ = CreateHistoryRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.history?service=no-history", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "History disabled")
	})
}

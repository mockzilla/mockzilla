package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/middleware"
	"github.com/stretchr/testify/assert"
)

func seedReplay(router *Router, serviceName, key string, rec *middleware.ReplayRecord) {
	router.GetDB(serviceName).Table("replay").Set(context.Background(), key, rec, 0)
}

func sampleReplayRecord() *middleware.ReplayRecord {
	return &middleware.ReplayRecord{
		Method:      "POST",
		Path:        "/pay/credit-card",
		Resource:    "/pay/{paymentMethod}",
		Data:        []byte(`{"ok":true}`),
		Headers:     map[string]string{"Content-Type": "application/json"},
		StatusCode:  200,
		ContentType: "application/json",
		RequestBody: []byte(`{"reference":"abc123"}`),
		MatchValues: map[string]any{"body:reference": "abc123"},
		CreatedAt:   time.Now(),
	}
}

func TestCreateReplayRoutes(t *testing.T) {
	t.Run("Creates replay routes when UI is enabled", func(t *testing.T) {
		router := newTestRouter(t)

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)

		err := CreateReplayRoutes(router)
		assert.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/.replay?service=test-service", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Does not create routes when UI is disabled", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.DisableUI = true

		err := CreateReplayRoutes(router)
		assert.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/.replay?service=test-service", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("Does not create routes when replay URL is empty", func(t *testing.T) {
		router := newTestRouter(t)
		router.config.Replay.URL = ""

		err := CreateReplayRoutes(router)
		assert.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/.replay?service=test-service", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestReplayHandler_list(t *testing.T) {
	t.Run("Returns empty list when no recordings", func(t *testing.T) {
		router := newTestRouter(t)

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)
		_ = CreateReplayRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.replay?service=test-service", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ReplaySummaryListResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Empty(t, response.Items)
	})

	t.Run("Returns summaries without bodies", func(t *testing.T) {
		router := newTestRouter(t)

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)
		seedReplay(router, "test-service", "key1", sampleReplayRecord())
		_ = CreateReplayRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.replay?service=test-service", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ReplaySummaryListResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Len(t, response.Items, 1)

		item := response.Items[0]
		assert.Equal(t, "key1", item.Key)
		assert.Equal(t, "POST", item.Method)
		assert.Equal(t, "/pay/{paymentMethod}", item.Resource)
		assert.Equal(t, 200, item.StatusCode)
		assert.Equal(t, "abc123", item.MatchValues["body:reference"])

		// The summary JSON must not carry the bodies.
		var raw map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &raw)
		first := raw["items"].([]any)[0].(map[string]any)
		_, hasData := first["data"]
		_, hasReqBody := first["requestBody"]
		assert.False(t, hasData)
		assert.False(t, hasReqBody)
	})

	t.Run("Sorts newest first", func(t *testing.T) {
		router := newTestRouter(t)

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)

		older := sampleReplayRecord()
		older.CreatedAt = time.Now().Add(-1 * time.Hour)
		older.MatchValues = map[string]any{"body:reference": "older"}
		newer := sampleReplayRecord()
		newer.MatchValues = map[string]any{"body:reference": "newer"}
		seedReplay(router, "test-service", "old", older)
		seedReplay(router, "test-service", "new", newer)
		_ = CreateReplayRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.replay?service=test-service", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		var response ReplaySummaryListResponse
		_ = json.Unmarshal(w.Body.Bytes(), &response)
		assert.Len(t, response.Items, 2)
		assert.Equal(t, "new", response.Items[0].Key)
		assert.Equal(t, "old", response.Items[1].Key)
	})

	t.Run("Caps at 100 newest and flags truncated", func(t *testing.T) {
		router := newTestRouter(t)

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)
		for i := 0; i < maxSummaryItems+1; i++ {
			seedReplay(router, "test-service", fmt.Sprintf("key%d", i), sampleReplayRecord())
		}
		_ = CreateReplayRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.replay?service=test-service", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		var response ReplaySummaryListResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Len(t, response.Items, maxSummaryItems)
		assert.True(t, response.Truncated)
	})

	t.Run("Returns 404 for unknown service", func(t *testing.T) {
		router := newTestRouter(t)
		_ = CreateReplayRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.replay?service=nonexistent", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "Service not found")
	})
}

func TestReplayHandler_getByKey(t *testing.T) {
	t.Run("Returns full record by key with bodies", func(t *testing.T) {
		router := newTestRouter(t)

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)
		seedReplay(router, "test-service", "key1", sampleReplayRecord())
		_ = CreateReplayRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.replay?service=test-service&key=key1", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var got middleware.ReplayRecord
		err := json.Unmarshal(w.Body.Bytes(), &got)
		assert.NoError(t, err)
		assert.Equal(t, "POST", got.Method)
		assert.Equal(t, 200, got.StatusCode)
		assert.Equal(t, []byte(`{"ok":true}`), got.Data)
		assert.Equal(t, []byte(`{"reference":"abc123"}`), got.RequestBody)
		assert.Equal(t, "abc123", got.MatchValues["body:reference"])
	})

	t.Run("Returns 404 for unknown key", func(t *testing.T) {
		router := newTestRouter(t)

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)
		_ = CreateReplayRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.replay?service=test-service&key=nope", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "Recording not found")
	})
}

func TestReplayHandler_clear(t *testing.T) {
	t.Run("Deletes a single recording by key", func(t *testing.T) {
		router := newTestRouter(t)

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)
		seedReplay(router, "test-service", "keep", sampleReplayRecord())
		seedReplay(router, "test-service", "drop", sampleReplayRecord())
		_ = CreateReplayRoutes(router)

		req := httptest.NewRequest(http.MethodDelete, "/.replay?service=test-service&key=drop", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		data := router.GetDB("test-service").Table("replay").Data(context.Background())
		assert.Len(t, data, 1)
		_, kept := data["keep"]
		assert.True(t, kept)
	})

	t.Run("Clears all recordings", func(t *testing.T) {
		router := newTestRouter(t)

		service := &mockService{
			name:   "test-service",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)
		seedReplay(router, "test-service", "a", sampleReplayRecord())
		seedReplay(router, "test-service", "b", sampleReplayRecord())
		_ = CreateReplayRoutes(router)

		req := httptest.NewRequest(http.MethodDelete, "/.replay?service=test-service", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ReplaySummaryListResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Empty(t, response.Items)

		data := router.GetDB("test-service").Table("replay").Data(context.Background())
		assert.Empty(t, data)
	})

	t.Run("Returns 404 for unknown service", func(t *testing.T) {
		router := newTestRouter(t)
		_ = CreateReplayRoutes(router)

		req := httptest.NewRequest(http.MethodDelete, "/.replay?service=nonexistent", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestReplayHandler_rootService(t *testing.T) {
	t.Run("Returns recordings for root service via .root name", func(t *testing.T) {
		router := newTestRouter(t)

		service := &mockService{
			name:   "",
			config: config.NewServiceConfig(),
			routes: func(r chi.Router) {},
		}
		registerTestService(router, service)
		seedReplay(router, "", "rootkey", sampleReplayRecord())
		_ = CreateReplayRoutes(router)

		req := httptest.NewRequest(http.MethodGet, "/.replay?service="+RootServiceName, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ReplaySummaryListResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Len(t, response.Items, 1)
	})
}

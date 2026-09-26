package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/stretchr/testify/assert"
)

func TestCreateServiceConfigRoutes(t *testing.T) {
	router := newTestRouter(t)
	service := &mockService{
		name:   "test-service",
		config: config.NewServiceConfig(),
		routes: func(r chi.Router) {},
	}
	registerTestService(router, service)

	assert.NoError(t, CreateServiceConfigRoutes(router))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/.config?service=test-service", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

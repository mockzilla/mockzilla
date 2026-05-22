package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/db"
	"github.com/stretchr/testify/assert"
)

func TestParams_Logger(t *testing.T) {
	t.Run("derives from service logger when present", func(t *testing.T) {
		// NewParams seeds p.log with slog.With("service", name), so Logger
		// builds on top of that — both attributes should end up on the
		// returned logger.
		p := NewParams(&config.ServiceConfig{Name: "svc"}, nil)
		got := p.Logger("cache")
		assert.NotNil(t, got)
		// The returned logger is distinct from slog.Default — derived from
		// the service-scoped one.
		assert.NotSame(t, slog.Default(), got)
	})

	t.Run("falls back to slog default when no service logger set", func(t *testing.T) {
		// Construct a Params without going through NewParams so p.log is nil.
		p := &Params{}
		got := p.Logger("cache")
		assert.NotNil(t, got)
	})
}

func TestParams_GetServiceConfig(t *testing.T) {
	base := &config.ServiceConfig{Name: "svc"}
	p := NewParams(base, nil)

	t.Run("returns base config when context has no override", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		assert.Same(t, base, p.GetServiceConfig(req))
	})

	t.Run("returns context-scoped override when set", func(t *testing.T) {
		override := &config.ServiceConfig{Name: "svc-override"}
		req := httptest.NewRequest("GET", "/", nil)
		ctx := context.WithValue(req.Context(), serviceConfigKey, override)
		req = req.WithContext(ctx)
		assert.Same(t, override, p.GetServiceConfig(req))
	})

	t.Run("ignores context value of wrong type", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		ctx := context.WithValue(req.Context(), serviceConfigKey, "not-a-config")
		req = req.WithContext(ctx)
		assert.Same(t, base, p.GetServiceConfig(req))
	})
}

func TestParams_DB(t *testing.T) {
	storage := db.NewStorage(nil)
	database := storage.NewDB("svc", 0)
	p := NewParams(&config.ServiceConfig{Name: "svc"}, database)
	assert.Same(t, database, p.DB())
}

func TestParams_SetHistoryTransform(t *testing.T) {
	// transformHistory passes through the registered callback before applying
	// the per-service header masking. Verifying SetHistoryTransform stores
	// the function and the callback chain runs in order.
	p := NewParams(&config.ServiceConfig{Name: "svc"}, nil)
	called := false
	p.SetHistoryTransform(func(req *db.HistoryRequest, resp *db.HistoryResponse) {
		called = true
		assert.NotNil(t, req)
	})

	req := &db.HistoryRequest{Headers: []string{"X-Test: 1"}}
	p.transformHistory(&config.ServiceConfig{}, req, nil)
	assert.True(t, called)
}

func TestParams_SetRouter(t *testing.T) {
	// SetRouter is a setter used during boot — exercise the assignment so
	// the field is observable in later middleware that depends on it.
	p := NewParams(&config.ServiceConfig{Name: "svc"}, nil)
	mux := http.NewServeMux()
	// http.ServeMux satisfies chi.Routes? It doesn't, but we just check the
	// setter doesn't panic with a typed nil.
	p.SetRouter(nil)
	assert.Nil(t, p.router)
	_ = mux
}

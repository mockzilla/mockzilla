package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mockzilla/connexions/v2/pkg/config"
	assert2 "github.com/stretchr/testify/assert"
)

func TestCreateLatencyAndErrorMiddleware(t *testing.T) {
	assert := assert2.New(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Hello, world!"))
	})

	t.Run("no latency and no error", func(t *testing.T) {
		cfg := config.NewServiceConfig()

		params := newTestParams(cfg)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		mw := CreateLatencyAndErrorMiddleware(params)
		start := time.Now()
		mw(handler).ServeHTTP(w, req)
		duration := time.Since(start)

		assert.Equal("Hello, world!", string(w.buf))
		assert.Equal(http.StatusOK, w.statusCode)
		assert.Less(duration, 50*time.Millisecond, "Should not have any latency")
	})

	t.Run("with latency", func(t *testing.T) {
		cfg := config.NewServiceConfig()
		cfg.Latency = 100 * time.Millisecond

		params := newTestParams(cfg)

		// Capture slog output
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, nil))
		oldLogger := slog.Default()
		slog.SetDefault(logger)
		defer slog.SetDefault(oldLogger)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		mw := CreateLatencyAndErrorMiddleware(params)
		start := time.Now()
		mw(handler).ServeHTTP(w, req)
		duration := time.Since(start)

		assert.Equal("Hello, world!", string(w.buf))
		assert.Equal(http.StatusOK, w.statusCode)
		assert.GreaterOrEqual(duration, 100*time.Millisecond, "Should have at least 100ms latency")

		// Verify latency was logged
		logOutput := buf.String()
		assert.True(strings.Contains(logOutput, "Latency"), "Expected log to contain 'Latency'")
		assert.True(strings.Contains(logOutput, "delay"), "Expected log to contain 'delay'")
	})

	t.Run("with error", func(t *testing.T) {
		cfg := config.NewServiceConfig()
		cfg.Errors = map[string]int{
			"p100": http.StatusInternalServerError,
		}
		// Parse errors to populate internal errors slice
		cfgBytes := []byte(`
errors:
  p100: 500
`)
		cfg, _ = config.NewServiceConfigFromBytes(cfgBytes)

		params := newTestParams(cfg)

		// Capture slog output
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, nil))
		oldLogger := slog.Default()
		slog.SetDefault(logger)
		defer slog.SetDefault(oldLogger)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		mw := CreateLatencyAndErrorMiddleware(params)
		mw(handler).ServeHTTP(w, req)

		// Should return error, not call next handler
		assert.Equal(http.StatusInternalServerError, w.statusCode)
		assert.True(strings.Contains(string(w.buf), "Simulated error"), "Expected error message")

		// Verify error was logged
		logOutput := buf.String()
		assert.True(strings.Contains(logOutput, "Simulated error"), "Expected log to contain 'Simulated error'")
		assert.True(strings.Contains(logOutput, "code"), "Expected log to contain 'code'")

		// Verify custom response header is set
		assert.Equal(ResponseHeaderSourceGenerated, w.header.Get(ResponseHeaderSource))
	})

	t.Run("with latency and error", func(t *testing.T) {
		cfgBytes := []byte(`
latency: 50ms
errors:
  p100: 503
`)
		cfg, _ := config.NewServiceConfigFromBytes(cfgBytes)

		params := newTestParams(cfg)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		mw := CreateLatencyAndErrorMiddleware(params)
		start := time.Now()
		mw(handler).ServeHTTP(w, req)
		duration := time.Since(start)

		// Should have latency AND return error
		assert.GreaterOrEqual(duration, 50*time.Millisecond, "Should have at least 50ms latency")
		assert.Equal(http.StatusServiceUnavailable, w.statusCode)
		assert.True(strings.Contains(string(w.buf), "Simulated error"), "Expected error message")
	})
}

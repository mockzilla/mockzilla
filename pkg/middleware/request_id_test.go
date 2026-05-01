package middleware

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chiMw "github.com/go-chi/chi/v5/middleware"
	assert2 "github.com/stretchr/testify/assert"
)

func TestGetRequestID(t *testing.T) {
	assert := assert2.New(t)

	t.Run("returns request ID from context", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		ctx := context.WithValue(req.Context(), chiMw.RequestIDKey, "test-request-123")
		req = req.WithContext(ctx)

		assert.Equal("test-request-123", GetRequestID(req))
	})

	t.Run("returns empty when no request ID in context", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		assert.Equal("", GetRequestID(req))
	})
}

func TestSetRequestIDHeader(t *testing.T) {
	assert := assert2.New(t)

	t.Run("sets header when request ID exists", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		ctx := context.WithValue(req.Context(), chiMw.RequestIDKey, "req-abc-456")
		req = req.WithContext(ctx)

		SetRequestIDHeader(w, req)

		assert.Equal("req-abc-456", w.Header().Get(ResponseHeaderRequestID))
	})

	t.Run("does nothing when no request ID", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		SetRequestIDHeader(w, req)

		assert.Empty(w.Header().Get(ResponseHeaderRequestID))
	})
}

func TestRequestLog(t *testing.T) {
	assert := assert2.New(t)

	t.Run("enriches logger with request ID", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, nil))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		ctx := context.WithValue(req.Context(), chiMw.RequestIDKey, "req-enriched-789")
		req = req.WithContext(ctx)

		enriched := RequestLog(logger, req)
		enriched.Info("test message")

		logOutput := buf.String()
		assert.True(strings.Contains(logOutput, "requestId=req-enriched-789"), "Expected requestId in log output, got: %s", logOutput)
		assert.True(strings.Contains(logOutput, "test message"), "Expected message in log output")
	})

	t.Run("returns same logger when no request ID", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, nil))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		result := RequestLog(logger, req)
		result.Info("test message")

		logOutput := buf.String()
		assert.False(strings.Contains(logOutput, "requestId"), "Expected no requestId in log output, got: %s", logOutput)
		assert.True(strings.Contains(logOutput, "test message"), "Expected message in log output")
	})
}

func TestResponseHeaderRequestID(t *testing.T) {
	assert := assert2.New(t)
	assert.Equal("X-Mockzilla-Request-Id", ResponseHeaderRequestID)
}

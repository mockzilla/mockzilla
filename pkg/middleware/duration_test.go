package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	assert2 "github.com/stretchr/testify/assert"
)

func TestStartTimeMiddleware(t *testing.T) {
	assert := assert2.New(t)

	var capturedCtx context.Context
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
	})

	mw := StartTimeMiddleware(handler)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	mw.ServeHTTP(w, req)

	// Check that start time is stored in context
	startTime := capturedCtx.Value(startTimeKey)
	assert.NotNil(startTime)
	assert.IsType(time.Time{}, startTime)
}

func TestSetDurationHeader(t *testing.T) {
	assert := assert2.New(t)

	t.Run("sets header when start time exists", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		ctx := context.WithValue(req.Context(), startTimeKey, time.Now().Add(-10*time.Millisecond))
		req = req.WithContext(ctx)

		SetDurationHeader(w, req)

		duration := w.Header().Get("X-Mockzilla-Duration")
		assert.NotEmpty(duration)
		assert.True(strings.HasSuffix(duration, "ms"), "duration should end with 'ms'")
	})

	t.Run("does nothing when start time missing", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		SetDurationHeader(w, req)

		duration := w.Header().Get("X-Mockzilla-Duration")
		assert.Empty(duration)
	})
}

func TestGetDuration(t *testing.T) {
	assert := assert2.New(t)

	t.Run("returns elapsed time when start time exists", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		ctx := context.WithValue(req.Context(), startTimeKey, time.Now().Add(-100*time.Millisecond))
		req = req.WithContext(ctx)

		d := GetDuration(req)
		assert.GreaterOrEqual(d, 100*time.Millisecond)
		assert.Less(d, 200*time.Millisecond)
	})

	t.Run("returns zero when start time missing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		d := GetDuration(req)
		assert.Equal(time.Duration(0), d)
	})
}

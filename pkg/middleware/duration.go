package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type ctxKey string

const startTimeKey ctxKey = "startTime"

// StartTimeMiddleware stores request start time in context.
func StartTimeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), startTimeKey, time.Now())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// SetDurationHeader sets X-Mz-Duration header based on start time from context.
func SetDurationHeader(w http.ResponseWriter, r *http.Request) {
	if start, ok := r.Context().Value(startTimeKey).(time.Time); ok {
		duration := float64(time.Since(start).Microseconds()) / 1000
		w.Header().Set("X-Mz-Duration", fmt.Sprintf("%.3fms", duration))
	}
}

// GetDuration returns the elapsed time since the request started.
func GetDuration(r *http.Request) time.Duration {
	if start, ok := r.Context().Value(startTimeKey).(time.Time); ok {
		return time.Since(start)
	}
	return 0
}

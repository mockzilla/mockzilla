package middleware

import (
	"log/slog"
	"net/http"

	chiMw "github.com/go-chi/chi/v5/middleware"
)

// ResponseHeaderRequestID is the response header containing the request ID.
const ResponseHeaderRequestID = "X-Mockzilla-Request-Id"

// GetRequestID extracts the request ID set by chi's RequestID middleware.
func GetRequestID(r *http.Request) string {
	return chiMw.GetReqID(r.Context())
}

// SetRequestIDHeader sets the X-Mockzilla-Request-Id response header from context.
func SetRequestIDHeader(w http.ResponseWriter, r *http.Request) {
	if id := GetRequestID(r); id != "" {
		w.Header().Set(ResponseHeaderRequestID, id)
	}
}

// RequestLog returns a logger enriched with the request ID.
func RequestLog(log *slog.Logger, r *http.Request) *slog.Logger {
	if id := GetRequestID(r); id != "" {
		return log.With("requestId", id)
	}
	return log
}

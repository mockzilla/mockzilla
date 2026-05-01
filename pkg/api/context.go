package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
)

// ContextHeaderName is the header name for passing context replacements via HTTP requests.
// The value should be base64-encoded JSON.
const ContextHeaderName = "X-Mockzilla-Context"

type contextKeyType struct{}

var userContextKey = contextKeyType{}

// ExtractContextFromRequest reads and decodes the X-Mockzilla-Context header from an HTTP request.
// Returns nil if the header is absent or cannot be decoded.
func ExtractContextFromRequest(r *http.Request) map[string]any {
	encoded := r.Header.Get(ContextHeaderName)
	if encoded == "" {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil
	}
	var ctx map[string]any
	if err := json.Unmarshal(decoded, &ctx); err != nil {
		return nil
	}
	return ctx
}

// ContextReplacementsMiddleware extracts the X-Mockzilla-Context header and stores
// the decoded context data on the request's Go context.
func ContextReplacementsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ctxData := ExtractContextFromRequest(r); ctxData != nil {
			slog.Debug("User context from header", "data", ctxData)
			r = r.WithContext(context.WithValue(r.Context(), userContextKey, ctxData))
		}
		next.ServeHTTP(w, r)
	})
}

// UserContextFromGoContext retrieves user-provided context replacements from a Go context.
func UserContextFromGoContext(ctx context.Context) map[string]any {
	data, _ := ctx.Value(userContextKey).(map[string]any)
	return data
}

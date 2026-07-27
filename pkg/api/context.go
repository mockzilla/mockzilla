package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// ContextHeaderName is the header name for passing context replacements via HTTP requests.
// The value should be base64-encoded JSON.
const ContextHeaderName = "X-Mockzilla-Context"

// maxBufferedBodyBytes caps how much of a request body is kept for `request:`
// context values. Paths into a payload larger than this aren't worth the memory.
const maxBufferedBodyBytes = 1 << 20

type contextKeyType struct{}

type requestContextKeyType struct{}

type requestBodyKeyType struct{}

var (
	userContextKey    = contextKeyType{}
	requestContextKey = requestContextKeyType{}
	requestBodyKey    = requestBodyKeyType{}
)

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
// the decoded context data on the request's Go context, along with the request itself
// and a copy of its body.
func ContextReplacementsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if body, ok := bufferRequestBody(r); ok {
			ctx = context.WithValue(ctx, requestBodyKey, body)
		}
		if ctxData := ExtractContextFromRequest(r); ctxData != nil {
			slog.Debug("User context from header", "data", ctxData)
			ctx = context.WithValue(ctx, userContextKey, ctxData)
		}

		// The request stored under requestContextKey must carry the values
		// above on its own context: handlers reach the buffered body through
		// it, after the generated adapter has drained r.Body.
		r = r.WithContext(ctx)
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, requestContextKey, r)))
	})
}

// UserContextFromGoContext retrieves user-provided context replacements from a Go context.
func UserContextFromGoContext(ctx context.Context) map[string]any {
	data, _ := ctx.Value(userContextKey).(map[string]any)
	return data
}

// RequestFromGoContext retrieves the HTTP request stored by ContextReplacementsMiddleware.
func RequestFromGoContext(ctx context.Context) *http.Request {
	r, _ := ctx.Value(requestContextKey).(*http.Request)
	return r
}

// RequestBodyFromGoContext retrieves the request body buffered by
// ContextReplacementsMiddleware. Returns nil when nothing was buffered.
func RequestBodyFromGoContext(ctx context.Context) []byte {
	body, _ := ctx.Value(requestBodyKey).([]byte)
	return body
}

// RequestBody returns the body of the incoming request, preferring the copy
// buffered by ContextReplacementsMiddleware and falling back to reading r.Body
// directly. The body is restored either way, so handlers can still read it.
func RequestBody(r *http.Request) []byte {
	if r == nil {
		return nil
	}
	if body := RequestBodyFromGoContext(r.Context()); body != nil {
		return body
	}
	body, _ := bufferRequestBody(r)
	return body
}

// bufferRequestBody reads a structured request body into memory and restores
// r.Body for the handlers downstream. Only payloads a context path can navigate
// are buffered; uploads and unknown content types are left alone.
func bufferRequestBody(r *http.Request) ([]byte, bool) {
	if r.Body == nil || r.Body == http.NoBody || !isNavigableContentType(r.Header.Get("Content-Type")) {
		return nil, false
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBufferedBodyBytes))
	if err != nil {
		return nil, false
	}
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), r.Body))

	return body, len(body) > 0
}

func isNavigableContentType(contentType string) bool {
	contentType = strings.ToLower(contentType)
	return strings.Contains(contentType, "json") ||
		strings.Contains(contentType, "application/x-www-form-urlencoded")
}

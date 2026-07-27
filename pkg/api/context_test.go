package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	assert2 "github.com/stretchr/testify/assert"
)

func TestExtractContextFromRequest(t *testing.T) {
	assert := assert2.New(t)

	t.Run("no header returns nil", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		assert.Nil(ExtractContextFromRequest(r))
	})

	t.Run("empty header returns nil", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set(ContextHeaderName, "")
		assert.Nil(ExtractContextFromRequest(r))
	})

	t.Run("invalid base64 returns nil", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set(ContextHeaderName, "not-valid-base64!!!")
		assert.Nil(ExtractContextFromRequest(r))
	})

	t.Run("valid base64 but invalid JSON returns nil", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set(ContextHeaderName, base64.StdEncoding.EncodeToString([]byte("not json")))
		assert.Nil(ExtractContextFromRequest(r))
	})

	t.Run("valid context decoded", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set(ContextHeaderName, base64.StdEncoding.EncodeToString([]byte(`{"name":"foo","id":11}`)))
		ctx := ExtractContextFromRequest(r)
		assert.Equal("foo", ctx["name"])
		assert.Equal(float64(11), ctx["id"])
	})

	t.Run("nested context decoded", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		encoded := base64.StdEncoding.EncodeToString([]byte(`{"in-path":{"id":"func:int_between:2,10"}}`))
		r.Header.Set(ContextHeaderName, encoded)
		ctx := ExtractContextFromRequest(r)
		inPath, ok := ctx["in-path"].(map[string]any)
		assert.True(ok)
		assert.Equal("func:int_between:2,10", inPath["id"])
	})
}

func TestContextReplacementsMiddleware(t *testing.T) {
	assert := assert2.New(t)

	t.Run("no header passes through", func(t *testing.T) {
		var capturedCtx context.Context
		handler := ContextReplacementsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedCtx = r.Context()
		}))

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		handler.ServeHTTP(httptest.NewRecorder(), r)

		assert.Nil(UserContextFromGoContext(capturedCtx))
	})

	t.Run("valid header stored on context", func(t *testing.T) {
		var capturedCtx context.Context
		handler := ContextReplacementsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedCtx = r.Context()
		}))

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set(ContextHeaderName, base64.StdEncoding.EncodeToString([]byte(`{"status":"active"}`)))
		handler.ServeHTTP(httptest.NewRecorder(), r)

		ctx := UserContextFromGoContext(capturedCtx)
		assert.Equal("active", ctx["status"])
	})

	t.Run("request stored on context", func(t *testing.T) {
		var capturedCtx context.Context
		handler := ContextReplacementsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedCtx = r.Context()
		}))

		r := httptest.NewRequest(http.MethodGet, "/pets", nil)
		handler.ServeHTTP(httptest.NewRecorder(), r)

		stored := RequestFromGoContext(capturedCtx)
		assert.NotNil(stored)
		assert.Equal("/pets", stored.URL.Path)
	})
}

func TestUserContextFromGoContext(t *testing.T) {
	assert := assert2.New(t)

	t.Run("empty context returns nil", func(t *testing.T) {
		assert.Nil(UserContextFromGoContext(context.Background()))
	})

	t.Run("returns stored data", func(t *testing.T) {
		data := map[string]any{"foo": "bar"}
		ctx := context.WithValue(context.Background(), userContextKey, data)
		assert.Equal(data, UserContextFromGoContext(ctx))
	})
}

func TestRequestFromGoContext(t *testing.T) {
	assert := assert2.New(t)

	t.Run("empty context returns nil", func(t *testing.T) {
		assert.Nil(RequestFromGoContext(context.Background()))
	})

	t.Run("returns stored request", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		ctx := context.WithValue(context.Background(), requestContextKey, r)
		assert.Same(r, RequestFromGoContext(ctx))
	})
}

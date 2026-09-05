package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
	assert2 "github.com/stretchr/testify/assert"
)

// seedCache stores a response in the request cache the way cache-write would.
func seedCache(params *Params, method, url string, resp *cachedResponse) {
	writeCache(context.Background(), params.DB(), cacheKey(method, url), resp, time.Minute)
}

func TestCreateCacheReadMiddleware(t *testing.T) {
	assert := assert2.New(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fresh"))
	})

	t.Run("nil config passes through", func(t *testing.T) {
		params := newTestParams(nil)
		params.serviceConfig = nil
		mw := CreateCacheReadMiddleware(params)
		assert.NotNil(mw)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/foo/bar", nil)

		mw(handler).ServeHTTP(w, req)

		assert.Equal("fresh", string(w.buf))
	})

	t.Run("nil cache config passes through", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{
			Name:  "test",
			Cache: nil,
		})
		mw := CreateCacheReadMiddleware(params)
		assert.NotNil(mw)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/foo/bar", nil)

		mw(handler).ServeHTTP(w, req)

		assert.Equal("fresh", string(w.buf))
	})

	t.Run("on", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{
			Name: "foo",
			Cache: &config.CacheConfig{
				Requests: true,
			},
		})

		seedCache(params, http.MethodGet, "/foo/bar", &cachedResponse{
			Body:        []byte("cached"),
			StatusCode:  http.StatusOK,
			ContentType: "application/json",
		})

		mw := CreateCacheReadMiddleware(params)
		assert.NotNil(mw)

		t.Run("not-get", func(t *testing.T) {
			w := NewBufferedResponseWriter()
			req := httptest.NewRequest(http.MethodPost, "/foo/bar", nil)

			mw(handler).ServeHTTP(w, req)

			assert.Equal("fresh", string(w.buf))
		})

		t.Run("get-no-cache", func(t *testing.T) {
			w := NewBufferedResponseWriter()
			req := httptest.NewRequest(http.MethodGet, "/foo/bar/new", nil)

			mw(handler).ServeHTTP(w, req)

			assert.Equal("fresh", string(w.buf))
		})

		t.Run("get-cache", func(t *testing.T) {
			w := NewBufferedResponseWriter()
			req := httptest.NewRequest(http.MethodGet, "/foo/bar", nil)

			mw(handler).ServeHTTP(w, req)

			assert.Equal("cached", string(w.buf))
		})
	})

	t.Run("off", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{
			Name: "foo",
			Cache: &config.CacheConfig{
				Requests: false,
			},
		})

		seedCache(params, http.MethodGet, "/foo/bar", &cachedResponse{
			Body:       []byte("cached"),
			StatusCode: http.StatusOK,
		})

		mw := CreateCacheReadMiddleware(params)
		assert.NotNil(mw)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/foo/bar", nil)

		mw(handler).ServeHTTP(w, req)

		assert.Equal("fresh", string(w.buf))
	})

	t.Run("serves the cache with history off", func(t *testing.T) {
		disabled := false
		params := newTestParams(&config.ServiceConfig{
			Name: "foo",
			Cache: &config.CacheConfig{
				Requests: true,
			},
			History: &config.HistoryConfig{
				Enabled: &disabled,
			},
		})

		seedCache(params, http.MethodGet, "/foo/bar", &cachedResponse{
			Body:       []byte("cached"),
			StatusCode: http.StatusOK,
		})

		mw := CreateCacheReadMiddleware(params)
		assert.NotNil(mw)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/foo/bar", nil)

		mw(handler).ServeHTTP(w, req)

		assert.Equal("cached", string(w.buf))
	})

	t.Run("restores content-type from cache", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{
			Name: "service",
			Cache: &config.CacheConfig{
				Requests: true,
			},
		})

		seedCache(params, http.MethodGet, "/api/data", &cachedResponse{
			Body:        []byte(`{"cached": true}`),
			StatusCode:  http.StatusOK,
			ContentType: "application/json",
		})

		mw := CreateCacheReadMiddleware(params)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/api/data", nil)

		mw(handler).ServeHTTP(w, req)

		assert.Equal(`{"cached": true}`, string(w.buf))
		assert.Equal("application/json", w.header.Get("Content-Type"))
	})

	t.Run("sets custom response headers on cache hit", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{
			Name: "service",
			Cache: &config.CacheConfig{
				Requests: true,
			},
		})

		seedCache(params, http.MethodGet, "/api/cached", &cachedResponse{
			Body:       []byte(`{"cached": true}`),
			StatusCode: http.StatusOK,
		})

		mw := CreateCacheReadMiddleware(params)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/api/cached", nil)

		mw(handler).ServeHTTP(w, req)

		assert.Equal(ResponseHeaderSourceCache, w.header.Get(ResponseHeaderSource))
	})

	t.Run("query string is part of the key", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{
			Name: "service",
			Cache: &config.CacheConfig{
				Requests: true,
			},
		})

		seedCache(params, http.MethodGet, "/api/data?page=1", &cachedResponse{
			Body:       []byte("page one"),
			StatusCode: http.StatusOK,
		})

		mw := CreateCacheReadMiddleware(params)

		w := NewBufferedResponseWriter()
		mw(handler).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/data?page=1", nil))
		assert.Equal("page one", string(w.buf))

		w = NewBufferedResponseWriter()
		mw(handler).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/data?page=2", nil))
		assert.Equal("fresh", string(w.buf))
	})
}

func TestWriteCache_SkipsOversizedBody(t *testing.T) {
	assert := assert2.New(t)

	params := newTestParams(&config.ServiceConfig{
		Name:  "service",
		Cache: &config.CacheConfig{Requests: true},
	})

	seedCache(params, http.MethodGet, "/api/big", &cachedResponse{
		Body:       make([]byte, maxCacheBodyBytes+1),
		StatusCode: http.StatusOK,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/big", nil)
	_, ok := readCache(context.Background(), params.DB(), req)
	assert.False(ok)
}

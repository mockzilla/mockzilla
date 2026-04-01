package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mockzilla/connexions/v2/pkg/config"
	assert2 "github.com/stretchr/testify/assert"
)

func TestParseConfigOverrides(t *testing.T) {
	assert := assert2.New(t)

	t.Run("no headers returns empty", func(t *testing.T) {
		headers := http.Header{}
		overrides := parseConfigOverrides(headers)
		assert.Empty(overrides)
	})

	t.Run("non-matching headers ignored", func(t *testing.T) {
		headers := http.Header{
			"Content-Type":  []string{"application/json"},
			"Authorization": []string{"Bearer token"},
		}
		overrides := parseConfigOverrides(headers)
		assert.Empty(overrides)
	})

	t.Run("parses X-Cxs headers", func(t *testing.T) {
		headers := http.Header{
			"X-Cxs-Cache-Requests": []string{"false"},
			"X-Cxs-Latency":        []string{"100ms"},
		}
		overrides := parseConfigOverrides(headers)
		assert.Len(overrides, 2)
	})

	t.Run("headers are case-insensitive via http.Header canonicalization", func(t *testing.T) {
		headers := http.Header{}
		// http.Header.Set canonicalizes the key
		headers.Set("x-cxs-cache-requests", "false")
		headers.Set("X-CXS-LATENCY", "100ms")

		overrides := parseConfigOverrides(headers)
		assert.Len(overrides, 2)

		// Keys should be canonicalized
		keys := make(map[string]bool)
		for _, o := range overrides {
			keys[o.key] = true
		}
		assert.True(keys["Cache-Requests"])
		assert.True(keys["Latency"])
	})

	t.Run("uses first value for multiple values", func(t *testing.T) {
		headers := http.Header{
			"X-Cxs-Latency": []string{"100ms", "200ms"},
		}
		overrides := parseConfigOverrides(headers)
		assert.Len(overrides, 1)
		assert.Equal("100ms", overrides[0].value)
	})

	t.Run("skips headers with empty values array", func(t *testing.T) {
		headers := http.Header{
			"X-Cxs-Latency": []string{},
		}
		overrides := parseConfigOverrides(headers)
		assert.Len(overrides, 0)
	})
}

func TestApplyOverrides(t *testing.T) {
	assert := assert2.New(t)

	t.Run("nil config returns nil", func(t *testing.T) {
		result := applyOverrides(nil, []configOverride{{key: "Latency", value: "100ms"}})
		assert.Nil(result)
	})

	t.Run("empty overrides returns copy", func(t *testing.T) {
		original := &config.ServiceConfig{Name: "test"}
		result := applyOverrides(original, nil)
		assert.NotSame(original, result)
		assert.Equal("test", result.Name)
	})

	t.Run("overrides Cache-Requests", func(t *testing.T) {
		original := &config.ServiceConfig{
			Cache: &config.CacheConfig{Requests: true},
		}
		result := applyOverrides(original, []configOverride{
			{key: headerCacheRequests, value: "false"},
		})
		assert.False(result.Cache.Requests)
		// Original unchanged
		assert.True(original.Cache.Requests)
	})

	t.Run("creates Cache if nil", func(t *testing.T) {
		original := &config.ServiceConfig{}
		result := applyOverrides(original, []configOverride{
			{key: headerCacheRequests, value: "false"},
		})
		assert.NotNil(result.Cache)
		assert.False(result.Cache.Requests)
	})

	t.Run("overrides Latency", func(t *testing.T) {
		original := &config.ServiceConfig{Latency: 50 * time.Millisecond}
		result := applyOverrides(original, []configOverride{
			{key: headerLatency, value: "200ms"},
		})
		assert.Equal(200*time.Millisecond, result.Latency)
	})

	t.Run("invalid latency ignored", func(t *testing.T) {
		original := &config.ServiceConfig{Latency: 50 * time.Millisecond}
		result := applyOverrides(original, []configOverride{
			{key: headerLatency, value: "invalid"},
		})
		assert.Equal(50*time.Millisecond, result.Latency)
	})

	t.Run("overrides Upstream-Url", func(t *testing.T) {
		original := &config.ServiceConfig{
			Upstream: &config.UpstreamConfig{URL: "http://old.com"},
		}
		result := applyOverrides(original, []configOverride{
			{key: headerUpstreamURL, value: "http://new.com"},
		})
		assert.Equal("http://new.com", result.Upstream.URL)
	})

	t.Run("empty Upstream-Url sets Upstream to nil", func(t *testing.T) {
		original := &config.ServiceConfig{
			Upstream: &config.UpstreamConfig{URL: "http://old.com"},
		}
		result := applyOverrides(original, []configOverride{
			{key: headerUpstreamURL, value: ""},
		})
		assert.Nil(result.Upstream)
		// Original unchanged
		assert.NotNil(original.Upstream)
	})

	t.Run("creates Upstream if nil and URL provided", func(t *testing.T) {
		original := &config.ServiceConfig{}
		result := applyOverrides(original, []configOverride{
			{key: headerUpstreamURL, value: "http://new.com"},
		})
		assert.NotNil(result.Upstream)
		assert.Equal("http://new.com", result.Upstream.URL)
	})
}

func TestCreateConfigOverrideMiddleware(t *testing.T) {
	assert := assert2.New(t)

	t.Run("no headers passes through unchanged", func(t *testing.T) {
		original := &config.ServiceConfig{
			Name:    "test",
			Latency: 100 * time.Millisecond,
		}
		params := newTestParams(original)

		var capturedConfig *config.ServiceConfig
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedConfig = params.GetServiceConfig(r)
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := NewBufferedResponseWriter()

		mw := CreateConfigOverrideMiddleware(params)
		mw(handler).ServeHTTP(w, req)

		assert.Same(original, capturedConfig)
	})

	t.Run("overrides config for request duration", func(t *testing.T) {
		original := &config.ServiceConfig{
			Name:    "test",
			Latency: 100 * time.Millisecond,
		}
		params := newTestParams(original)

		var capturedLatency time.Duration
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedLatency = params.GetServiceConfig(r).Latency
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("X-Cxs-Latency", "500ms")
		w := NewBufferedResponseWriter()

		mw := CreateConfigOverrideMiddleware(params)
		mw(handler).ServeHTTP(w, req)

		// Handler saw overridden value
		assert.Equal(500*time.Millisecond, capturedLatency)
		// Original unchanged
		assert.Equal(100*time.Millisecond, original.Latency)
	})

	t.Run("original config unchanged after override", func(t *testing.T) {
		original := &config.ServiceConfig{
			Name:    "test",
			Latency: 100 * time.Millisecond,
		}
		params := newTestParams(original)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("X-Cxs-Latency", "500ms")
		w := NewBufferedResponseWriter()

		mw := CreateConfigOverrideMiddleware(params)
		mw(handler).ServeHTTP(w, req)

		// Original never mutated
		assert.Equal(100*time.Millisecond, original.Latency)
	})

	t.Run("X-Cxs headers are preserved on request", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{Name: "test"})

		var capturedHeaders http.Header
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedHeaders = r.Header.Clone()
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("X-Cxs-Latency", "200ms")
		req.Header.Set("X-Cxs-Context", "eyJmb28iOiJiYXIifQ==")
		w := NewBufferedResponseWriter()

		mw := CreateConfigOverrideMiddleware(params)
		mw(handler).ServeHTTP(w, req)

		assert.Equal("200ms", capturedHeaders.Get("X-Cxs-Latency"))
		assert.Equal("eyJmb28iOiJiYXIifQ==", capturedHeaders.Get("X-Cxs-Context"))
	})

	t.Run("browser headers are stripped from request", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{Name: "test"})

		var capturedHeaders http.Header
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedHeaders = r.Header.Clone()
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer 123")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost:2200")
		req.Header.Set("Referer", "http://localhost:2200/ui")
		req.Header.Set("Cookie", "session=abc")
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Sec-Ch-Ua", "\"Chromium\";v=\"130\"")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		w := NewBufferedResponseWriter()

		mw := CreateConfigOverrideMiddleware(params)
		mw(handler).ServeHTTP(w, req)

		// Authorization is preserved when request is not from UI
		assert.Equal("Bearer 123", capturedHeaders.Get("Authorization"))
		assert.Equal("application/json", capturedHeaders.Get("Content-Type"))
		assert.Empty(capturedHeaders.Get("Origin"))
		assert.Empty(capturedHeaders.Get("Referer"))
		assert.Empty(capturedHeaders.Get("Cookie"))
		assert.Empty(capturedHeaders.Get("Sec-Fetch-Mode"))
		assert.Empty(capturedHeaders.Get("Sec-Fetch-Site"))
		assert.Empty(capturedHeaders.Get("Sec-Ch-Ua"))
		assert.Empty(capturedHeaders.Get("Accept-Language"))
	})

	t.Run("Authorization is stripped when request comes from UI", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{Name: "test"})

		var capturedHeaders http.Header
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedHeaders = r.Header.Clone()
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Basic ui-session-creds")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Cxs-Source", "ui")
		req.Header.Set("Origin", "http://localhost:2200")
		w := NewBufferedResponseWriter()

		mw := CreateConfigOverrideMiddleware(params)
		mw(handler).ServeHTTP(w, req)

		assert.Empty(capturedHeaders.Get("Authorization"))
		assert.Empty(capturedHeaders.Get("Origin"))
		assert.Equal("application/json", capturedHeaders.Get("Content-Type"))
		assert.Equal("ui", capturedHeaders.Get("X-Cxs-Source"))
	})

	t.Run("multiple overrides applied", func(t *testing.T) {
		original := &config.ServiceConfig{
			Name:    "test",
			Latency: 100 * time.Millisecond,
			Cache:   &config.CacheConfig{Requests: true},
		}
		params := newTestParams(original)

		var captured struct {
			latency       time.Duration
			cacheRequests bool
		}
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cfg := params.GetServiceConfig(r)
			captured.latency = cfg.Latency
			captured.cacheRequests = cfg.Cache.Requests
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("X-Cxs-Latency", "200ms")
		req.Header.Set("X-Cxs-Cache-Requests", "false")
		w := NewBufferedResponseWriter()

		mw := CreateConfigOverrideMiddleware(params)
		mw(handler).ServeHTTP(w, req)

		assert.Equal(200*time.Millisecond, captured.latency)
		assert.False(captured.cacheRequests)
	})
}

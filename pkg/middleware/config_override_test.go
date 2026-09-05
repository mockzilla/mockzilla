package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
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

	t.Run("parses X-Mockzilla headers", func(t *testing.T) {
		headers := http.Header{
			"X-Mockzilla-Cache-Requests": []string{"false"},
			"X-Mockzilla-Latency":        []string{"100ms"},
		}
		overrides := parseConfigOverrides(headers)
		assert.Len(overrides, 2)
	})

	t.Run("headers are case-insensitive via http.Header canonicalization", func(t *testing.T) {
		headers := http.Header{}
		// http.Header.Set canonicalizes the key
		headers.Set("x-mockzilla-cache-requests", "false")
		headers.Set("X-MOCKZILLA-LATENCY", "100ms")

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
			"X-Mockzilla-Latency": []string{"100ms", "200ms"},
		}
		overrides := parseConfigOverrides(headers)
		assert.Len(overrides, 1)
		assert.Equal("100ms", overrides[0].value)
	})

	t.Run("skips headers with empty values array", func(t *testing.T) {
		headers := http.Header{
			"X-Mockzilla-Latency": []string{},
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
		original := &config.ServiceConfig{BehaviorConfig: config.BehaviorConfig{Latency: 50 * time.Millisecond}}
		result := applyOverrides(original, []configOverride{
			{key: headerLatency, value: "200ms"},
		})
		assert.Equal(200*time.Millisecond, result.Latency)
	})

	t.Run("invalid latency ignored", func(t *testing.T) {
		original := &config.ServiceConfig{BehaviorConfig: config.BehaviorConfig{Latency: 50 * time.Millisecond}}
		result := applyOverrides(original, []configOverride{
			{key: headerLatency, value: "invalid"},
		})
		assert.Equal(50*time.Millisecond, result.Latency)
	})

	t.Run("overrides Upstream-Url", func(t *testing.T) {
		original := &config.ServiceConfig{
			BehaviorConfig: config.BehaviorConfig{Upstream: &config.UpstreamConfig{URL: "http://old.com"}},
		}
		result := applyOverrides(original, []configOverride{
			{key: headerUpstreamURL, value: "http://new.com"},
		})
		assert.Equal("http://new.com", result.Upstream.URL)
	})

	t.Run("empty Upstream-Url sets Upstream to nil", func(t *testing.T) {
		original := &config.ServiceConfig{
			BehaviorConfig: config.BehaviorConfig{Upstream: &config.UpstreamConfig{URL: "http://old.com"}},
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

	t.Run("overrides Validate-Request true on service with no Validate config", func(t *testing.T) {
		original := &config.ServiceConfig{}
		result := applyOverrides(original, []configOverride{
			{key: headerValidateRequest, value: "true"},
		})
		assert.NotNil(result.Validate)
		assert.True(result.Validate.RequestEnabled())
		// Original untouched
		assert.Nil(original.Validate)
	})

	t.Run("overrides Validate-Response false on service that booted with it on", func(t *testing.T) {
		on := true
		original := &config.ServiceConfig{
			Validate: &config.ValidateConfig{Request: &on, Response: &on},
		}
		result := applyOverrides(original, []configOverride{
			{key: headerValidateResponse, value: "false"},
		})
		assert.True(result.Validate.RequestEnabled())
		assert.False(result.Validate.ResponseEnabled())
		// Original Response still true (deep copy)
		assert.True(original.Validate.ResponseEnabled())
	})

	t.Run("invalid validate bool ignored", func(t *testing.T) {
		on := true
		original := &config.ServiceConfig{
			Validate: &config.ValidateConfig{Request: &on},
		}
		result := applyOverrides(original, []configOverride{
			{key: headerValidateRequest, value: "garbage"},
		})
		assert.True(result.Validate.RequestEnabled())
	})

	t.Run("overrides Validate-Verbose true", func(t *testing.T) {
		original := &config.ServiceConfig{}
		result := applyOverrides(original, []configOverride{
			{key: headerValidateVerbose, value: "true"},
		})
		assert.NotNil(result.Validate)
		assert.True(result.Validate.VerboseEnabled())
		assert.Nil(original.Validate)
	})

	t.Run("overrides Validate-Timeout with parseable duration", func(t *testing.T) {
		original := &config.ServiceConfig{}
		result := applyOverrides(original, []configOverride{
			{key: headerValidateTimeout, value: "5s"},
		})
		assert.NotNil(result.Validate)
		assert.Equal(5*time.Second, result.Validate.TimeoutOrDefault())
		assert.Nil(original.Validate)
	})

	t.Run("Validate-Timeout with unparseable value falls back to default", func(t *testing.T) {
		original := &config.ServiceConfig{}
		result := applyOverrides(original, []configOverride{
			{key: headerValidateTimeout, value: "not-a-duration"},
		})
		// applyOverrides initialises Validate eagerly even when the
		// override doesn't take, so the field exists but Timeout stays
		// nil and TimeoutOrDefault returns the package default.
		if result.Validate != nil {
			assert.Nil(result.Validate.Timeout)
		}
		assert.Equal(config.DefaultValidationTimeout, result.Validate.TimeoutOrDefault())
	})

	t.Run("Validate-Timeout zero falls back to default (treated as unset)", func(t *testing.T) {
		original := &config.ServiceConfig{}
		result := applyOverrides(original, []configOverride{
			{key: headerValidateTimeout, value: "0s"},
		})
		assert.Equal(config.DefaultValidationTimeout, result.Validate.TimeoutOrDefault())
	})
}

func TestCreateConfigOverrideMiddleware(t *testing.T) {
	assert := assert2.New(t)

	t.Run("no headers passes through unchanged", func(t *testing.T) {
		original := &config.ServiceConfig{
			Name:           "test",
			BehaviorConfig: config.BehaviorConfig{Latency: 100 * time.Millisecond},
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
			Name:           "test",
			BehaviorConfig: config.BehaviorConfig{Latency: 100 * time.Millisecond},
		}
		params := newTestParams(original)

		var capturedLatency time.Duration
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedLatency = params.GetServiceConfig(r).Latency
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("X-Mockzilla-Latency", "500ms")
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
			Name:           "test",
			BehaviorConfig: config.BehaviorConfig{Latency: 100 * time.Millisecond},
		}
		params := newTestParams(original)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("X-Mockzilla-Latency", "500ms")
		w := NewBufferedResponseWriter()

		mw := CreateConfigOverrideMiddleware(params)
		mw(handler).ServeHTTP(w, req)

		// Original never mutated
		assert.Equal(100*time.Millisecond, original.Latency)
	})

	t.Run("X-Mockzilla headers are preserved on request", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{Name: "test"})

		var capturedHeaders http.Header
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedHeaders = r.Header.Clone()
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("X-Mockzilla-Latency", "200ms")
		req.Header.Set("X-Mockzilla-Context", "eyJmb28iOiJiYXIifQ==")
		w := NewBufferedResponseWriter()

		mw := CreateConfigOverrideMiddleware(params)
		mw(handler).ServeHTTP(w, req)

		assert.Equal("200ms", capturedHeaders.Get("X-Mockzilla-Latency"))
		assert.Equal("eyJmb28iOiJiYXIifQ==", capturedHeaders.Get("X-Mockzilla-Context"))
	})

	t.Run("browser headers reach the handler", func(t *testing.T) {
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
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		w := NewBufferedResponseWriter()

		mw := CreateConfigOverrideMiddleware(params)
		mw(handler).ServeHTTP(w, req)

		// A service whose spec declares one has to be able to read it.
		assert.Equal("Bearer 123", capturedHeaders.Get("Authorization"))
		assert.Equal("application/json", capturedHeaders.Get("Content-Type"))
		assert.Equal("http://localhost:2200", capturedHeaders.Get("Origin"))
		assert.Equal("en-US,en;q=0.9", capturedHeaders.Get("Accept-Language"))
	})

	t.Run("browser headers stay out of history", func(t *testing.T) {
		h := http.Header{}
		h.Set("Content-Type", "application/json")
		h.Set("Authorization", "Bearer 123")
		h.Set("Accept-Language", "en-US,en;q=0.9")
		h.Set("Sec-Fetch-Mode", "cors")
		h.Set("Origin", "http://localhost:2200")

		kept := strings.Join(historyHeaders(h), "\n")

		assert.Contains(kept, "Content-Type")
		assert.NotContains(kept, "Authorization")
		assert.NotContains(kept, "Accept-Language")
		assert.NotContains(kept, "Sec-Fetch-Mode")
		assert.NotContains(kept, "Origin")
	})

	// The reason for an allow-list: anything unrecognised is dropped, so a
	// header AWS adds next year is out without anyone changing this file.
	t.Run("an unknown platform header is dropped without being named", func(t *testing.T) {
		h := http.Header{}
		h.Set("Content-Type", "application/json")
		h.Set("X-Some-Future-Aws-Header", "internal state")

		kept := strings.Join(historyHeaders(h), "\n")

		assert.Contains(kept, "Content-Type")
		assert.NotContains(kept, "X-Some-Future-Aws-Header")
	})

	t.Run("platform headers stay out of history", func(t *testing.T) {
		h := http.Header{}
		h.Set("Content-Type", "application/json")
		h.Set("X-Amzn-Request-Context", `{"accountId":"1","apiId":"x"}`)
		h.Set("X-Amzn-Lambda-Context", `{"awsRequestId":"1"}`)
		h.Set("X-Amzn-Trace-Id", "Root=1-abc")
		h.Set("X-Amz-Cf-Id", "abc==")
		h.Set("Cloudfront-Viewer-Country", "DE")
		h.Set("Cloudfront-Viewer-Time-Zone", "Europe/Berlin")
		h.Set("X-Forwarded-For", "1.2.3.4")
		h.Set("Via", "2.0 cloudfront")

		kept := strings.Join(historyHeaders(h), "\n")

		assert.Contains(kept, "Content-Type")
		assert.NotContains(kept, "X-Amzn-")
		assert.NotContains(kept, "X-Amz-Cf-Id")
		assert.NotContains(kept, "Cloudfront-")
		assert.NotContains(kept, "X-Forwarded-For")
		assert.NotContains(kept, "Via")
	})

	// These are the caller's and they change the response, so a history entry
	// that hides them cannot explain itself.
	t.Run("mockzilla control headers stay in history", func(t *testing.T) {
		h := http.Header{}
		h.Set("X-Mockzilla-Latency", "1s")
		h.Set("X-Mockzilla-Cache-Requests", "false")

		kept := strings.Join(historyHeaders(h), "\n")

		assert.Contains(kept, "X-Mockzilla-Latency")
		assert.Contains(kept, "X-Mockzilla-Cache-Requests")
	})

	t.Run("browser headers are not forwarded upstream", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		req.Header.Set("Sec-Fetch-Mode", "cors")

		cleanUpstreamHeaders(req)

		assert.Equal("application/json", req.Header.Get("Content-Type"))
		assert.Empty(req.Header.Get("Accept-Language"))
		assert.Empty(req.Header.Get("Sec-Fetch-Mode"))
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
		req.Header.Set("X-Mockzilla-Source", "ui")
		req.Header.Set("Origin", "http://localhost:2200")
		w := NewBufferedResponseWriter()

		mw := CreateConfigOverrideMiddleware(params)
		mw(handler).ServeHTTP(w, req)

		// The UI's own credential is not the target API's, so it does not reach it.
		// Everything else the browser sent does; it is only noise to history.
		assert.Empty(capturedHeaders.Get("Authorization"))
		assert.Equal("http://localhost:2200", capturedHeaders.Get("Origin"))
		assert.Equal("application/json", capturedHeaders.Get("Content-Type"))
		assert.Equal("ui", capturedHeaders.Get("X-Mockzilla-Source"))
	})

	t.Run("multiple overrides applied", func(t *testing.T) {
		original := &config.ServiceConfig{
			Name:           "test",
			BehaviorConfig: config.BehaviorConfig{Latency: 100 * time.Millisecond},
			Cache:          &config.CacheConfig{Requests: true},
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
		req.Header.Set("X-Mockzilla-Latency", "200ms")
		req.Header.Set("X-Mockzilla-Cache-Requests", "false")
		w := NewBufferedResponseWriter()

		mw := CreateConfigOverrideMiddleware(params)
		mw(handler).ServeHTTP(w, req)

		assert.Equal(200*time.Millisecond, captured.latency)
		assert.False(captured.cacheRequests)
	})
}

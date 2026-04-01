package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mockzilla/connexions/v2/pkg/config"
	assert2 "github.com/stretchr/testify/assert"
)

func TestCreateReplayWriteMiddleware(t *testing.T) {
	assert := assert2.New(t)

	t.Run("no header passes through", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/foo": {"POST": {Match: &config.ReplayMatch{Body: []string{"name"}}}},
					},
				},
			},
		})
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("ok"))
		})
		mw := CreateReplayWriteMiddleware(params)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", strings.NewReader(`{"name":"test"}`))
		// No header
		mw(handler).ServeHTTP(w, req)

		assert.Equal("ok", string(w.buf))
		// Nothing recorded
		data := params.DB().Table("replay").Data(context.TODO())
		assert.Empty(data)
	})

	t.Run("nil config passes through", func(t *testing.T) {
		params := newTestParams(nil)
		params.serviceConfig = nil
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("ok"))
		})
		mw := CreateReplayWriteMiddleware(params)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodPost, "/test/foo", strings.NewReader(`{"name":"test"}`))
		req.Header.Set(headerReplayMatch, "name")
		mw(handler).ServeHTTP(w, req)

		assert.Equal("ok", string(w.buf))
	})

	t.Run("records response and writes through", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					TTL: 1 * time.Hour,
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/foo": {"POST": {Match: &config.ReplayMatch{Body: []string{"name"}}}},
					},
				},
			},
		})

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set(ResponseHeaderSource, ResponseHeaderSourceGenerated)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		})
		mw := CreateReplayWriteMiddleware(params)

		w := httptest.NewRecorder()
		body := `{"name":"Jane"}`
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", strings.NewReader(body))
		req.Header.Set(headerReplayMatch, "") // empty → fall back to config
		mw(handler).ServeHTTP(w, req)
		waitForAsync()

		// Verify write-through
		assert.Equal(http.StatusCreated, w.Code)
		assert.Equal(`{"id":1}`, w.Body.String())

		// Verify recorded
		key := buildReplayKey(httptest.NewRequest(http.MethodPost, "/foo", nil), "/foo", "/foo", &config.ReplayMatch{Body: []string{"name"}}, []byte(body))
		val, exists := params.DB().Table("replay").Get(context.TODO(), key)
		assert.True(exists)

		rec := val.(*ReplayRecord)
		assert.Equal([]byte(`{"id":1}`), rec.Data)
		assert.Equal(http.StatusCreated, rec.StatusCode)
		assert.Equal("application/json", rec.ContentType)
		assert.Equal(map[string]any{"body:name": "Jane"}, rec.MatchValues)
	})

	t.Run("header-only without config records with actual path", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{
			Name: "svc",
		})

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set(ResponseHeaderSource, ResponseHeaderSourceGenerated)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		})
		mw := CreateReplayWriteMiddleware(params)

		w := httptest.NewRecorder()
		body := `{"name":"Jane"}`
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", strings.NewReader(body))
		req.Header.Set(headerReplayMatch, "name")
		mw(handler).ServeHTTP(w, req)
		waitForAsync()

		// Verify recorded with actual path
		key := buildReplayKey(httptest.NewRequest(http.MethodPost, "/foo", nil), "/foo", "/foo", &config.ReplayMatch{Body: []string{"name"}}, []byte(body))
		_, exists := params.DB().Table("replay").Get(context.TODO(), key)
		assert.True(exists)
	})

	t.Run("skips duplicate recording", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/foo": {"POST": {Match: &config.ReplayMatch{Body: []string{"name"}}}},
					},
				},
			},
		})

		body := []byte(`{"name":"Jane"}`)
		key := buildReplayKey(httptest.NewRequest(http.MethodPost, "/foo", nil), "/foo", "/foo", &config.ReplayMatch{Body: []string{"name"}}, body)

		// Pre-store
		existing := &ReplayRecord{
			Data:       []byte(`{"original":true}`),
			StatusCode: http.StatusOK,
			CreatedAt:  time.Now(),
		}
		params.DB().Table("replay").Set(context.TODO(), key, existing, 0)

		handlerCalled := false
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"new":true}`))
		})
		mw := CreateReplayWriteMiddleware(params)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", strings.NewReader(`{"name":"Jane"}`))
		req.Header.Set(headerReplayMatch, "") // empty → config
		mw(handler).ServeHTTP(w, req)
		waitForAsync()

		// Handler should still be called (pass through)
		assert.True(handlerCalled)

		// But the stored record should not be overwritten
		val, _ := params.DB().Table("replay").Get(context.TODO(), key)
		rec := val.(*ReplayRecord)
		assert.Equal([]byte(`{"original":true}`), rec.Data)
	})

	t.Run("skips recording cache source responses", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/foo": {"POST": {Match: &config.ReplayMatch{Body: []string{"name"}}}},
					},
				},
			},
		})

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(ResponseHeaderSource, ResponseHeaderSourceCache)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"cached":true}`))
		})
		mw := CreateReplayWriteMiddleware(params)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", strings.NewReader(`{"name":"Jane"}`))
		req.Header.Set(headerReplayMatch, "name")
		mw(handler).ServeHTTP(w, req)
		waitForAsync()

		// Should write through
		assert.Equal(`{"cached":true}`, w.Body.String())

		// Should NOT be recorded
		body := []byte(`{"name":"Jane"}`)
		key := buildReplayKey(httptest.NewRequest(http.MethodPost, "/foo", nil), "/foo", "/foo", &config.ReplayMatch{Body: []string{"name"}}, body)
		_, exists := params.DB().Table("replay").Get(context.TODO(), key)
		assert.False(exists)
	})

	t.Run("upstream-only returns error for non-upstream responses", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					UpstreamOnly: true,
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/foo": {"POST": {Match: &config.ReplayMatch{Body: []string{"name"}}}},
					},
				},
			},
		})

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(ResponseHeaderSource, ResponseHeaderSourceGenerated)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"generated":true}`))
		})
		mw := CreateReplayWriteMiddleware(params)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", strings.NewReader(`{"name":"Jane"}`))
		req.Header.Set(headerReplayMatch, "") // empty → config
		mw(handler).ServeHTTP(w, req)
		waitForAsync()

		// Should return 502 with error message
		assert.Equal(http.StatusBadGateway, w.Code)
		assert.Contains(w.Body.String(), "upstream-only is configured but response source is generated")
		assert.Equal(ResponseHeaderSourceGenerated, w.Header().Get(ResponseHeaderSource))

		// Should NOT be recorded
		body := []byte(`{"name":"Jane"}`)
		key := buildReplayKey(httptest.NewRequest(http.MethodPost, "/foo", nil), "/foo", "/foo", &config.ReplayMatch{Body: []string{"name"}}, body)
		_, exists := params.DB().Table("replay").Get(context.TODO(), key)
		assert.False(exists)
	})

	t.Run("upstream-only records upstream responses", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					UpstreamOnly: true,
					TTL:          1 * time.Hour,
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/foo": {"POST": {Match: &config.ReplayMatch{Body: []string{"name"}}}},
					},
				},
			},
		})

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(ResponseHeaderSource, ResponseHeaderSourceUpstream)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"upstream":true}`))
		})
		mw := CreateReplayWriteMiddleware(params)

		w := httptest.NewRecorder()
		body := `{"name":"Jane"}`
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", strings.NewReader(body))
		req.Header.Set(headerReplayMatch, "") // empty → config
		mw(handler).ServeHTTP(w, req)
		waitForAsync()

		// Should be recorded
		key := buildReplayKey(httptest.NewRequest(http.MethodPost, "/foo", nil), "/foo", "/foo", &config.ReplayMatch{Body: []string{"name"}}, []byte(body))
		val, exists := params.DB().Table("replay").Get(context.TODO(), key)
		assert.True(exists)

		rec := val.(*ReplayRecord)
		assert.True(rec.IsFromUpstream)
		assert.Equal([]byte(`{"upstream":true}`), rec.Data)
	})

	t.Run("TTL is applied to stored record", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					TTL: 50 * time.Millisecond,
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/foo": {"POST": {Match: &config.ReplayMatch{Body: []string{"name"}}}},
					},
				},
			},
		})

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(ResponseHeaderSource, ResponseHeaderSourceGenerated)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		})
		mw := CreateReplayWriteMiddleware(params)

		w := httptest.NewRecorder()
		body := `{"name":"Jane"}`
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", strings.NewReader(body))
		req.Header.Set(headerReplayMatch, "") // empty → config
		mw(handler).ServeHTTP(w, req)
		waitForAsync()

		key := buildReplayKey(httptest.NewRequest(http.MethodPost, "/foo", nil), "/foo", "/foo", &config.ReplayMatch{Body: []string{"name"}}, []byte(body))

		// Should exist immediately
		_, exists := params.DB().Table("replay").Get(context.TODO(), key)
		assert.True(exists)

		// Should expire after TTL
		time.Sleep(100 * time.Millisecond)
		_, exists = params.DB().Table("replay").Get(context.TODO(), key)
		assert.False(exists)
	})

	t.Run("auto-replay records without header", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					AutoReplay: true,
					TTL:        1 * time.Hour,
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/foo": {"POST": {Match: &config.ReplayMatch{Body: []string{"name"}}}},
					},
				},
			},
		})

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set(ResponseHeaderSource, ResponseHeaderSourceGenerated)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"auto":true}`))
		})
		mw := CreateReplayWriteMiddleware(params)

		w := httptest.NewRecorder()
		body := `{"name":"Jane"}`
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", strings.NewReader(body))
		// No header - auto-replay should activate
		mw(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal(`{"auto":true}`, w.Body.String())

		key := buildReplayKey(httptest.NewRequest(http.MethodPost, "/foo", nil), "/foo", "/foo", &config.ReplayMatch{Body: []string{"name"}}, []byte(body))
		val, exists := params.DB().Table("replay").Get(context.TODO(), key)
		assert.True(exists)

		rec := val.(*ReplayRecord)
		assert.Equal([]byte(`{"auto":true}`), rec.Data)
	})

	t.Run("missing match field skips recording", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/foo": {"POST": {Match: &config.ReplayMatch{Body: []string{"name", "missing_field"}}}},
					},
				},
			},
		})

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(ResponseHeaderSource, ResponseHeaderSourceGenerated)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		})
		mw := CreateReplayWriteMiddleware(params)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", strings.NewReader(`{"name":"Jane"}`))
		req.Header.Set(headerReplayMatch, "") // empty → config
		mw(handler).ServeHTTP(w, req)
		waitForAsync()

		// Response should be written through
		assert.Equal(`{"ok":true}`, w.Body.String())

		// Nothing recorded
		data := params.DB().Table("replay").Data(context.TODO())
		assert.Empty(data)
	})

	t.Run("records with path variable match values", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					TTL: 1 * time.Hour,
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/pay/{paymentMethod}": {"POST": {Match: &config.ReplayMatch{Path: []string{"paymentMethod"}, Body: []string{"ref"}}}},
					},
				},
			},
		})

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set(ResponseHeaderSource, ResponseHeaderSourceGenerated)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		})
		mw := CreateReplayWriteMiddleware(params)

		w := httptest.NewRecorder()
		body := `{"ref":"REF123"}`
		req := httptest.NewRequest(http.MethodPost, "/svc/pay/credit-card", strings.NewReader(body))
		req.Header.Set(headerReplayMatch, "")
		mw(handler).ServeHTTP(w, req)
		waitForAsync()

		// Verify recorded with path variable in key
		match := &config.ReplayMatch{Path: []string{"paymentMethod"}, Body: []string{"ref"}}
		key := buildReplayKey(httptest.NewRequest(http.MethodPost, "/pay/credit-card", nil), "/pay/{paymentMethod}", "/pay/credit-card", match, []byte(body))
		val, exists := params.DB().Table("replay").Get(context.TODO(), key)
		assert.True(exists)

		rec := val.(*ReplayRecord)
		assert.Equal(map[string]any{"path:paymentMethod": "credit-card", "body:ref": "REF123"}, rec.MatchValues)
	})

	t.Run("records with query match values", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					TTL: 1 * time.Hour,
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/search": {"GET": {Match: &config.ReplayMatch{Query: []string{"q"}}}},
					},
				},
			},
		})

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set(ResponseHeaderSource, ResponseHeaderSourceGenerated)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"results":[]}`))
		})
		mw := CreateReplayWriteMiddleware(params)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/svc/search?q=test", nil)
		req.Header.Set(headerReplayMatch, "")
		mw(handler).ServeHTTP(w, req)
		waitForAsync()

		key := buildReplayKey(httptest.NewRequest(http.MethodGet, "/search?q=test", nil), "/search", "/search", &config.ReplayMatch{Query: []string{"q"}}, nil)
		val, exists := params.DB().Table("replay").Get(context.TODO(), key)
		assert.True(exists)

		rec := val.(*ReplayRecord)
		assert.Equal(map[string]any{"query:q": "test"}, rec.MatchValues)
	})

	t.Run("auto-replay skips non-configured endpoints", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					AutoReplay: true,
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/foo": {"POST": {Match: &config.ReplayMatch{Body: []string{"name"}}}},
					},
				},
			},
		})

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		})
		mw := CreateReplayWriteMiddleware(params)

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/svc/bar", strings.NewReader(`{"name":"Jane"}`))
		// No header, /bar not configured → pass through, no recording
		mw(handler).ServeHTTP(w, req)
		waitForAsync()

		data := params.DB().Table("replay").Data(context.TODO())
		assert.Empty(data)
	})
}

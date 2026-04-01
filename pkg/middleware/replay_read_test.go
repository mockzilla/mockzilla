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

func TestCreateReplayReadMiddleware(t *testing.T) {
	assert := assert2.New(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fresh"))
	})

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
		mw := CreateReplayReadMiddleware(params)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", strings.NewReader(`{"name":"test"}`))
		// No X-Cxs-Replay header
		mw(handler).ServeHTTP(w, req)

		assert.Equal("fresh", string(w.buf))
	})

	t.Run("nil config passes through", func(t *testing.T) {
		params := newTestParams(nil)
		params.serviceConfig = nil
		mw := CreateReplayReadMiddleware(params)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodPost, "/test/foo", strings.NewReader(`{"name":"test"}`))
		req.Header.Set(headerReplayMatch, "name")
		mw(handler).ServeHTTP(w, req)

		assert.Equal("fresh", string(w.buf))
	})

	t.Run("header with fields but no config works with actual path", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{
			Name: "svc",
		})

		// Store using actual endpoint path (no config pattern)
		body := []byte(`{"name":"Jane"}`)
		key := buildReplayKey(httptest.NewRequest(http.MethodPost, "/foo", nil), "/foo", "/foo", &config.ReplayMatch{Body: []string{"name"}}, body)
		rec := &ReplayRecord{
			Data:        []byte(`{"result":"no-config"}`),
			StatusCode:  http.StatusOK,
			ContentType: "application/json",
			CreatedAt:   time.Now(),
		}
		params.DB().Table("replay").Set(context.TODO(), key, rec, 0)

		mw := CreateReplayReadMiddleware(params)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", strings.NewReader(`{"name":"Jane"}`))
		req.Header.Set(headerReplayMatch, "name")
		mw(handler).ServeHTTP(w, req)

		assert.Equal(`{"result":"no-config"}`, string(w.buf))
		assert.Equal(ResponseHeaderSourceReplay, w.header.Get(ResponseHeaderSource))
	})

	t.Run("miss passes through", func(t *testing.T) {
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
		mw := CreateReplayReadMiddleware(params)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", strings.NewReader(`{"name":"test"}`))
		req.Header.Set(headerReplayMatch, "name")
		mw(handler).ServeHTTP(w, req)

		assert.Equal("fresh", string(w.buf))
	})

	t.Run("hit returns stored response", func(t *testing.T) {
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

		// Pre-store a replay record using config pattern path
		body := []byte(`{"name":"Jane"}`)
		key := buildReplayKey(httptest.NewRequest(http.MethodPost, "/foo", nil), "/foo", "/foo", &config.ReplayMatch{Body: []string{"name"}}, body)
		rec := &ReplayRecord{
			Data:        []byte(`{"result":"stored"}`),
			Headers:     map[string]string{"X-Custom": "value"},
			StatusCode:  http.StatusOK,
			ContentType: "application/json",
			CreatedAt:   time.Now(),
		}
		params.DB().Table("replay").Set(context.TODO(), key, rec, 0)

		mw := CreateReplayReadMiddleware(params)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", strings.NewReader(`{"name":"Jane"}`))
		req.Header.Set(headerReplayMatch, "") // empty value → fall back to config
		mw(handler).ServeHTTP(w, req)

		assert.Equal(`{"result":"stored"}`, string(w.buf))
		assert.Equal("application/json", w.header.Get("Content-Type"))
		assert.Equal(ResponseHeaderSourceReplay, w.header.Get(ResponseHeaderSource))
		assert.Equal("value", w.header.Get("X-Custom"))
	})

	t.Run("header value overrides config match fields", func(t *testing.T) {
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

		// Store with override fields but using config pattern path
		body := []byte(`{"name":"Jane","age":30}`)
		key := buildReplayKey(httptest.NewRequest(http.MethodPost, "/foo", nil), "/foo", "/foo", &config.ReplayMatch{Body: []string{"age"}}, body)
		rec := &ReplayRecord{
			Data:        []byte(`{"result":"header-override"}`),
			StatusCode:  http.StatusOK,
			ContentType: "application/json",
			CreatedAt:   time.Now(),
		}
		params.DB().Table("replay").Set(context.TODO(), key, rec, 0)

		mw := CreateReplayReadMiddleware(params)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", strings.NewReader(`{"name":"Jane","age":30}`))
		req.Header.Set(headerReplayMatch, "age")
		mw(handler).ServeHTTP(w, req)

		assert.Equal(`{"result":"header-override"}`, string(w.buf))
		assert.Equal(ResponseHeaderSourceReplay, w.header.Get(ResponseHeaderSource))
	})

	t.Run("empty header value with no config passes through", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{Name: "svc"})
		mw := CreateReplayReadMiddleware(params)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", strings.NewReader(`{"name":"test"}`))
		req.Header.Set(headerReplayMatch, "") // empty, no config → no match fields
		mw(handler).ServeHTTP(w, req)

		assert.Equal("fresh", string(w.buf))
	})

	t.Run("auto-replay activates without header", func(t *testing.T) {
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

		body := []byte(`{"name":"Jane"}`)
		key := buildReplayKey(httptest.NewRequest(http.MethodPost, "/foo", nil), "/foo", "/foo", &config.ReplayMatch{Body: []string{"name"}}, body)
		rec := &ReplayRecord{
			Data:        []byte(`{"result":"auto"}`),
			StatusCode:  http.StatusOK,
			ContentType: "application/json",
			CreatedAt:   time.Now(),
		}
		params.DB().Table("replay").Set(context.TODO(), key, rec, 0)

		mw := CreateReplayReadMiddleware(params)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", strings.NewReader(`{"name":"Jane"}`))
		// No header - auto-replay should activate
		mw(handler).ServeHTTP(w, req)

		assert.Equal(`{"result":"auto"}`, string(w.buf))
		assert.Equal(ResponseHeaderSourceReplay, w.header.Get(ResponseHeaderSource))
	})

	t.Run("missing match field passes through", func(t *testing.T) {
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
		mw := CreateReplayReadMiddleware(params)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", strings.NewReader(`{"name":"Jane"}`))
		req.Header.Set(headerReplayMatch, "") // empty → fall back to config
		mw(handler).ServeHTTP(w, req)

		assert.Equal("fresh", string(w.buf))
	})

	t.Run("corrupted record passes through", func(t *testing.T) {
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

		// Store a non-deserializable value
		body := []byte(`{"name":"Jane"}`)
		key := buildReplayKey(httptest.NewRequest(http.MethodPost, "/foo", nil), "/foo", "/foo", &config.ReplayMatch{Body: []string{"name"}}, body)
		params.DB().Table("replay").Set(context.TODO(), key, "not a record", 0)

		mw := CreateReplayReadMiddleware(params)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", strings.NewReader(`{"name":"Jane"}`))
		req.Header.Set(headerReplayMatch, "")
		mw(handler).ServeHTTP(w, req)

		assert.Equal("fresh", string(w.buf))
	})

	t.Run("path variable matching produces different recordings", func(t *testing.T) {
		params := newTestParams(&config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					AutoReplay: true,
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/pay/{paymentMethod}": {"POST": {Match: &config.ReplayMatch{Path: []string{"paymentMethod"}, Body: []string{"ref"}}}},
					},
				},
			},
		})

		// Store recording for credit-card
		body := []byte(`{"ref":"REF123"}`)
		match := &config.ReplayMatch{Path: []string{"paymentMethod"}, Body: []string{"ref"}}
		keyCreditCard := buildReplayKey(httptest.NewRequest(http.MethodPost, "/pay/credit-card", nil), "/pay/{paymentMethod}", "/pay/credit-card", match, body)
		params.DB().Table("replay").Set(context.TODO(), keyCreditCard, &ReplayRecord{
			Data:        []byte(`{"result":"credit-card"}`),
			StatusCode:  http.StatusOK,
			ContentType: "application/json",
			CreatedAt:   time.Now(),
		}, 0)

		// Store recording for bank-transfer
		keyBankTransfer := buildReplayKey(httptest.NewRequest(http.MethodPost, "/pay/bank-transfer", nil), "/pay/{paymentMethod}", "/pay/bank-transfer", match, body)
		params.DB().Table("replay").Set(context.TODO(), keyBankTransfer, &ReplayRecord{
			Data:        []byte(`{"result":"bank-transfer"}`),
			StatusCode:  http.StatusOK,
			ContentType: "application/json",
			CreatedAt:   time.Now(),
		}, 0)

		mw := CreateReplayReadMiddleware(params)

		// Request credit-card - should get credit-card recording
		w1 := NewBufferedResponseWriter()
		req1 := httptest.NewRequest(http.MethodPost, "/svc/pay/credit-card", strings.NewReader(`{"ref":"REF123"}`))
		mw(handler).ServeHTTP(w1, req1)
		assert.Equal(`{"result":"credit-card"}`, string(w1.buf))

		// Request bank-transfer - should get bank-transfer recording
		w2 := NewBufferedResponseWriter()
		req2 := httptest.NewRequest(http.MethodPost, "/svc/pay/bank-transfer", strings.NewReader(`{"ref":"REF123"}`))
		mw(handler).ServeHTTP(w2, req2)
		assert.Equal(`{"result":"bank-transfer"}`, string(w2.buf))
	})

	t.Run("hit increments hit count and last replayed time", func(t *testing.T) {
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
		rec := &ReplayRecord{
			Data:        []byte(`{"result":"stored"}`),
			StatusCode:  http.StatusOK,
			ContentType: "application/json",
			CreatedAt:   time.Now(),
		}
		params.DB().Table("replay").Set(context.TODO(), key, rec, 0)

		mw := CreateReplayReadMiddleware(params)

		// First hit
		w1 := NewBufferedResponseWriter()
		req1 := httptest.NewRequest(http.MethodPost, "/svc/foo", strings.NewReader(`{"name":"Jane"}`))
		req1.Header.Set(headerReplayMatch, "")
		mw(handler).ServeHTTP(w1, req1)

		val, _ := params.DB().Table("replay").Get(context.TODO(), key)
		updated := val.(*ReplayRecord)
		assert.Equal(1, updated.HitCount)
		assert.False(updated.LastReplayedAt.IsZero())
		firstReplayedAt := updated.LastReplayedAt

		// Second hit
		w2 := NewBufferedResponseWriter()
		req2 := httptest.NewRequest(http.MethodPost, "/svc/foo", strings.NewReader(`{"name":"Jane"}`))
		req2.Header.Set(headerReplayMatch, "")
		mw(handler).ServeHTTP(w2, req2)

		val, _ = params.DB().Table("replay").Get(context.TODO(), key)
		updated = val.(*ReplayRecord)
		assert.Equal(2, updated.HitCount)
		assert.True(updated.LastReplayedAt.Equal(firstReplayedAt) || updated.LastReplayedAt.After(firstReplayedAt))
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
		mw := CreateReplayReadMiddleware(params)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodPost, "/svc/bar", strings.NewReader(`{"name":"Jane"}`))
		// No header, /bar not configured → pass through
		mw(handler).ServeHTTP(w, req)

		assert.Equal("fresh", string(w.buf))
	})
}

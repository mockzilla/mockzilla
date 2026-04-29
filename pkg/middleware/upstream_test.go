package middleware

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
	"github.com/mockzilla/mockzilla/v2/pkg/db"
	assert2 "github.com/stretchr/testify/assert"
)

func TestCreateUpstreamRequestMiddleware(t *testing.T) {
	assert := assert2.New(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Hello, from local!"))
	})

	t.Run("upstream service response is used if present", func(t *testing.T) {
		var receivedHeaders http.Header
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedHeaders = r.Header.Clone()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"message": "Hello, from remote!"}`))
		}))
		defer upstreamServer.Close()

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test/foo", nil)
		req.Header.Set("Authorization", "Bearer 123")
		req.Header.Set("X-Test", "test")

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL: upstreamServer.URL,
			},
		})

		f := CreateUpstreamRequestMiddleware(params)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal(`{"message": "Hello, from remote!"}`, string(w.buf))

		// Check that headers were forwarded to upstream
		assert.Equal("Bearer 123", receivedHeaders.Get("Authorization"))
		assert.Equal("test", receivedHeaders.Get("X-Test"))
		assert.Equal("Mockzilla/2.0", receivedHeaders.Get("User-Agent"))

		// Check history
		data := params.DB().History().Data(context.Background())
		assert.Equal(1, len(data))
		rec := data[0]
		assert.Equal(200, rec.Response.StatusCode)
		assert.Equal([]byte(`{"message": "Hello, from remote!"}`), rec.Response.Body)
	})

	t.Run("upstream non-200 success status code is preserved", func(t *testing.T) {
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id": "new-resource"}`))
		}))
		defer upstreamServer.Close()

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodPost, "/test/resource", nil)

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL: upstreamServer.URL,
			},
		})

		f := CreateUpstreamRequestMiddleware(params)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal(`{"id": "new-resource"}`, string(w.buf))
		assert.Equal(http.StatusCreated, w.statusCode)

		// Check history preserves status code
		data := params.DB().History().Data(context.Background())
		assert.Equal(1, len(data))
		rec := data[0]
		assert.Equal(http.StatusCreated, rec.Response.StatusCode)
	})

	t.Run("X-Mz headers are stripped before forwarding to upstream", func(t *testing.T) {
		var receivedHeaders http.Header
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedHeaders = r.Header.Clone()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer upstreamServer.Close()

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test/foo", nil)
		req.Header.Set("Authorization", "Bearer 123")
		req.Header.Set("X-Mz-Latency", "200ms")
		req.Header.Set("X-Mz-Context", "eyJmb28iOiJiYXIifQ==")

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL: upstreamServer.URL,
			},
		})

		f := CreateUpstreamRequestMiddleware(params)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal("Bearer 123", receivedHeaders.Get("Authorization"))
		assert.Empty(receivedHeaders.Get("X-Mz-Latency"))
		assert.Empty(receivedHeaders.Get("X-Mz-Context"))
	})

	t.Run("X-Mz-Upstream-Headers allowlist keeps only listed headers for upstream", func(t *testing.T) {
		var receivedHeaders http.Header
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedHeaders = r.Header.Clone()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer upstreamServer.Close()

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test/foo", nil)
		req.Header.Set("Authorization", "Basic internal-creds")
		req.Header.Set("Smartum-Version", "2020-04-02")
		req.Header.Set("X-Custom", "keep-me")
		req.Header.Set("Cookie", "session=abc")
		req.Header.Set("X-Mz-Upstream-Headers", "Smartum-Version,X-Custom")

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL: upstreamServer.URL,
			},
		})

		f := CreateUpstreamRequestMiddleware(params)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal("2020-04-02", receivedHeaders.Get("Smartum-Version"))
		assert.Equal("keep-me", receivedHeaders.Get("X-Custom"))
		assert.Empty(receivedHeaders.Get("Authorization"))
		assert.Empty(receivedHeaders.Get("Cookie"))
		assert.Empty(receivedHeaders.Get("X-Mz-Upstream-Headers"))
	})

	t.Run("query parameters are forwarded to upstream", func(t *testing.T) {
		var receivedURL string
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedURL = r.URL.String()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"message": "OK"}`))
		}))
		defer upstreamServer.Close()

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test/payment/charge?reference=abc-123&amount=1000", nil)

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL: upstreamServer.URL,
			},
		})

		f := CreateUpstreamRequestMiddleware(params)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal(`{"message": "OK"}`, string(w.buf))
		assert.Equal("/payment/charge?reference=abc-123&amount=1000", receivedURL)
	})

	t.Run("upstream content-type header is forwarded", func(t *testing.T) {
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer upstreamServer.Close()

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test/foo", nil)

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL: upstreamServer.URL,
			},
		})

		f := CreateUpstreamRequestMiddleware(params)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal(`{"ok":true}`, string(w.buf))
		assert.Equal("application/json; charset=utf-8", w.header.Get("Content-Type"))

		// Check history has content-type
		data := params.DB().History().Data(context.Background())
		assert.Equal(1, len(data))
		rec := data[0]
		assert.Equal("application/json; charset=utf-8", rec.Response.ContentType)
	})

	t.Run("gzip upstream response is decompressed", func(t *testing.T) {
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Type", "application/json")
			gz := gzip.NewWriter(w)
			_, _ = gz.Write([]byte(`{"compressed": true}`))
			_ = gz.Close()
		}))
		defer upstreamServer.Close()

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test/gzip", nil)
		req.Header.Set("Accept-Encoding", "gzip")

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL: upstreamServer.URL,
			},
		})

		f := CreateUpstreamRequestMiddleware(params)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal(`{"compressed": true}`, string(w.buf))
	})

	t.Run("configured upstream headers are applied", func(t *testing.T) {
		var receivedHeaders http.Header
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedHeaders = r.Header.Clone()
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer upstreamServer.Close()

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test/headers", nil)
		req.Header.Set("Authorization", "Bearer original")

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL: upstreamServer.URL,
				Headers: map[string]string{
					"Authorization": "Bearer configured",
					"X-Custom":      "custom-value",
				},
			},
		})

		f := CreateUpstreamRequestMiddleware(params)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal(`{"ok":true}`, string(w.buf))
		assert.Equal("Bearer configured", receivedHeaders.Get("Authorization"))
		assert.Equal("custom-value", receivedHeaders.Get("X-Custom"))
	})

	t.Run("sets X-Mz-Source header to upstream", func(t *testing.T) {
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"from":"upstream"}`))
		}))
		defer upstreamServer.Close()

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test/source", nil)

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL: upstreamServer.URL,
			},
		})

		f := CreateUpstreamRequestMiddleware(params)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal(ResponseHeaderSourceUpstream, w.header.Get(ResponseHeaderSource))
	})

	t.Run("history is present", func(t *testing.T) {
		rcvdBody := ""
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rcvdBodyBts, _ := io.ReadAll(r.Body)
			rcvdBody = string(rcvdBodyBts)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"message": "Hello, from remote!"}`))
		}))
		defer upstreamServer.Close()

		w := NewBufferedResponseWriter()
		body := io.NopCloser(strings.NewReader(`{"foo": "bar"}`))
		req := httptest.NewRequest(http.MethodPost, "/foo/resource", body)

		params := newTestParams(&config.ServiceConfig{
			Name: "foo",
			Upstream: &config.UpstreamConfig{
				URL: upstreamServer.URL,
			},
		})

		resp := &db.HistoryResponse{
			Body:           []byte("cached"),
			StatusCode:     http.StatusOK,
			ContentType:    "application/json",
			IsFromUpstream: true,
		}
		params.DB().History().Set(context.Background(), "/foo/resource", &db.HistoryRequest{
			Method: http.MethodPost,
			URL:    "/foo/resource",
			Body:   []byte(`{"bar": "car"}`),
		}, resp)

		f := CreateUpstreamRequestMiddleware(params)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal(`{"message": "Hello, from remote!"}`, string(w.buf))
		assert.Equal(`{"foo": "bar"}`, rcvdBody)

		// Check history - 2 entries: the seeded one + the new upstream result
		data := params.DB().History().Data(context.Background())
		assert.Equal(2, len(data))
		// Latest entry should have the upstream response
		rec := data[len(data)-1]
		assert.Equal(200, rec.Response.StatusCode)
		assert.Equal([]byte(`{"message": "Hello, from remote!"}`), rec.Response.Body)
	})

	t.Run("not called if url is empty", func(t *testing.T) {
		callCount := 0
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"message": "OK"}`))
		}))
		defer upstreamServer.Close()

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/foo", nil)

		params := newTestParams(&config.ServiceConfig{
			Name:     "test",
			Upstream: &config.UpstreamConfig{},
		})

		f := CreateUpstreamRequestMiddleware(params)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal("Hello, from local!", string(w.buf))
		assert.Equal(0, callCount)
	})

	t.Run("upstream service response fails", func(t *testing.T) {
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message": "Internal Server Error!"}`))
		}))

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test/foo", nil)

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL: upstreamServer.URL,
			},
		})

		f := CreateUpstreamRequestMiddleware(params)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal("Hello, from local!", string(w.buf))
	})

	t.Run("request create fails", func(t *testing.T) {
		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test/foo", nil)

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL: "ht tps://example.com",
			},
		})

		f := CreateUpstreamRequestMiddleware(params)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal("Hello, from local!", string(w.buf))
	})

	t.Run("upstream service times out", func(t *testing.T) {
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(200 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"message": "OK"}`))
		}))

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test/foo", nil)

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL:     upstreamServer.URL,
				Timeout: 50 * time.Millisecond,
			},
		})

		f := CreateUpstreamRequestMiddleware(params)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal("Hello, from local!", string(w.buf))
	})

	t.Run("400 returns upstream error by default (fail-on 400-499 except 401,403)", func(t *testing.T) {
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message": "Bad Request"}`))
		}))
		defer upstreamServer.Close()

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test/foo", nil)

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL: upstreamServer.URL,
			},
		})

		f := CreateUpstreamRequestMiddleware(params)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal(`{"message": "Bad Request"}`, string(w.buf))
		assert.Equal(http.StatusBadRequest, w.statusCode)
		assert.Equal("application/json", w.header.Get("Content-Type"))
		assert.Equal(ResponseHeaderSourceUpstream, w.header.Get(ResponseHeaderSource))
	})

	t.Run("401 falls back to generator by default (excepted from 4xx range)", func(t *testing.T) {
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message": "Unauthorized"}`))
		}))
		defer upstreamServer.Close()

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test/foo", nil)

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL: upstreamServer.URL,
			},
		})

		f := CreateUpstreamRequestMiddleware(params)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal("Hello, from local!", string(w.buf))
	})

	t.Run("404 returns upstream error by default (4xx in range, not excepted)", func(t *testing.T) {
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message": "Not Found"}`))
		}))
		defer upstreamServer.Close()

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test/foo", nil)

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL: upstreamServer.URL,
			},
		})

		f := CreateUpstreamRequestMiddleware(params)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal(`{"message": "Not Found"}`, string(w.buf))
		assert.Equal(http.StatusNotFound, w.statusCode)
		assert.Equal(ResponseHeaderSourceUpstream, w.header.Get(ResponseHeaderSource))
	})

	t.Run("403 falls back to generator by default (excepted from 4xx range)", func(t *testing.T) {
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message": "Forbidden"}`))
		}))
		defer upstreamServer.Close()

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test/foo", nil)

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL: upstreamServer.URL,
			},
		})

		f := CreateUpstreamRequestMiddleware(params)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal("Hello, from local!", string(w.buf))
	})

	t.Run("4xx falls back to generator when fail-on is empty", func(t *testing.T) {
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message": "Unauthorized"}`))
		}))
		defer upstreamServer.Close()

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test/foo", nil)

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL:    upstreamServer.URL,
				FailOn: &config.HTTPStatusMatchConfig{},
			},
		})

		f := CreateUpstreamRequestMiddleware(params)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal("Hello, from local!", string(w.buf))
	})

	t.Run("5xx falls back to generator by default", func(t *testing.T) {
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"message": "Bad Gateway"}`))
		}))
		defer upstreamServer.Close()

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test/foo", nil)

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL: upstreamServer.URL,
			},
		})

		f := CreateUpstreamRequestMiddleware(params)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal("Hello, from local!", string(w.buf))
	})

	t.Run("sticky source skips upstream for all clients after generator fallback", func(t *testing.T) {
		upstreamCalls := 0
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			upstreamCalls++
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error": "server down"}`))
		}))
		defer upstreamServer.Close()

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL:           upstreamServer.URL,
				StickyTimeout: 30 * time.Second,
			},
		})

		f := CreateUpstreamRequestMiddleware(params)

		// First request: upstream fails, falls back to generator, sets sticky marker
		w1 := NewBufferedResponseWriter()
		req1 := httptest.NewRequest(http.MethodGet, "/test/foo", nil)
		req1.RemoteAddr = "10.0.0.1:12345"
		f(handler).ServeHTTP(w1, req1)
		waitForAsync()

		assert.Equal(1, upstreamCalls)
		assert.Equal("Hello, from local!", string(w1.buf))

		// Second request from a different client: upstream is skipped (service-wide sticky)
		w2 := NewBufferedResponseWriter()
		req2 := httptest.NewRequest(http.MethodGet, "/test/bar", nil)
		req2.RemoteAddr = "10.0.0.99:9999"
		f(handler).ServeHTTP(w2, req2)
		waitForAsync()

		assert.Equal(1, upstreamCalls) // still 1 - upstream was not called
		assert.Equal("Hello, from local!", string(w2.buf))
	})

	t.Run("sticky source cleared on successful upstream response", func(t *testing.T) {
		callCount := 0
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok": true}`))
		}))
		defer upstreamServer.Close()

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL:           upstreamServer.URL,
				StickyTimeout: 30 * time.Second,
			},
		})

		// Pre-set a sticky marker for this service
		params.DB().Table("sticky_source").Set(context.Background(), "test", "generated", 30*time.Second)

		f := CreateUpstreamRequestMiddleware(params)

		// Verify sticky marker exists
		_, exists := params.DB().Table("sticky_source").Get(context.Background(), "test")
		assert.True(exists)

		// Request hits sticky, goes to generator
		w1 := NewBufferedResponseWriter()
		req1 := httptest.NewRequest(http.MethodGet, "/test/foo", nil)
		f(handler).ServeHTTP(w1, req1)
		waitForAsync()

		assert.Equal(0, callCount) // upstream was skipped
		assert.Equal("Hello, from local!", string(w1.buf))

		// Now clear sticky and verify upstream is called again
		params.DB().Table("sticky_source").Delete(context.Background(), "test")

		w2 := NewBufferedResponseWriter()
		req2 := httptest.NewRequest(http.MethodGet, "/test/foo", nil)
		f(handler).ServeHTTP(w2, req2)
		waitForAsync()

		assert.Equal(1, callCount) // upstream was called
		assert.Equal(`{"ok": true}`, string(w2.buf))

		// Successful response should have cleared the marker
		_, exists = params.DB().Table("sticky_source").Get(context.Background(), "test")
		assert.False(exists)
	})

	t.Run("sticky source not set when fail-on matches", func(t *testing.T) {
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error": "bad request"}`))
		}))
		defer upstreamServer.Close()

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL:           upstreamServer.URL,
				StickyTimeout: 30 * time.Second,
			},
		})

		f := CreateUpstreamRequestMiddleware(params)

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test/foo", nil)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		// fail-on matched (400 is in default range), so error returned directly
		assert.Equal(http.StatusBadRequest, w.statusCode)

		// No sticky marker should be set for the service
		_, exists := params.DB().Table("sticky_source").Get(context.Background(), "test")
		assert.False(exists)
	})

	t.Run("sticky source not used when sticky-timeout is zero", func(t *testing.T) {
		upstreamCalls := 0
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			upstreamCalls++
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error": "fail"}`))
		}))
		defer upstreamServer.Close()

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL: upstreamServer.URL,
			},
		})

		f := CreateUpstreamRequestMiddleware(params)

		w1 := NewBufferedResponseWriter()
		req1 := httptest.NewRequest(http.MethodGet, "/test/foo", nil)
		f(handler).ServeHTTP(w1, req1)
		waitForAsync()

		// Second request - upstream should still be called (no sticky behavior)
		w2 := NewBufferedResponseWriter()
		req2 := httptest.NewRequest(http.MethodGet, "/test/bar", nil)
		f(handler).ServeHTTP(w2, req2)
		waitForAsync()

		assert.Equal(2, upstreamCalls)
	})

	t.Run("custom fail-on includes 5xx", func(t *testing.T) {
		upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"message": "Bad Gateway"}`))
		}))
		defer upstreamServer.Close()

		w := NewBufferedResponseWriter()
		req := httptest.NewRequest(http.MethodGet, "/test/foo", nil)

		params := newTestParams(&config.ServiceConfig{
			Name: "test",
			Upstream: &config.UpstreamConfig{
				URL: upstreamServer.URL,
				FailOn: &config.HTTPStatusMatchConfig{
					{Range: "400-599"},
				},
			},
		})

		f := CreateUpstreamRequestMiddleware(params)
		f(handler).ServeHTTP(w, req)
		waitForAsync()

		assert.Equal(`{"message": "Bad Gateway"}`, string(w.buf))
		assert.Equal(http.StatusBadGateway, w.statusCode)
	})

}

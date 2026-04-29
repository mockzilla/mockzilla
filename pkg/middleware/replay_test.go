package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mockzilla/mockzilla/v2/pkg/config"
	assert2 "github.com/stretchr/testify/assert"
)

func TestParseReplayHeader(t *testing.T) {
	assert := assert2.New(t)

	tests := []struct {
		name     string
		input    string
		expected *config.ReplayMatch
	}{
		{"empty string", "", nil},
		{"single field", "data.name", &config.ReplayMatch{Body: []string{"data.name"}}},
		{"multiple fields", "data.name,data.zip", &config.ReplayMatch{Body: []string{"data.name", "data.zip"}}},
		{"fields with spaces", " data.name , data.zip ", &config.ReplayMatch{Body: []string{"data.name", "data.zip"}}},
		{"trailing comma", "data.name,", &config.ReplayMatch{Body: []string{"data.name"}}},
		{"only commas", ",,", nil},
		{"only spaces", "  ,  ,  ", nil},
		{"path source", "path:paymentMethodName", &config.ReplayMatch{Path: []string{"paymentMethodName"}}},
		{"path and body", "path:paymentMethodName;body:reference", &config.ReplayMatch{Path: []string{"paymentMethodName"}, Body: []string{"reference"}}},
		{"path body query", "path:id;body:name;query:channel", &config.ReplayMatch{Path: []string{"id"}, Body: []string{"name"}, Query: []string{"channel"}}},
		{"empty sections", ";;name;;", &config.ReplayMatch{Body: []string{"name"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseReplayHeader(tt.input)
			assert.Equal(tt.expected, result)
		})
	}
}

func TestResolveReplayParams(t *testing.T) {
	assert := assert2.New(t)

	t.Run("no header and no auto-replay returns nil", func(t *testing.T) {
		cfg := &config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/foo": {"POST": {Match: &config.ReplayMatch{Body: []string{"name"}}}},
					},
				},
			},
		}
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", nil)

		match, pattern, _ := resolveReplayParams(req, cfg)
		assert.Nil(match)
		assert.Empty(pattern)
	})

	t.Run("header with values overrides config", func(t *testing.T) {
		cfg := &config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/foo": {"POST": {Match: &config.ReplayMatch{Body: []string{"name"}}}},
					},
				},
			},
		}
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", nil)
		req.Header.Set(headerReplayMatch, "age,city")

		match, pattern, _ := resolveReplayParams(req, cfg)
		assert.Equal(&config.ReplayMatch{Body: []string{"age", "city"}}, match)
		assert.Equal("/foo", pattern) // uses config pattern
	})

	t.Run("empty header falls back to config match", func(t *testing.T) {
		cfg := &config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/foo": {"POST": {Match: &config.ReplayMatch{Body: []string{"name", "zip"}}}},
					},
				},
			},
		}
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", nil)
		req.Header.Set(headerReplayMatch, "")

		match, pattern, _ := resolveReplayParams(req, cfg)
		assert.Equal(&config.ReplayMatch{Body: []string{"name", "zip"}}, match)
		assert.Equal("/foo", pattern)
	})

	t.Run("header without config uses actual path", func(t *testing.T) {
		cfg := &config.ServiceConfig{Name: "svc"}
		req := httptest.NewRequest(http.MethodPost, "/svc/foo/123", nil)
		req.Header.Set(headerReplayMatch, "name")

		match, pattern, _ := resolveReplayParams(req, cfg)
		assert.Equal(&config.ReplayMatch{Body: []string{"name"}}, match)
		assert.Equal("/foo/123", pattern) // actual path, no config pattern
	})

	t.Run("empty header without config returns nil", func(t *testing.T) {
		cfg := &config.ServiceConfig{Name: "svc"}
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", nil)
		req.Header.Set(headerReplayMatch, "")

		match, _, _ := resolveReplayParams(req, cfg)
		assert.Nil(match)
	})

	t.Run("empty header skips when method not configured", func(t *testing.T) {
		cfg := &config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/foo/{id}/bar": {"POST": {Match: &config.ReplayMatch{Body: []string{"name"}}}},
					},
				},
			},
		}
		req := httptest.NewRequest(http.MethodGet, "/svc/foo/123/bar", nil)
		req.Header.Set(headerReplayMatch, "")

		match, pattern, _ := resolveReplayParams(req, cfg)
		assert.Nil(match)
		assert.Empty(pattern)
	})

	t.Run("auto-replay activates for configured endpoint", func(t *testing.T) {
		cfg := &config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					AutoReplay: true,
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/foo": {"POST": {Match: &config.ReplayMatch{Body: []string{"name"}}}},
					},
				},
			},
		}
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", nil)

		match, pattern, _ := resolveReplayParams(req, cfg)
		assert.Equal(&config.ReplayMatch{Body: []string{"name"}}, match)
		assert.Equal("/foo", pattern)
	})

	t.Run("auto-replay skips non-configured endpoint", func(t *testing.T) {
		cfg := &config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					AutoReplay: true,
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/foo": {"POST": {Match: &config.ReplayMatch{Body: []string{"name"}}}},
					},
				},
			},
		}
		req := httptest.NewRequest(http.MethodPost, "/svc/bar", nil)

		match, pattern, _ := resolveReplayParams(req, cfg)
		assert.Nil(match)
		assert.Empty(pattern)
	})

	t.Run("header overrides auto-replay config", func(t *testing.T) {
		cfg := &config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					AutoReplay: true,
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/foo": {"POST": {Match: &config.ReplayMatch{Body: []string{"name"}}}},
					},
				},
			},
		}
		req := httptest.NewRequest(http.MethodPost, "/svc/foo", nil)
		req.Header.Set(headerReplayMatch, "age")

		match, pattern, _ := resolveReplayParams(req, cfg)
		assert.Equal(&config.ReplayMatch{Body: []string{"age"}}, match)
		assert.Equal("/foo", pattern) // still uses config pattern
	})

	t.Run("parameterized pattern path returned", func(t *testing.T) {
		cfg := &config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/foo/{id}/bar": {"POST": {Match: &config.ReplayMatch{Body: []string{"name"}}}},
					},
				},
			},
		}
		req := httptest.NewRequest(http.MethodPost, "/svc/foo/123/bar", nil)
		req.Header.Set(headerReplayMatch, "")

		match, pattern, _ := resolveReplayParams(req, cfg)
		assert.Equal(&config.ReplayMatch{Body: []string{"name"}}, match)
		assert.Equal("/foo/{id}/bar", pattern)
	})

	t.Run("returns endpoint path for path variable extraction", func(t *testing.T) {
		cfg := &config.ServiceConfig{
			Name: "svc",
			Cache: &config.CacheConfig{
				Replay: &config.ReplayConfig{
					Endpoints: map[string]map[string]*config.ReplayEndpoint{
						"/pay/{paymentMethod}/tx/{txId}": {"POST": {Match: &config.ReplayMatch{Path: []string{"paymentMethod"}, Body: []string{"ref"}}}},
					},
				},
			},
		}
		req := httptest.NewRequest(http.MethodPost, "/svc/pay/credit-card/tx/123", nil)
		req.Header.Set(headerReplayMatch, "")

		match, pattern, endpointPath := resolveReplayParams(req, cfg)
		assert.Equal(&config.ReplayMatch{Path: []string{"paymentMethod"}, Body: []string{"ref"}}, match)
		assert.Equal("/pay/{paymentMethod}/tx/{txId}", pattern)
		assert.Equal("/pay/credit-card/tx/123", endpointPath)
	})
}

func TestBuildReplayKey(t *testing.T) {
	assert := assert2.New(t)

	t.Run("deterministic for same input", func(t *testing.T) {
		body := []byte(`{"name":"Jane","zip":"12345"}`)
		req := httptest.NewRequest(http.MethodPost, "/foo", nil)
		key1 := buildReplayKey(req, "/foo", "/foo", &config.ReplayMatch{Body: []string{"name", "zip"}}, body)
		key2 := buildReplayKey(req, "/foo", "/foo", &config.ReplayMatch{Body: []string{"name", "zip"}}, body)
		assert.Equal(key1, key2)
	})

	t.Run("fields are sorted for determinism", func(t *testing.T) {
		body := []byte(`{"name":"Jane","zip":"12345"}`)
		req := httptest.NewRequest(http.MethodPost, "/foo", nil)
		key1 := buildReplayKey(req, "/foo", "/foo", &config.ReplayMatch{Body: []string{"name", "zip"}}, body)
		key2 := buildReplayKey(req, "/foo", "/foo", &config.ReplayMatch{Body: []string{"zip", "name"}}, body)
		assert.Equal(key1, key2)
	})

	t.Run("different methods produce different keys", func(t *testing.T) {
		body := []byte(`{"name":"Jane"}`)
		req1 := httptest.NewRequest(http.MethodPost, "/foo", nil)
		req2 := httptest.NewRequest(http.MethodPut, "/foo", nil)
		key1 := buildReplayKey(req1, "/foo", "/foo", &config.ReplayMatch{Body: []string{"name"}}, body)
		key2 := buildReplayKey(req2, "/foo", "/foo", &config.ReplayMatch{Body: []string{"name"}}, body)
		assert.NotEqual(key1, key2)
	})

	t.Run("different paths produce different keys", func(t *testing.T) {
		body := []byte(`{"name":"Jane"}`)
		req := httptest.NewRequest(http.MethodPost, "/foo", nil)
		key1 := buildReplayKey(req, "/foo", "/foo", &config.ReplayMatch{Body: []string{"name"}}, body)
		key2 := buildReplayKey(req, "/bar", "/bar", &config.ReplayMatch{Body: []string{"name"}}, body)
		assert.NotEqual(key1, key2)
	})

	t.Run("different values produce different keys", func(t *testing.T) {
		body1 := []byte(`{"name":"Jane"}`)
		body2 := []byte(`{"name":"John"}`)
		req := httptest.NewRequest(http.MethodPost, "/foo", nil)
		key1 := buildReplayKey(req, "/foo", "/foo", &config.ReplayMatch{Body: []string{"name"}}, body1)
		key2 := buildReplayKey(req, "/foo", "/foo", &config.ReplayMatch{Body: []string{"name"}}, body2)
		assert.NotEqual(key1, key2)
	})

	t.Run("missing body field returns empty key", func(t *testing.T) {
		body := []byte(`{"name":"Jane"}`)
		req := httptest.NewRequest(http.MethodPost, "/foo", nil)
		key := buildReplayKey(req, "/foo", "/foo", &config.ReplayMatch{Body: []string{"missing"}}, body)
		assert.Empty(key)
	})

	t.Run("nil body with body match returns empty key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/foo", nil)
		key := buildReplayKey(req, "/foo", "/foo", &config.ReplayMatch{Body: []string{"name"}}, nil)
		assert.Empty(key)
	})

	t.Run("missing query field returns empty key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/foo?other=1", nil)
		key := buildReplayKey(req, "/foo", "/foo", &config.ReplayMatch{Query: []string{"missing"}}, nil)
		assert.Empty(key)
	})

	t.Run("present fields still produce valid key", func(t *testing.T) {
		body := []byte(`{"name":"Jane"}`)
		req := httptest.NewRequest(http.MethodPost, "/foo?channel=web", nil)
		key := buildReplayKey(req, "/foo", "/foo", &config.ReplayMatch{Body: []string{"name"}, Query: []string{"channel"}}, body)
		assert.NotEmpty(key)
		assert.Len(key, 64)
	})

	t.Run("empty fields", func(t *testing.T) {
		body := []byte(`{"name":"Jane"}`)
		req := httptest.NewRequest(http.MethodPost, "/foo", nil)
		key := buildReplayKey(req, "/foo", "/foo", &config.ReplayMatch{Body: []string{}}, body)
		assert.NotEmpty(key)
		assert.Len(key, 64)
	})

	t.Run("form-encoded body", func(t *testing.T) {
		body := []byte("amount=50&biller=BLR0001&reference=REF123")
		req := httptest.NewRequest(http.MethodPost, "/pay", nil)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		key1 := buildReplayKey(req, "/pay", "/pay", &config.ReplayMatch{Body: []string{"biller", "reference"}}, body)
		key2 := buildReplayKey(req, "/pay", "/pay", &config.ReplayMatch{Body: []string{"reference", "biller"}}, body)
		assert.Equal(key1, key2)

		// Different biller produces different key
		body2 := []byte("amount=50&biller=BLR0002&reference=REF123")
		key3 := buildReplayKey(req, "/pay", "/pay", &config.ReplayMatch{Body: []string{"biller", "reference"}}, body2)
		assert.NotEqual(key1, key3)
	})

	t.Run("query string parameters", func(t *testing.T) {
		req1 := httptest.NewRequest(http.MethodGet, "/pay?amount=50&biller=BLR0001", nil)
		key1 := buildReplayKey(req1, "/pay", "/pay", &config.ReplayMatch{Query: []string{"amount", "biller"}}, nil)
		key2 := buildReplayKey(req1, "/pay", "/pay", &config.ReplayMatch{Query: []string{"biller", "amount"}}, nil)
		assert.Equal(key1, key2)

		// Different query value produces different key
		req2 := httptest.NewRequest(http.MethodGet, "/pay?amount=100&biller=BLR0001", nil)
		key3 := buildReplayKey(req2, "/pay", "/pay", &config.ReplayMatch{Query: []string{"amount", "biller"}}, nil)
		assert.NotEqual(key1, key3)
	})

	t.Run("path variables produce different keys for different values", func(t *testing.T) {
		body := []byte(`{"ref":"REF123"}`)
		req := httptest.NewRequest(http.MethodPost, "/pay/credit-card/tx/123", nil)
		match := &config.ReplayMatch{Path: []string{"paymentMethodName"}, Body: []string{"ref"}}
		key1 := buildReplayKey(req, "/pay/{paymentMethodName}/tx/{txId}", "/pay/credit-card/tx/123", match, body)
		key2 := buildReplayKey(req, "/pay/{paymentMethodName}/tx/{txId}", "/pay/bank-transfer/tx/123", match, body)
		assert.NotEmpty(key1)
		assert.NotEmpty(key2)
		assert.NotEqual(key1, key2)
	})

	t.Run("path variables are sorted with other fields", func(t *testing.T) {
		body := []byte(`{"ref":"REF123"}`)
		req := httptest.NewRequest(http.MethodPost, "/pay/credit-card/tx/123", nil)
		match1 := &config.ReplayMatch{Path: []string{"paymentMethodName"}, Body: []string{"ref"}}
		match2 := &config.ReplayMatch{Body: []string{"ref"}, Path: []string{"paymentMethodName"}}
		key1 := buildReplayKey(req, "/pay/{paymentMethodName}/tx/{txId}", "/pay/credit-card/tx/123", match1, body)
		key2 := buildReplayKey(req, "/pay/{paymentMethodName}/tx/{txId}", "/pay/credit-card/tx/123", match2, body)
		assert.Equal(key1, key2)
	})

	t.Run("missing path variable returns empty key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/pay/credit-card", nil)
		match := &config.ReplayMatch{Path: []string{"nonExistent"}}
		key := buildReplayKey(req, "/pay/{paymentMethodName}", "/pay/credit-card", match, nil)
		assert.Empty(key)
	})
}

func TestReadAndRestoreBody(t *testing.T) {
	assert := assert2.New(t)

	t.Run("reads and restores body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"name":"Jane"}`))

		body := readAndRestoreBody(req)
		assert.Equal([]byte(`{"name":"Jane"}`), body)

		// Body should be readable again
		restored, err := io.ReadAll(req.Body)
		assert.NoError(err)
		assert.Equal([]byte(`{"name":"Jane"}`), restored)
	})

	t.Run("nil body returns nil", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Body = nil

		body := readAndRestoreBody(req)
		assert.Nil(body)
	})

	t.Run("empty body returns empty bytes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(""))

		body := readAndRestoreBody(req)
		assert.Equal([]byte{}, body)
	})

	t.Run("body can be read multiple times", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader("data"))

		body1 := readAndRestoreBody(req)
		body2 := readAndRestoreBody(req)
		assert.Equal(body1, body2)
	})
}

func TestGetEndpointPath(t *testing.T) {
	assert := assert2.New(t)

	tests := []struct {
		name        string
		urlPath     string
		serviceName string
		expected    string
	}{
		{"strips service prefix", "/svc/foo/bar", "svc", "/foo/bar"},
		{"root path", "/svc", "svc", "/"},
		{"no prefix match", "/other/foo", "svc", "/other/foo"},
		{"nested path", "/svc/a/b/c", "svc", "/a/b/c"},
		{"empty service name", "/foo/bar", "", "foo/bar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.urlPath, nil)
			result := getEndpointPath(req, tt.serviceName)
			assert.Equal(tt.expected, result)
		})
	}
}

func TestDeserializeReplayRecord(t *testing.T) {
	assert := assert2.New(t)

	t.Run("nil returns nil", func(t *testing.T) {
		assert.Nil(deserializeReplayRecord(nil))
	})

	t.Run("direct *ReplayRecord", func(t *testing.T) {
		rec := &ReplayRecord{
			Data:       []byte("test"),
			StatusCode: 200,
		}
		result := deserializeReplayRecord(rec)
		assert.Equal(rec, result)
	})

	t.Run("map[string]any (Redis-like)", func(t *testing.T) {
		m := map[string]any{
			"data":           "dGVzdA==", // base64 of "test"
			"statusCode":     float64(200),
			"contentType":    "application/json",
			"isFromUpstream": true,
			"headers":        map[string]any{"X-Custom": "val"},
			"matchValues":    map[string]any{"body:name": "Jane"},
			"createdAt":      "2024-01-01T00:00:00Z",
		}

		result := deserializeReplayRecord(m)
		assert.NotNil(result)
		assert.Equal(200, result.StatusCode)
		assert.Equal("application/json", result.ContentType)
		assert.True(result.IsFromUpstream)
	})

	t.Run("incompatible type returns nil", func(t *testing.T) {
		result := deserializeReplayRecord("not a record")
		assert.Nil(result)
	})

	t.Run("complete round-trip", func(t *testing.T) {
		original := &ReplayRecord{
			Data:           []byte(`{"result":true}`),
			Headers:        map[string]string{"Content-Type": "application/json"},
			StatusCode:     201,
			ContentType:    "application/json",
			IsFromUpstream: false,
			RequestBody:    []byte(`{"name":"Jane"}`),
			MatchValues:    map[string]any{"body:name": "Jane"},
			CreatedAt:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		}

		// Direct pointer
		result := deserializeReplayRecord(original)
		assert.Equal(original, result)
	})
}

func TestWriteThrough(t *testing.T) {
	assert := assert2.New(t)

	t.Run("copies headers status and body", func(t *testing.T) {
		w := httptest.NewRecorder()

		buf := new(bytes.Buffer)
		buf.WriteString("response body")

		underlying := httptest.NewRecorder()
		underlying.Header().Set("Content-Type", "application/json")
		underlying.Header().Set("X-Custom", "value")

		capture := &responseWriter{
			ResponseWriter: underlying,
			body:           buf,
			statusCode:     http.StatusCreated,
		}

		writeThrough(w, capture)

		assert.Equal(http.StatusCreated, w.Code)
		assert.Equal("response body", w.Body.String())
		assert.Equal("application/json", w.Header().Get("Content-Type"))
		assert.Equal("value", w.Header().Get("X-Custom"))
	})

	t.Run("empty body and default status", func(t *testing.T) {
		w := httptest.NewRecorder()

		capture := &responseWriter{
			ResponseWriter: httptest.NewRecorder(),
			body:           new(bytes.Buffer),
			statusCode:     http.StatusOK,
		}

		writeThrough(w, capture)

		assert.Equal(http.StatusOK, w.Code)
		assert.Empty(w.Body.String())
	})
}

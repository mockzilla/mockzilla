package db

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	assert2 "github.com/stretchr/testify/assert"
)

// newTestHistoryTable creates a history table with a backing memory table for testing.
func newTestHistoryTable(clearTimeout time.Duration) *memoryHistoryTable {
	table := newMemoryTable()
	return newMemoryHistoryTable(table, clearTimeout)
}

func TestMemoryHistoryTable_Get(t *testing.T) {
	assert := assert2.New(t)
	ctx := context.Background()

	t.Run("get existing request", func(t *testing.T) {
		h := newTestHistoryTable(0)
		h.Set(ctx, "Foo", &HistoryRequest{Method: "GET", URL: "/foo/1"}, nil)

		req, _ := http.NewRequest("GET", "/foo/1", nil)
		resource, ok := h.Get(ctx, req)

		assert.True(ok)
		assert.Equal("Foo", resource.Resource)
	})

	t.Run("get non-existing request", func(t *testing.T) {
		h := newTestHistoryTable(0)

		req, _ := http.NewRequest("GET", "/foo/1", nil)
		resource, ok := h.Get(ctx, req)

		assert.False(ok)
		assert.Nil(resource)
	})

	t.Run("get returns latest entry", func(t *testing.T) {
		h := newTestHistoryTable(0)
		histReq := &HistoryRequest{Method: "GET", URL: "/foo/1"}

		h.Set(ctx, "First", histReq, nil)
		h.Set(ctx, "Second", histReq, nil)

		req, _ := http.NewRequest("GET", "/foo/1", nil)
		resource, ok := h.Get(ctx, req)
		assert.True(ok)
		assert.Equal("Second", resource.Resource)
	})
}

func TestMemoryHistoryTable_Set(t *testing.T) {
	assert := assert2.New(t)
	ctx := context.Background()

	t.Run("set new request", func(t *testing.T) {
		h := newTestHistoryTable(0)

		result := h.Set(ctx, "/foo/{id}", &HistoryRequest{
			Method: "POST",
			URL:    "/foo/1",
			Body:   []byte(`{"name":"test"}`),
		}, nil)

		assert.Equal("/foo/{id}", result.Resource)
		assert.Equal(`{"name":"test"}`, string(result.Request.Body))
		assert.NotEmpty(result.ID)
	})

	t.Run("set with response", func(t *testing.T) {
		h := newTestHistoryTable(0)

		response := &HistoryResponse{Body: []byte("response"), StatusCode: 200}
		result := h.Set(ctx, "/foo/{id}", &HistoryRequest{Method: "GET", URL: "/foo/1"}, response)

		assert.Equal(response, result.Response)
	})

	t.Run("multiple sets to same endpoint create unique entries", func(t *testing.T) {
		h := newTestHistoryTable(0)

		e1 := h.Set(ctx, "/foo/{id}", &HistoryRequest{Method: "POST", URL: "/foo/1", Body: []byte(`{"a":"1"}`)}, nil)
		e2 := h.Set(ctx, "/foo/{id}", &HistoryRequest{Method: "POST", URL: "/foo/1", Body: []byte(`{"a":"2"}`)}, nil)

		assert.NotEqual(e1.ID, e2.ID)
		assert.Len(h.Data(ctx), 2)
	})
}

func TestMemoryHistoryTable_GetByID(t *testing.T) {
	assert := assert2.New(t)
	ctx := context.Background()

	t.Run("returns entry by ID", func(t *testing.T) {
		h := newTestHistoryTable(0)
		entry := h.Set(ctx, "/foo", &HistoryRequest{Method: "GET", URL: "/foo"}, &HistoryResponse{StatusCode: 200})

		got, ok := h.GetByID(ctx, entry.ID)
		assert.True(ok)
		assert.Equal(entry.ID, got.ID)
	})

	t.Run("returns false for unknown ID", func(t *testing.T) {
		h := newTestHistoryTable(0)
		_, ok := h.GetByID(ctx, "nonexistent")
		assert.False(ok)
	})
}

func TestMemoryHistoryTable_Set_RequestID(t *testing.T) {
	assert := assert2.New(t)
	ctx := context.Background()

	h := newTestHistoryTable(0)

	entry := h.Set(ctx, "/foo/{id}", &HistoryRequest{
		Method:    "POST",
		URL:       "/foo/1",
		RequestID: "req-123-abc",
	}, nil)

	assert.Equal("req-123-abc", entry.Request.RequestID)

	got, ok := h.GetByID(ctx, entry.ID)
	assert.True(ok)
	assert.Equal("req-123-abc", got.Request.RequestID)
}

func TestMemoryHistoryTable_Set_Duration(t *testing.T) {
	assert := assert2.New(t)
	ctx := context.Background()

	h := newTestHistoryTable(0)

	entry := h.Set(ctx, "/foo/{id}", &HistoryRequest{
		Method: "GET",
		URL:    "/foo/1",
	}, &HistoryResponse{
		StatusCode: 200,
		Duration:   42 * time.Millisecond,
	})

	assert.Equal(42*time.Millisecond, entry.Response.Duration)
}

func TestMemoryHistoryTable_SetResponse(t *testing.T) {
	assert := assert2.New(t)
	ctx := context.Background()

	t.Run("set response for existing request", func(t *testing.T) {
		h := newTestHistoryTable(0)

		histReq := &HistoryRequest{Method: "GET", URL: "/foo/1"}
		h.Set(ctx, "/foo/{id}", histReq, nil)

		response := &HistoryResponse{Body: []byte("response"), StatusCode: 200}
		h.SetResponse(ctx, histReq, response)

		req, _ := http.NewRequest("GET", "/foo/1", nil)
		record, _ := h.Get(ctx, req)
		assert.Equal(response, record.Response)
	})

	t.Run("set response for non-existing request", func(t *testing.T) {
		h := newTestHistoryTable(0)

		response := &HistoryResponse{Body: []byte("response"), StatusCode: 200}

		// Should not panic, just log
		h.SetResponse(ctx, &HistoryRequest{Method: "GET", URL: "/foo/1"}, response)
	})

	t.Run("set response updates latest entry only", func(t *testing.T) {
		h := newTestHistoryTable(0)

		histReq := &HistoryRequest{Method: "GET", URL: "/foo/1"}
		h.Set(ctx, "/foo/{id}", histReq, &HistoryResponse{StatusCode: 100})
		h.Set(ctx, "/foo/{id}", histReq, nil)

		h.SetResponse(ctx, histReq, &HistoryResponse{StatusCode: 200})

		entries := h.Data(ctx)
		assert.Len(entries, 2)
		// First entry should keep its original response
		assert.Equal(100, entries[0].Response.StatusCode)
		// Second (latest) entry should have the updated response
		assert.Equal(200, entries[1].Response.StatusCode)
	})
}

func TestMemoryHistoryTable_Data(t *testing.T) {
	assert := assert2.New(t)
	ctx := context.Background()

	h := newTestHistoryTable(0)

	h.Set(ctx, "/foo/{id}", &HistoryRequest{Method: "GET", URL: "/foo/1"}, nil)

	data := h.Data(ctx)

	assert.Len(data, 1)
	assert.Equal("/foo/{id}", data[0].Resource)
	assert.NotEmpty(data[0].ID)
}

func TestMemoryHistoryTable_Len(t *testing.T) {
	assert := assert2.New(t)
	ctx := context.Background()

	h := newTestHistoryTable(0)
	assert.Equal(0, h.Len(ctx))

	histReq := &HistoryRequest{Method: "GET", URL: "/foo/1"}
	h.Set(ctx, "/foo/{id}", histReq, nil)
	assert.Equal(1, h.Len(ctx))

	h.Set(ctx, "/foo/{id}", histReq, nil)
	assert.Equal(2, h.Len(ctx))
}

func TestMemoryHistoryTable_Clear(t *testing.T) {
	assert := assert2.New(t)
	ctx := context.Background()

	h := newTestHistoryTable(0)

	h.Set(ctx, "/foo/{id}", &HistoryRequest{Method: "GET", URL: "/foo/1"}, nil)

	h.Clear(ctx)

	assert.Empty(h.Data(ctx))
	assert.Equal(0, h.Len(ctx))
}

func TestMemoryHistoryTable_TTL(t *testing.T) {
	assert := assert2.New(t)
	ctx := context.Background()

	h := newTestHistoryTable(50 * time.Millisecond)

	h.Set(ctx, "/foo/{id}", &HistoryRequest{Method: "GET", URL: "/foo/1"}, nil)

	assert.Equal(1, h.Len(ctx))

	// Wait for entries to expire
	time.Sleep(100 * time.Millisecond)

	assert.Empty(h.Data(ctx))
	assert.Equal(0, h.Len(ctx))

	req, _ := http.NewRequest("GET", "/foo/1", nil)
	_, ok := h.Get(ctx, req)
	assert.False(ok)
}

func TestMemoryHistoryTable_TTL_GetByID(t *testing.T) {
	assert := assert2.New(t)
	ctx := context.Background()

	h := newTestHistoryTable(50 * time.Millisecond)

	entry := h.Set(ctx, "/foo/{id}", &HistoryRequest{Method: "GET", URL: "/foo/1"}, nil)

	got, ok := h.GetByID(ctx, entry.ID)
	assert.True(ok)
	assert.Equal(entry.ID, got.ID)

	// Wait for entry to expire
	time.Sleep(100 * time.Millisecond)

	_, ok = h.GetByID(ctx, entry.ID)
	assert.False(ok)
}

func TestMemoryHistoryTable_TTL_MixedExpiry(t *testing.T) {
	assert := assert2.New(t)
	ctx := context.Background()

	h := newTestHistoryTable(100 * time.Millisecond)

	h.Set(ctx, "/foo/{id}", &HistoryRequest{Method: "GET", URL: "/foo/1"}, nil)

	// Wait 60ms, then add another entry
	time.Sleep(60 * time.Millisecond)
	h.Set(ctx, "/bar/{id}", &HistoryRequest{Method: "GET", URL: "/bar/1"}, nil)

	// At ~60ms: first entry is ~60ms old, second is fresh - both alive
	assert.Equal(2, h.Len(ctx))

	// Wait another 50ms (total ~110ms for first, ~50ms for second)
	time.Sleep(50 * time.Millisecond)

	// First entry expired, second still alive
	data := h.Data(ctx)
	assert.Len(data, 1)
	assert.Equal("/bar/{id}", data[0].Resource)
}

func TestFlattenHeaders(t *testing.T) {
	assert := assert2.New(t)

	t.Run("nil header", func(t *testing.T) {
		assert.Nil(FlattenHeaders(nil))
	})

	t.Run("empty header", func(t *testing.T) {
		assert.Nil(FlattenHeaders(http.Header{}))
	})

	t.Run("single values sorted", func(t *testing.T) {
		h := http.Header{
			"Content-Type": {"application/json"},
			"Accept":       {"text/html"},
		}
		result := FlattenHeaders(h)
		assert.Equal([]string{
			"Accept: text/html",
			"Content-Type: application/json",
		}, result)
	})

	t.Run("multi values joined", func(t *testing.T) {
		h := http.Header{
			"Accept": {"text/html", "application/json"},
		}
		result := FlattenHeaders(h)
		assert.Equal([]string{"Accept: text/html, application/json"}, result)
	})
}

func TestMaskHeaderValues(t *testing.T) {
	assert := assert2.New(t)

	t.Run("nil headers", func(t *testing.T) {
		MaskHeaderValues(nil, []string{"Authorization"})
	})

	t.Run("nil mask list", func(t *testing.T) {
		headers := []string{"Authorization: Bearer token123"}
		MaskHeaderValues(headers, nil)
		assert.Equal([]string{"Authorization: Bearer token123"}, headers)
	})

	t.Run("empty mask list", func(t *testing.T) {
		headers := []string{"Authorization: Bearer token123"}
		MaskHeaderValues(headers, []string{})
		assert.Equal([]string{"Authorization: Bearer token123"}, headers)
	})

	t.Run("masks matching header", func(t *testing.T) {
		headers := []string{
			"Accept: text/html",
			"Authorization: Bearer token123",
			"Content-Type: application/json",
		}
		MaskHeaderValues(headers, []string{"Authorization"})
		assert.Equal([]string{
			"Accept: text/html",
			"Authorization: ***********n123",
			"Content-Type: application/json",
		}, headers)
	})

	t.Run("case insensitive match", func(t *testing.T) {
		headers := []string{"Authorization: Bearer secret"}
		MaskHeaderValues(headers, []string{"authorization"})
		assert.Equal([]string{"Authorization: *********cret"}, headers)
	})

	t.Run("multiple mask headers", func(t *testing.T) {
		headers := []string{
			"Authorization: Bearer abc123",
			"X-Api-Key: my-secret-key",
		}
		MaskHeaderValues(headers, []string{"Authorization", "X-Api-Key"})
		assert.Equal([]string{
			"Authorization: *********c123",
			"X-Api-Key: *********-key",
		}, headers)
	})

	t.Run("short value fully masked", func(t *testing.T) {
		headers := []string{"X-Token: abc"}
		MaskHeaderValues(headers, []string{"X-Token"})
		assert.Equal([]string{"X-Token: ***"}, headers)
	})

	t.Run("exactly 4 chars fully masked", func(t *testing.T) {
		headers := []string{"X-Token: abcd"}
		MaskHeaderValues(headers, []string{"X-Token"})
		assert.Equal([]string{"X-Token: ****"}, headers)
	})

	t.Run("5 chars shows last 4", func(t *testing.T) {
		headers := []string{"X-Token: abcde"}
		MaskHeaderValues(headers, []string{"X-Token"})
		assert.Equal([]string{"X-Token: *bcde"}, headers)
	})

	t.Run("prefix pattern masks matching headers", func(t *testing.T) {
		headers := []string{
			"Accept: text/html",
			"X-Internal-Token: secret-value",
			"X-Internal-Trace: trace-id-123",
		}
		MaskHeaderValues(headers, []string{"X-Internal-*"})
		assert.Equal([]string{
			"Accept: text/html",
			"X-Internal-Token: ********alue",
			"X-Internal-Trace: ********-123",
		}, headers)
	})

	t.Run("prefix pattern case insensitive", func(t *testing.T) {
		headers := []string{"X-Internal-Token: secret"}
		MaskHeaderValues(headers, []string{"x-internal-*"})
		assert.Equal([]string{"X-Internal-Token: **cret"}, headers)
	})

	t.Run("mixed exact and prefix patterns", func(t *testing.T) {
		headers := []string{
			"Authorization: Bearer tok123",
			"X-Custom-Secret: hidden",
			"X-Custom-Public: visible",
			"Content-Type: application/json",
		}
		MaskHeaderValues(headers, []string{"Authorization", "X-Custom-*"})
		assert.Equal([]string{
			"Authorization: *********k123",
			"X-Custom-Secret: **dden",
			"X-Custom-Public: ***ible",
			"Content-Type: application/json",
		}, headers)
	})
}

// lookupKey is tested indirectly through Get (uses *http.Request) and Set (uses *HistoryRequest)
// to verify they produce the same key.
func TestMemoryHistoryTable_LookupKeyConsistency(t *testing.T) {
	assert := assert2.New(t)
	ctx := context.Background()

	h := newTestHistoryTable(0)

	// Set via HistoryRequest
	h.Set(ctx, "/test", &HistoryRequest{Method: "GET", URL: "/test?q=1"}, &HistoryResponse{StatusCode: 200})

	// Get via *http.Request with same method+URL
	req := &http.Request{Method: "GET", URL: &url.URL{Path: "/test", RawQuery: "q=1"}}
	entry, ok := h.Get(ctx, req)
	assert.True(ok)
	assert.Equal(200, entry.Response.StatusCode)
}

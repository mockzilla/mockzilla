package db

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	assert2 "github.com/stretchr/testify/assert"
)

func TestMemoryHistoryTable_Set(t *testing.T) {
	assert := assert2.New(t)
	ctx := context.Background()

	t.Run("set new request", func(t *testing.T) {
		h := newMemoryHistoryTable(0)

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
		h := newMemoryHistoryTable(0)

		response := &HistoryResponse{Body: []byte("response"), StatusCode: 200}
		result := h.Set(ctx, "/foo/{id}", &HistoryRequest{Method: "GET", URL: "/foo/1"}, response)

		assert.Equal(response, result.Response)
	})

	t.Run("multiple sets to same endpoint create unique entries", func(t *testing.T) {
		h := newMemoryHistoryTable(0)

		e1 := h.Set(ctx, "/foo/{id}", &HistoryRequest{Method: "POST", URL: "/foo/1", Body: []byte(`{"a":"1"}`)}, nil)
		e2 := h.Set(ctx, "/foo/{id}", &HistoryRequest{Method: "POST", URL: "/foo/1", Body: []byte(`{"a":"2"}`)}, nil)

		assert.NotEqual(e1.ID, e2.ID)
		assert.Len(h.Recent(ctx, 10), 2)
	})

	t.Run("ids are time-ordered", func(t *testing.T) {
		h := newMemoryHistoryTable(0)

		e1 := h.Set(ctx, "/foo", &HistoryRequest{Method: "GET", URL: "/foo"}, nil)
		e2 := h.Set(ctx, "/foo", &HistoryRequest{Method: "GET", URL: "/foo"}, nil)

		assert.Less(e1.ID, e2.ID)
	})
}

func TestMemoryHistoryTable_GetByID(t *testing.T) {
	assert := assert2.New(t)
	ctx := context.Background()

	t.Run("returns entry by ID", func(t *testing.T) {
		h := newMemoryHistoryTable(0)
		entry := h.Set(ctx, "/foo", &HistoryRequest{Method: "GET", URL: "/foo"}, &HistoryResponse{StatusCode: 200})

		got, ok := h.GetByID(ctx, entry.ID)
		assert.True(ok)
		assert.Equal(entry.ID, got.ID)
	})

	t.Run("returns false for unknown ID", func(t *testing.T) {
		h := newMemoryHistoryTable(0)
		_, ok := h.GetByID(ctx, "nonexistent")
		assert.False(ok)
	})

	t.Run("returns false for expired entry", func(t *testing.T) {
		h := newMemoryHistoryTable(50 * time.Millisecond)
		entry := h.Set(ctx, "/foo/{id}", &HistoryRequest{Method: "GET", URL: "/foo/1"}, nil)

		got, ok := h.GetByID(ctx, entry.ID)
		assert.True(ok)
		assert.Equal(entry.ID, got.ID)

		time.Sleep(100 * time.Millisecond)

		_, ok = h.GetByID(ctx, entry.ID)
		assert.False(ok)
	})
}

func TestMemoryHistoryTable_Set_RequestID(t *testing.T) {
	assert := assert2.New(t)
	ctx := context.Background()

	h := newMemoryHistoryTable(0)

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

	h := newMemoryHistoryTable(0)

	entry := h.Set(ctx, "/foo/{id}", &HistoryRequest{
		Method: "GET",
		URL:    "/foo/1",
	}, &HistoryResponse{
		StatusCode: 200,
		Duration:   42 * time.Millisecond,
	})

	assert.Equal(42*time.Millisecond, entry.Response.Duration)
}

func TestMemoryHistoryTable_Recent(t *testing.T) {
	assert := assert2.New(t)
	ctx := context.Background()

	t.Run("returns body-less projection", func(t *testing.T) {
		h := newMemoryHistoryTable(0)

		h.Set(ctx, "/foo/{id}", &HistoryRequest{
			Method:    "POST",
			URL:       "/foo/1",
			Body:      []byte(`{"name":"test"}`),
			RequestID: "req-1",
		}, &HistoryResponse{
			StatusCode:  201,
			Body:        []byte(`{"id":1}`),
			ContentType: "application/json",
			Duration:    42 * time.Millisecond,
		})

		summaries := h.Recent(ctx, 10)
		assert.Len(summaries, 1)

		s := summaries[0]
		assert.NotEmpty(s.ID)
		assert.Equal("/foo/{id}", s.Resource)
		assert.NotNil(s.Request)
		assert.Equal("POST", s.Request.Method)
		assert.Equal("/foo/1", s.Request.URL)
		assert.Equal("req-1", s.Request.RequestID)
		assert.NotNil(s.Response)
		assert.Equal(201, s.Response.StatusCode)
		assert.Equal("application/json", s.Response.ContentType)
		assert.Equal(42*time.Millisecond, s.Response.Duration)
	})

	t.Run("newest first", func(t *testing.T) {
		h := newMemoryHistoryTable(0)
		h.Set(ctx, "/first", &HistoryRequest{Method: "GET", URL: "/1"}, nil)
		h.Set(ctx, "/second", &HistoryRequest{Method: "GET", URL: "/2"}, nil)
		h.Set(ctx, "/third", &HistoryRequest{Method: "GET", URL: "/3"}, nil)

		summaries := h.Recent(ctx, 10)
		assert.Len(summaries, 3)
		assert.Equal("/third", summaries[0].Resource)
		assert.Equal("/second", summaries[1].Resource)
		assert.Equal("/first", summaries[2].Resource)
	})

	t.Run("honours limit", func(t *testing.T) {
		h := newMemoryHistoryTable(0)
		for i := 0; i < 5; i++ {
			h.Set(ctx, "/foo", &HistoryRequest{Method: "GET", URL: "/foo"}, nil)
		}

		assert.Len(h.Recent(ctx, 2), 2)
		assert.Nil(h.Recent(ctx, 0))
		assert.Nil(h.Recent(ctx, -1))
	})

	t.Run("empty history", func(t *testing.T) {
		h := newMemoryHistoryTable(0)
		assert.Empty(h.Recent(ctx, 10))
	})

	t.Run("expired entries skipped", func(t *testing.T) {
		h := newMemoryHistoryTable(50 * time.Millisecond)
		h.Set(ctx, "/old", &HistoryRequest{Method: "GET", URL: "/old"}, nil)
		time.Sleep(80 * time.Millisecond)
		h.Set(ctx, "/new", &HistoryRequest{Method: "GET", URL: "/new"}, nil)

		summaries := h.Recent(ctx, 10)
		assert.Len(summaries, 1)
		assert.Equal("/new", summaries[0].Resource)
	})
}

func TestMemoryHistoryTable_Eviction(t *testing.T) {
	assert := assert2.New(t)
	ctx := context.Background()

	h := newMemoryHistoryTable(0)

	overflow := 10
	ids := make([]string, 0, MaxHistoryEntries+overflow)
	for i := 0; i < MaxHistoryEntries+overflow; i++ {
		ids = append(ids, h.Set(ctx, "/foo", &HistoryRequest{Method: "GET", URL: "/foo"}, nil).ID)
	}

	assert.Len(h.Recent(ctx, MaxHistoryEntries*2), MaxHistoryEntries)

	for _, id := range ids[:overflow] {
		_, ok := h.GetByID(ctx, id)
		assert.False(ok, "evicted entry should be dropped from the id index")
	}
	for _, id := range ids[overflow:] {
		_, ok := h.GetByID(ctx, id)
		assert.True(ok)
	}
}

func TestMemoryHistoryTable_Clear(t *testing.T) {
	assert := assert2.New(t)
	ctx := context.Background()

	h := newMemoryHistoryTable(0)

	entry := h.Set(ctx, "/foo/{id}", &HistoryRequest{Method: "GET", URL: "/foo/1"}, nil)

	h.Clear(ctx)

	assert.Empty(h.Recent(ctx, 10))
	_, ok := h.GetByID(ctx, entry.ID)
	assert.False(ok)
}

func TestMemoryHistoryTable_TTL(t *testing.T) {
	assert := assert2.New(t)
	ctx := context.Background()

	h := newMemoryHistoryTable(50 * time.Millisecond)

	h.Set(ctx, "/foo/{id}", &HistoryRequest{Method: "GET", URL: "/foo/1"}, nil)
	assert.Len(h.Recent(ctx, 10), 1)

	time.Sleep(100 * time.Millisecond)

	assert.Empty(h.Recent(ctx, 10))
}

func TestMemoryHistoryTable_TTL_MixedExpiry(t *testing.T) {
	assert := assert2.New(t)
	ctx := context.Background()

	h := newMemoryHistoryTable(100 * time.Millisecond)

	h.Set(ctx, "/foo/{id}", &HistoryRequest{Method: "GET", URL: "/foo/1"}, nil)

	time.Sleep(60 * time.Millisecond)
	h.Set(ctx, "/bar/{id}", &HistoryRequest{Method: "GET", URL: "/bar/1"}, nil)

	assert.Len(h.Recent(ctx, 10), 2)

	time.Sleep(50 * time.Millisecond)

	summaries := h.Recent(ctx, 10)
	assert.Len(summaries, 1)
	assert.Equal("/bar/{id}", summaries[0].Resource)
}

func TestSummaryOf(t *testing.T) {
	assert := assert2.New(t)

	assert.Nil(SummaryOf(nil))

	// A record with no response yet projects to a summary with no response.
	s := SummaryOf(&HistoryEntry{ID: "1", Resource: "/x", Request: &HistoryRequest{Method: "GET", URL: "/x"}})
	assert.NotNil(s.Request)
	assert.Nil(s.Response)

	// And a bare record keeps both sides nil rather than inventing them.
	s = SummaryOf(&HistoryEntry{ID: "1"})
	assert.Nil(s.Request)
	assert.Nil(s.Response)
}

func TestNewHistoryID(t *testing.T) {
	assert := assert2.New(t)

	assert.NotEqual(NewHistoryID(), NewHistoryID())
	assert.Less(NewHistoryID(), NewHistoryID())
}

// brokenRand stands in for an entropy source that has stopped working.
type brokenRand struct{}

func (brokenRand) Read([]byte) (int, error) { return 0, errors.New("no entropy") }

func TestNewHistoryID_EntropyFailure(t *testing.T) {
	assert := assert2.New(t)

	uuid.SetRand(brokenRand{})
	defer uuid.SetRand(nil)

	// uuid's own string helpers panic on this error, so the fallback must not
	// reach for one.
	var first, second string
	assert.NotPanics(func() {
		first = NewHistoryID()
		second = NewHistoryID()
	})

	assert.NotEmpty(first)
	assert.NotEqual(first, second)
	assert.Less(first, second, "the fallback still has to sort by time")
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

	t.Run("leaves a header with no separator alone", func(t *testing.T) {
		headers := []string{"Authorization", "Cookie: secret-value"}
		MaskHeaderValues(headers, []string{"Authorization", "Cookie"})
		assert.Equal([]string{"Authorization", "Cookie: ********alue"}, headers)
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

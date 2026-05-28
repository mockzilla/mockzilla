package db

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

func newTestRedisHistory(t *testing.T) (*redisHistoryTable, *miniredis.Miniredis) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	history := newRedisHistoryTable(client, "test:history", 5*time.Minute)
	return history, mr
}

func TestRedisHistory_Get(t *testing.T) {
	ctx := context.Background()

	t.Run("get existing entry", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)
		history.Set(ctx, "/users/123", &HistoryRequest{Method: "GET", URL: "/users/123"}, &HistoryResponse{
			StatusCode: 200,
			Body:       []byte(`{"id":123}`),
		})

		req := &http.Request{Method: "GET", URL: &url.URL{Path: "/users/123"}}
		entry, ok := history.Get(ctx, req)
		assert.True(t, ok)
		assert.Equal(t, "/users/123", entry.Resource)
		assert.Equal(t, 200, entry.Response.StatusCode)
		assert.NotEmpty(t, entry.ID)
	})

	t.Run("get non-existing entry", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)
		req := &http.Request{Method: "GET", URL: &url.URL{Path: "/notfound"}}

		entry, ok := history.Get(ctx, req)
		assert.False(t, ok)
		assert.Nil(t, entry)
	})

	t.Run("get returns latest entry", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)
		histReq := &HistoryRequest{Method: "GET", URL: "/test"}

		history.Set(ctx, "first", histReq, &HistoryResponse{StatusCode: 100})
		history.Set(ctx, "second", histReq, &HistoryResponse{StatusCode: 200})

		req := &http.Request{Method: "GET", URL: &url.URL{Path: "/test"}}
		entry, ok := history.Get(ctx, req)
		assert.True(t, ok)
		assert.Equal(t, "second", entry.Resource)
		assert.Equal(t, 200, entry.Response.StatusCode)
	})
}

func TestRedisHistory_Set(t *testing.T) {
	ctx := context.Background()

	t.Run("set with body", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)
		body := `{"name":"test"}`

		entry := history.Set(ctx, "/users", &HistoryRequest{
			Method: "POST",
			URL:    "/users",
			Body:   []byte(body),
		}, &HistoryResponse{StatusCode: 201})

		assert.Equal(t, "/users", entry.Resource)
		assert.Equal(t, []byte(body), entry.Request.Body)
		assert.Equal(t, 201, entry.Response.StatusCode)
		assert.NotEmpty(t, entry.ID)
	})

	t.Run("set without body", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)

		entry := history.Set(ctx, "/health", &HistoryRequest{Method: "GET", URL: "/health"}, nil)

		assert.Equal(t, "/health", entry.Resource)
		assert.Empty(t, entry.Request.Body)
	})

	t.Run("set with request ID round-trips", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)

		entry := history.Set(ctx, "/users", &HistoryRequest{
			Method:    "POST",
			URL:       "/users",
			RequestID: "redis-req-id-42",
		}, &HistoryResponse{StatusCode: 201})

		assert.Equal(t, "redis-req-id-42", entry.Request.RequestID)

		got, ok := history.GetByID(ctx, entry.ID)
		assert.True(t, ok)
		assert.Equal(t, "redis-req-id-42", got.Request.RequestID)
	})

	t.Run("set with duration round-trips", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)

		entry := history.Set(ctx, "/test", &HistoryRequest{
			Method: "GET",
			URL:    "/test",
		}, &HistoryResponse{
			StatusCode: 200,
			Duration:   55 * time.Millisecond,
		})

		assert.Equal(t, 55*time.Millisecond, entry.Response.Duration)

		got, ok := history.GetByID(ctx, entry.ID)
		assert.True(t, ok)
		assert.Equal(t, 55*time.Millisecond, got.Response.Duration)
	})

	t.Run("multiple sets create unique entries", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)
		histReq := &HistoryRequest{Method: "GET", URL: "/test"}

		e1 := history.Set(ctx, "/test", histReq, nil)
		e2 := history.Set(ctx, "/test", histReq, nil)

		assert.NotEqual(t, e1.ID, e2.ID)
		assert.Len(t, history.Data(ctx), 2)
	})
}

func TestRedisHistory_GetByID(t *testing.T) {
	ctx := context.Background()

	t.Run("returns entry by ID", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)
		entry := history.Set(ctx, "/test", &HistoryRequest{Method: "GET", URL: "/test"}, &HistoryResponse{StatusCode: 200})

		got, ok := history.GetByID(ctx, entry.ID)
		assert.True(t, ok)
		assert.Equal(t, entry.ID, got.ID)
		assert.Equal(t, 200, got.Response.StatusCode)
	})

	t.Run("returns false for unknown ID", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)
		_, ok := history.GetByID(ctx, "nonexistent")
		assert.False(t, ok)
	})
}

func TestRedisHistory_SetResponse(t *testing.T) {
	ctx := context.Background()

	t.Run("update existing entry", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)
		histReq := &HistoryRequest{Method: "GET", URL: "/test"}
		history.Set(ctx, "/test", histReq, nil)

		history.SetResponse(ctx, histReq, &HistoryResponse{
			StatusCode:  200,
			Body:        []byte(`{"ok":true}`),
			ContentType: "application/json",
		})

		req := &http.Request{Method: "GET", URL: &url.URL{Path: "/test"}}
		entry, ok := history.Get(ctx, req)
		assert.True(t, ok)
		assert.Equal(t, 200, entry.Response.StatusCode)
		assert.Equal(t, "application/json", entry.Response.ContentType)
	})

	t.Run("set response for non-existing entry", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)

		// Should not panic, just log
		history.SetResponse(ctx, &HistoryRequest{Method: "GET", URL: "/missing"}, &HistoryResponse{StatusCode: 200})

		// Entry should still not exist
		req := &http.Request{Method: "GET", URL: &url.URL{Path: "/missing"}}
		_, ok := history.Get(ctx, req)
		assert.False(t, ok)
	})
}

func TestRedisHistory_Data(t *testing.T) {
	ctx := context.Background()

	t.Run("returns all entries", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)
		history.Set(ctx, "/a", &HistoryRequest{Method: "GET", URL: "/a"}, &HistoryResponse{StatusCode: 200})
		history.Set(ctx, "/b", &HistoryRequest{Method: "POST", URL: "/b"}, &HistoryResponse{StatusCode: 201})

		data := history.Data(ctx)

		assert.Len(t, data, 2)
	})

	t.Run("empty history", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)

		data := history.Data(ctx)

		assert.Empty(t, data)
	})

	t.Run("skips invalid json entries", func(t *testing.T) {
		history, mr := newTestRedisHistory(t)
		history.Set(ctx, "/valid", &HistoryRequest{Method: "GET", URL: "/valid"}, &HistoryResponse{StatusCode: 200})

		// Inject invalid JSON directly into an entry key
		_ = mr.Set("test:history:entry:bad", "not-valid-json{")

		data := history.Data(ctx)

		// Should only contain the valid entry
		assert.Len(t, data, 1)
	})
}

func TestRedisHistory_Summaries(t *testing.T) {
	ctx := context.Background()

	t.Run("returns body-less projection", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)
		history.Set(ctx, "/foo/{id}", &HistoryRequest{
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

		summaries := history.Summaries(ctx)
		assert.Len(t, summaries, 1)
		s := summaries[0]
		assert.Equal(t, "/foo/{id}", s.Resource)
		assert.Equal(t, "POST", s.Request.Method)
		assert.Equal(t, "req-1", s.Request.RequestID)
		assert.Equal(t, 201, s.Response.StatusCode)
		assert.Equal(t, 42*time.Millisecond, s.Response.Duration)
	})

	t.Run("empty history", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)
		assert.Empty(t, history.Summaries(ctx))
	})
}

func TestRedisHistory_Len(t *testing.T) {
	ctx := context.Background()

	t.Run("empty", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)
		assert.Equal(t, 0, history.Len(ctx))
	})

	t.Run("counts entries", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)
		histReq := &HistoryRequest{Method: "GET", URL: "/test"}
		history.Set(ctx, "/test", histReq, nil)
		history.Set(ctx, "/test", histReq, nil)

		assert.Equal(t, 2, history.Len(ctx))
	})
}

func TestRedisHistory_Clear(t *testing.T) {
	ctx := context.Background()

	t.Run("clears all entries", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)
		history.Set(ctx, "/x", &HistoryRequest{Method: "GET", URL: "/x"}, nil)
		history.Set(ctx, "/y", &HistoryRequest{Method: "GET", URL: "/y"}, nil)

		history.Clear(ctx)

		data := history.Data(ctx)
		assert.Empty(t, data)
		assert.Equal(t, 0, history.Len(ctx))
	})

	t.Run("clear empty history", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)
		// Should not panic
		history.Clear(ctx)
	})
}

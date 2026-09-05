package db

import (
	"context"
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
		assert.Len(t, history.Recent(ctx, 10), 2)
	})

	t.Run("writes one index key and one entry key", func(t *testing.T) {
		history, mr := newTestRedisHistory(t)
		history.Set(ctx, "/test", &HistoryRequest{Method: "GET", URL: "/test"}, nil)

		assert.Len(t, mr.Keys(), 2)
		assert.Contains(t, mr.Keys(), "test:history:index")
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

	t.Run("returns false for invalid json", func(t *testing.T) {
		history, mr := newTestRedisHistory(t)
		_ = mr.Set("test:history:entry:bad", "not-valid-json{")

		_, ok := history.GetByID(ctx, "bad")
		assert.False(t, ok)
	})
}

func TestRedisHistory_Recent(t *testing.T) {
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

		summaries := history.Recent(ctx, 10)
		assert.Len(t, summaries, 1)
		s := summaries[0]
		assert.Equal(t, "/foo/{id}", s.Resource)
		assert.Equal(t, "POST", s.Request.Method)
		assert.Equal(t, "req-1", s.Request.RequestID)
		assert.Equal(t, 201, s.Response.StatusCode)
		assert.Equal(t, 42*time.Millisecond, s.Response.Duration)
	})

	t.Run("newest first", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)
		history.Set(ctx, "/a", &HistoryRequest{Method: "GET", URL: "/a"}, nil)
		history.Set(ctx, "/b", &HistoryRequest{Method: "GET", URL: "/b"}, nil)

		summaries := history.Recent(ctx, 10)
		assert.Len(t, summaries, 2)
		assert.Equal(t, "/b", summaries[0].Resource)
		assert.Equal(t, "/a", summaries[1].Resource)
	})

	t.Run("honours limit", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)
		for i := 0; i < 5; i++ {
			history.Set(ctx, "/test", &HistoryRequest{Method: "GET", URL: "/test"}, nil)
		}

		assert.Len(t, history.Recent(ctx, 2), 2)
		assert.Nil(t, history.Recent(ctx, 0))
	})

	t.Run("empty history", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)
		assert.Empty(t, history.Recent(ctx, 10))
	})

	t.Run("skips invalid json entries", func(t *testing.T) {
		history, mr := newTestRedisHistory(t)
		history.Set(ctx, "/valid", &HistoryRequest{Method: "GET", URL: "/valid"}, nil)
		_, _ = mr.Lpush("test:history:index", "not-valid-json{")

		summaries := history.Recent(ctx, 10)
		assert.Len(t, summaries, 1)
		assert.Equal(t, "/valid", summaries[0].Resource)
	})

	t.Run("index is trimmed to the cap", func(t *testing.T) {
		history, mr := newTestRedisHistory(t)
		for i := 0; i < MaxHistoryEntries+5; i++ {
			history.Set(ctx, "/test", &HistoryRequest{Method: "GET", URL: "/test"}, nil)
		}

		length, err := mr.List("test:history:index")
		assert.NoError(t, err)
		assert.Len(t, length, MaxHistoryEntries)
		assert.Len(t, history.Recent(ctx, MaxHistoryEntries*2), MaxHistoryEntries)
	})
}

func TestRedisHistory_Recent_TTL(t *testing.T) {
	ctx := context.Background()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	history := newRedisHistoryTable(client, "test:history", 50*time.Millisecond)

	history.Set(ctx, "/old", &HistoryRequest{Method: "GET", URL: "/old"}, nil)
	assert.Len(t, history.Recent(ctx, 10), 1)

	time.Sleep(80 * time.Millisecond)
	history.Set(ctx, "/new", &HistoryRequest{Method: "GET", URL: "/new"}, nil)

	summaries := history.Recent(ctx, 10)
	assert.Len(t, summaries, 1)
	assert.Equal(t, "/new", summaries[0].Resource)
}

func TestRedisHistory_NoTTL(t *testing.T) {
	ctx := context.Background()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	history := newRedisHistoryTable(client, "test:history", 0)

	entry := history.Set(ctx, "/x", &HistoryRequest{Method: "GET", URL: "/x"}, nil)

	assert.Zero(t, mr.TTL("test:history:index"), "no TTL means the index must not expire")
	assert.Zero(t, mr.TTL(history.entryKey(entry.ID)))

	// Nothing is ever past the cutoff, however old the clock says it is.
	mr.FastForward(365 * 24 * time.Hour)
	assert.Len(t, history.Recent(ctx, 10), 1)
}

func TestRedisHistory_Recent_AllExpired(t *testing.T) {
	ctx := context.Background()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	history := newRedisHistoryTable(client, "test:history", 50*time.Millisecond)

	history.Set(ctx, "/old", &HistoryRequest{Method: "GET", URL: "/old"}, nil)
	time.Sleep(80 * time.Millisecond)

	assert.Nil(t, history.Recent(ctx, 10), "a page of nothing but expired summaries is empty")
}

func TestRedisHistory_ListAndDetailCanDisagree(t *testing.T) {
	ctx := context.Background()
	history, mr := newTestRedisHistory(t)

	entry := history.Set(ctx, "/x", &HistoryRequest{Method: "GET", URL: "/x"}, nil)
	mr.Del(history.entryKey(entry.ID))

	// The summary outlives its record, so the row lists but the detail 404s.
	assert.Len(t, history.Recent(ctx, 10), 1)
	_, ok := history.GetByID(ctx, entry.ID)
	assert.False(t, ok)
}

func TestRedisHistory_ClosedClient(t *testing.T) {
	ctx := context.Background()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	history := newRedisHistoryTable(client, "test:history", time.Minute)
	assert.NoError(t, client.Close())

	// Every operation reports nothing rather than panicking or blocking.
	assert.NotNil(t, history.Set(ctx, "/x", &HistoryRequest{Method: "GET", URL: "/x"}, nil))
	assert.Nil(t, history.Recent(ctx, 10))
	_, ok := history.GetByID(ctx, "any")
	assert.False(t, ok)
	history.Clear(ctx)
}

func TestRedisHistory_Clear(t *testing.T) {
	ctx := context.Background()

	t.Run("clears index and entries", func(t *testing.T) {
		history, mr := newTestRedisHistory(t)
		history.Set(ctx, "/x", &HistoryRequest{Method: "GET", URL: "/x"}, nil)
		history.Set(ctx, "/y", &HistoryRequest{Method: "GET", URL: "/y"}, nil)

		history.Clear(ctx)

		assert.Empty(t, history.Recent(ctx, 10))
		assert.Empty(t, mr.Keys())
	})

	t.Run("skips invalid json but still clears the rest", func(t *testing.T) {
		history, mr := newTestRedisHistory(t)
		entry := history.Set(ctx, "/x", &HistoryRequest{Method: "GET", URL: "/x"}, nil)
		_, _ = mr.Lpush("test:history:index", "not-valid-json{")

		history.Clear(ctx)

		assert.Empty(t, mr.Keys())
		_, ok := history.GetByID(ctx, entry.ID)
		assert.False(t, ok)
	})

	t.Run("clear empty history", func(t *testing.T) {
		history, _ := newTestRedisHistory(t)
		history.Clear(ctx)
	})
}

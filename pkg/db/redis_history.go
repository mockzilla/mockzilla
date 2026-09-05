package db

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisHistoryTable is a Redis-backed implementation of HistoryTable.
//
// Two keys per service hold the log:
//
//	{ns}:index      a list of summary JSON, newest first, trimmed to MaxHistoryEntries
//	{ns}:entry:{id} the full record, read only when a caller opens one entry
//
// The split is what keeps the list read cheap: Redis cannot project fields out
// of a value, so a single-key layout would drag every request and response body
// across the wire to render a body-less list. Writes pipeline into one round
// trip, and IDs are generated client-side so no sequence fetch precedes them.
type redisHistoryTable struct {
	client    *redis.Client
	namespace string // format: {service}:history
	ttl       time.Duration
}

func newRedisHistoryTable(client *redis.Client, namespace string, ttl time.Duration) *redisHistoryTable {
	return &redisHistoryTable{
		client:    client,
		namespace: namespace,
		ttl:       ttl,
	}
}

func (h *redisHistoryTable) Set(ctx context.Context, resource string, req *HistoryRequest, response *HistoryResponse) *HistoryEntry {
	entry := &HistoryEntry{
		ID:        NewHistoryID(),
		Resource:  resource,
		Request:   req,
		Response:  response,
		CreatedAt: time.Now().UTC(),
	}

	// Both hold nothing but strings, numbers, bytes and times, so neither can
	// fail to marshal.
	full, _ := json.Marshal(entry)
	summary, _ := json.Marshal(SummaryOf(entry))

	pipe := h.client.Pipeline()
	pipe.Set(ctx, h.entryKey(entry.ID), full, h.ttl)
	pipe.LPush(ctx, h.indexKey(), summary)
	pipe.LTrim(ctx, h.indexKey(), 0, MaxHistoryEntries-1)
	if h.ttl > 0 {
		// The list holds mixed-age summaries, so it expires as a whole once the
		// service goes quiet for the full TTL. Individual entries older than the
		// TTL are filtered out on read.
		pipe.Expire(ctx, h.indexKey(), h.ttl)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		slog.Error("Error saving history record", "error", err)
	}

	return entry
}

func (h *redisHistoryTable) GetByID(ctx context.Context, id string) (*HistoryEntry, bool) {
	data, err := h.client.Get(ctx, h.entryKey(id)).Bytes()
	if err != nil {
		return nil, false
	}

	var entry HistoryEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false
	}

	return &entry, true
}

func (h *redisHistoryTable) Recent(ctx context.Context, limit int) []*HistorySummary {
	if limit <= 0 {
		return nil
	}

	vals, err := h.client.LRange(ctx, h.indexKey(), 0, int64(limit-1)).Result()
	if err != nil || len(vals) == 0 {
		return nil
	}

	cutoff := h.cutoff()
	summaries := make([]*HistorySummary, 0, len(vals))
	for _, val := range vals {
		var s HistorySummary
		if err := json.Unmarshal([]byte(val), &s); err != nil {
			continue
		}
		if !cutoff.IsZero() && s.CreatedAt.Before(cutoff) {
			continue
		}
		summaries = append(summaries, &s)
	}

	if len(summaries) == 0 {
		return nil
	}
	return summaries
}

// Entry keys are derived from the index rather than scanned for, so the cost is
// bounded by MaxHistoryEntries instead of the size of the Redis keyspace. An
// entry the index has already trimmed away is left to its own TTL: nothing can
// list it, and finding it would mean the scan this avoids.
func (h *redisHistoryTable) Clear(ctx context.Context) {
	vals, err := h.client.LRange(ctx, h.indexKey(), 0, -1).Result()
	if err != nil {
		return
	}

	keys := make([]string, 0, len(vals)+1)
	keys = append(keys, h.indexKey())
	for _, val := range vals {
		var s HistorySummary
		if err := json.Unmarshal([]byte(val), &s); err != nil {
			continue
		}
		keys = append(keys, h.entryKey(s.ID))
	}

	h.client.Del(ctx, keys...)
}

func (h *redisHistoryTable) cutoff() time.Time {
	if h.ttl <= 0 {
		return time.Time{}
	}
	return time.Now().UTC().Add(-h.ttl)
}

func (h *redisHistoryTable) entryKey(id string) string {
	return h.namespace + ":entry:" + id
}

func (h *redisHistoryTable) indexKey() string {
	return h.namespace + ":index"
}

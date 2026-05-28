package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisHistoryTable is a Redis-backed implementation of HistoryTable.
type redisHistoryTable struct {
	client    *redis.Client
	namespace string // format: {service}:history
	ttl       time.Duration
}

// redisHistoryRecord is the serializable form of HistoryEntry for Redis storage.
type redisHistoryRecord struct {
	ID         string           `json:"id"`
	Resource   string           `json:"resource"`
	Method     string           `json:"method"`
	URL        string           `json:"url"`
	Body       []byte           `json:"body"`
	Headers    []string         `json:"headers,omitempty"`
	Response   *HistoryResponse `json:"response,omitempty"`
	RemoteAddr string           `json:"remoteAddr,omitempty"`
	RequestID  string           `json:"requestId,omitempty"`
	CreatedAt  time.Time        `json:"createdAt"`
}

func (r *redisHistoryRecord) toEntry() *HistoryEntry {
	return &HistoryEntry{
		ID:       r.ID,
		Resource: r.Resource,
		Response: r.Response,
		Request: &HistoryRequest{
			Method:     r.Method,
			URL:        r.URL,
			Body:       r.Body,
			Headers:    r.Headers,
			RemoteAddr: r.RemoteAddr,
			RequestID:  r.RequestID,
		},
		CreatedAt: r.CreatedAt,
	}
}

// newRedisHistoryTable creates a new Redis-backed history table.
func newRedisHistoryTable(client *redis.Client, namespace string, ttl time.Duration) *redisHistoryTable {
	return &redisHistoryTable{
		client:    client,
		namespace: namespace,
		ttl:       ttl,
	}
}

// Get retrieves the latest request record matching the HTTP request's method and URL.
func (h *redisHistoryTable) Get(ctx context.Context, req *http.Request) (*HistoryEntry, bool) {
	id, err := h.client.Get(ctx, h.latestKey(req.Method, req.URL.String())).Result()
	if errors.Is(err, redis.Nil) || err != nil {
		return nil, false
	}

	data, err := h.client.Get(ctx, h.entryKey(id)).Bytes()
	if errors.Is(err, redis.Nil) || err != nil {
		return nil, false
	}

	var record redisHistoryRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, false
	}

	return record.toEntry(), true
}

// Set stores a request record with a unique ID.
func (h *redisHistoryTable) Set(ctx context.Context, resource string, req *HistoryRequest, response *HistoryResponse) *HistoryEntry {
	id := fmt.Sprintf("%d", h.client.Incr(ctx, h.namespace+":counter").Val())

	now := time.Now().UTC()
	record := redisHistoryRecord{
		ID:         id,
		Resource:   resource,
		Method:     req.Method,
		URL:        req.URL,
		Body:       req.Body,
		Headers:    req.Headers,
		Response:   response,
		RemoteAddr: req.RemoteAddr,
		RequestID:  req.RequestID,
		CreatedAt:  now,
	}

	data, err := json.Marshal(record)
	if err != nil {
		slog.Error("Error marshaling history record", "error", err)
		return nil
	}

	// Pipeline both SETs into a single round-trip.
	pipe := h.client.Pipeline()
	pipe.Set(ctx, h.entryKey(id), data, h.ttl)
	pipe.Set(ctx, h.latestKey(req.Method, req.URL), id, h.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		slog.Error("Error saving history record", "error", err)
	}

	return &HistoryEntry{
		ID:        id,
		Resource:  resource,
		Request:   req,
		Response:  response,
		CreatedAt: now,
	}
}

// SetResponse updates the response for the latest request record matching the request's method and URL.
func (h *redisHistoryTable) SetResponse(ctx context.Context, req *HistoryRequest, response *HistoryResponse) {
	id, err := h.client.Get(ctx, h.latestKey(req.Method, req.URL)).Result()
	if errors.Is(err, redis.Nil) {
		slog.Info(fmt.Sprintf("Request for URL %s not found. Cannot set response", req.URL))
		return
	}
	if err != nil {
		slog.Error("Error getting latest ID", "error", err)
		return
	}

	entryKey := h.entryKey(id)
	data, err := h.client.Get(ctx, entryKey).Bytes()
	if err != nil {
		slog.Error("Error getting history record", "error", err)
		return
	}

	var record redisHistoryRecord
	if err := json.Unmarshal(data, &record); err != nil {
		slog.Error("Error unmarshaling history record", "error", err)
		return
	}

	record.Response = response

	newData, err := json.Marshal(record)
	if err != nil {
		slog.Error("Error marshaling history record", "error", err)
		return
	}

	h.client.Set(ctx, entryKey, newData, h.ttl)
}

// GetByID retrieves a single history entry by its ID.
func (h *redisHistoryTable) GetByID(ctx context.Context, id string) (*HistoryEntry, bool) {
	data, err := h.client.Get(ctx, h.entryKey(id)).Bytes()
	if err != nil {
		return nil, false
	}

	var record redisHistoryRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, false
	}

	return record.toEntry(), true
}

// Data returns all request records as an ordered log.
func (h *redisHistoryTable) Data(ctx context.Context) []*HistoryEntry {
	records := h.loadAllRecords(ctx)
	if len(records) == 0 {
		return nil
	}

	entries := make([]*HistoryEntry, 0, len(records))
	for _, r := range records {
		entries = append(entries, r.toEntry())
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CreatedAt.Before(entries[j].CreatedAt)
	})

	return entries
}

// Summaries returns body-less projections of all entries.
func (h *redisHistoryTable) Summaries(ctx context.Context) []*HistorySummary {
	records := h.loadAllRecords(ctx)
	if len(records) == 0 {
		return nil
	}

	summaries := make([]*HistorySummary, 0, len(records))
	for _, r := range records {
		summaries = append(summaries, SummaryOf(r.toEntry()))
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].CreatedAt.Before(summaries[j].CreatedAt)
	})

	return summaries
}

func (h *redisHistoryTable) loadAllRecords(ctx context.Context) []redisHistoryRecord {
	pattern := h.namespace + ":entry:*"

	var keys []string
	iter := h.client.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}

	if len(keys) == 0 {
		return nil
	}

	vals, err := h.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil
	}

	records := make([]redisHistoryRecord, 0, len(vals))
	for _, val := range vals {
		str, ok := val.(string)
		if !ok || str == "" {
			continue
		}
		var record redisHistoryRecord
		if err := json.Unmarshal([]byte(str), &record); err != nil {
			continue
		}
		records = append(records, record)
	}
	return records
}

// Len returns the number of history entries.
func (h *redisHistoryTable) Len(ctx context.Context) int {
	pattern := h.namespace + ":entry:*"
	var count int
	iter := h.client.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		count++
	}
	return count
}

// Clear removes all history records.
func (h *redisHistoryTable) Clear(ctx context.Context) {
	pattern := h.namespace + ":*"

	iter := h.client.Scan(ctx, 0, pattern, 0).Iterator()
	var keys []string

	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}

	if len(keys) > 0 {
		h.client.Del(ctx, keys...)
	}
}

func (h *redisHistoryTable) entryKey(id string) string {
	return h.namespace + ":entry:" + id
}

func (h *redisHistoryTable) latestKey(method, url string) string {
	return h.namespace + ":latest:" + method + ":" + url
}

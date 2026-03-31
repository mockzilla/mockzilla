package db

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// memoryHistoryTable is an in-memory implementation of HistoryTable.
// It stores entries as an ordered log with unique IDs and maintains
// an index of METHOD:URL → latest entry for fast lookups.
type memoryHistoryTable struct {
	mu          sync.RWMutex
	entries     []*HistoryEntry
	latestIndex map[string]*HistoryEntry // lookupKey → latest entry
	idIndex     map[string]*HistoryEntry // id → entry
	counter     atomic.Int64
	ttl         time.Duration
}

// newMemoryHistoryTable creates a new in-memory history table.
// ttl specifies how long each entry lives before expiring (0 means no expiry).
func newMemoryHistoryTable(_ *memoryTable, ttl time.Duration) *memoryHistoryTable {
	return &memoryHistoryTable{
		latestIndex: make(map[string]*HistoryEntry),
		idIndex:     make(map[string]*HistoryEntry),
		ttl:         ttl,
	}
}

// Get retrieves the latest non-expired request record matching the HTTP request's method and URL.
func (h *memoryHistoryTable) Get(_ context.Context, req *http.Request) (*HistoryEntry, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	entry, ok := h.latestIndex[lookupKey(req.Method, req.URL.String())]
	if !ok || h.isExpired(entry) {
		return nil, false
	}
	return entry, true
}

// Set stores a request record with a unique ID.
func (h *memoryHistoryTable) Set(_ context.Context, resource string, req *HistoryRequest, response *HistoryResponse) *HistoryEntry {
	id := fmt.Sprintf("%d", h.counter.Add(1))

	entry := &HistoryEntry{
		ID:        id,
		Resource:  resource,
		Request:   req,
		Response:  response,
		CreatedAt: time.Now().UTC(),
	}

	h.mu.Lock()
	h.entries = append(h.entries, entry)
	h.latestIndex[lookupKey(req.Method, req.URL)] = entry
	h.idIndex[id] = entry
	h.mu.Unlock()

	return entry
}

// SetResponse updates the response for the latest request record matching the request's method and URL.
func (h *memoryHistoryTable) SetResponse(_ context.Context, req *HistoryRequest, response *HistoryResponse) {
	h.mu.Lock()
	defer h.mu.Unlock()

	entry, ok := h.latestIndex[lookupKey(req.Method, req.URL)]
	if !ok {
		slog.Info(fmt.Sprintf("Request for URL %s not found. Cannot set response", req.URL))
		return
	}
	entry.Response = response
}

// GetByID retrieves a single non-expired history entry by its ID.
func (h *memoryHistoryTable) GetByID(_ context.Context, id string) (*HistoryEntry, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	entry, ok := h.idIndex[id]
	if !ok || h.isExpired(entry) {
		return nil, false
	}
	return entry, true
}

// Data returns all non-expired request records as an ordered log.
func (h *memoryHistoryTable) Data(_ context.Context) []*HistoryEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var result []*HistoryEntry
	for _, entry := range h.entries {
		if !h.isExpired(entry) {
			result = append(result, entry)
		}
	}
	return result
}

// Len returns the number of non-expired history entries.
func (h *memoryHistoryTable) Len(_ context.Context) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	count := 0
	for _, entry := range h.entries {
		if !h.isExpired(entry) {
			count++
		}
	}
	return count
}

// Clear removes all history records.
func (h *memoryHistoryTable) Clear(_ context.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries = nil
	h.latestIndex = make(map[string]*HistoryEntry)
	h.idIndex = make(map[string]*HistoryEntry)
}

func lookupKey(method, url string) string {
	return method + ":" + url
}

// isExpired returns true if the entry has expired based on the table's TTL.
func (h *memoryHistoryTable) isExpired(entry *HistoryEntry) bool {
	return h.ttl > 0 && time.Now().After(entry.CreatedAt.Add(h.ttl))
}

package db

import (
	"context"
	"sync"
	"time"
)

// memoryHistoryTable is an in-memory implementation of HistoryTable.
// Entries live in a fixed-size ring of MaxHistoryEntries: the oldest is
// evicted on overflow and dropped from the ID index, so a long-running
// service does not retain every body it ever served.
type memoryHistoryTable struct {
	mu      sync.RWMutex
	ring    []*HistoryEntry
	next    int
	size    int
	byID    map[string]*HistoryEntry
	ttl     time.Duration
	maxSize int
}

// newMemoryHistoryTable reads a ttl of 0 as entries that never expire.
func newMemoryHistoryTable(ttl time.Duration) *memoryHistoryTable {
	return &memoryHistoryTable{
		ring:    make([]*HistoryEntry, MaxHistoryEntries),
		byID:    make(map[string]*HistoryEntry, MaxHistoryEntries),
		ttl:     ttl,
		maxSize: MaxHistoryEntries,
	}
}

func (h *memoryHistoryTable) Set(_ context.Context, resource string, req *HistoryRequest, response *HistoryResponse) *HistoryEntry {
	entry := &HistoryEntry{
		ID:        NewHistoryID(),
		Resource:  resource,
		Request:   req,
		Response:  response,
		CreatedAt: time.Now().UTC(),
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if evicted := h.ring[h.next]; evicted != nil {
		delete(h.byID, evicted.ID)
	}
	h.ring[h.next] = entry
	h.byID[entry.ID] = entry
	h.next = (h.next + 1) % h.maxSize
	if h.size < h.maxSize {
		h.size++
	}

	return entry
}

func (h *memoryHistoryTable) GetByID(_ context.Context, id string) (*HistoryEntry, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	entry, ok := h.byID[id]
	if !ok || h.isExpired(entry) {
		return nil, false
	}
	return entry, true
}

func (h *memoryHistoryTable) Recent(_ context.Context, limit int) []*HistorySummary {
	if limit <= 0 {
		return nil
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make([]*HistorySummary, 0, min(limit, h.size))
	for i := 0; i < h.size && len(result) < limit; i++ {
		idx := h.next - 1 - i
		if idx < 0 {
			idx += h.maxSize
		}
		entry := h.ring[idx]
		if h.isExpired(entry) {
			continue
		}
		result = append(result, SummaryOf(entry))
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

func (h *memoryHistoryTable) Clear(_ context.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ring = make([]*HistoryEntry, h.maxSize)
	h.byID = make(map[string]*HistoryEntry, h.maxSize)
	h.next = 0
	h.size = 0
}

func (h *memoryHistoryTable) isExpired(entry *HistoryEntry) bool {
	return h.ttl > 0 && time.Now().After(entry.CreatedAt.Add(h.ttl))
}

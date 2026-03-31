package db

import (
	"sync"
	"time"

	"github.com/sony/gobreaker/v2"
)

// Ensure memoryStorage implements Storage interface.
var _ Storage = (*memoryStorage)(nil)

// memoryStorage is a shared in-memory storage backend.
type memoryStorage struct {
	mu      sync.RWMutex
	tables  map[string]*memoryTable // keyed by "serviceName:tableName"
	cbStore *memoryCircuitBreakerStore
}

// newMemoryStorage creates a new shared in-memory storage.
func newMemoryStorage() *memoryStorage {
	return &memoryStorage{
		tables:  make(map[string]*memoryTable),
		cbStore: newMemoryCircuitBreakerStore(),
	}
}

// NewDB returns a DB scoped to a specific service.
func (s *memoryStorage) NewDB(serviceName string, historyDuration time.Duration) DB {
	return &memoryServiceDB{
		storage:         s,
		serviceName:     serviceName,
		historyDuration: historyDuration,
		history:         newMemoryHistoryTable(newMemoryTable(), historyDuration),
	}
}

// Close releases resources (no-op for memory storage).
func (s *memoryStorage) Close() {
	// Nothing to close for memory storage
}

// getOrCreateTable returns an existing table or creates a new one.
func (s *memoryStorage) getOrCreateTable(fullKey string) *memoryTable {
	s.mu.RLock()
	t, ok := s.tables[fullKey]
	s.mu.RUnlock()

	if ok {
		return t
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock
	if t, ok = s.tables[fullKey]; ok {
		return t
	}

	t = newMemoryTable()
	s.tables[fullKey] = t
	return t
}

// memoryServiceDB is a service-scoped view into shared memory storage.
type memoryServiceDB struct {
	storage         *memoryStorage
	serviceName     string
	historyDuration time.Duration
	history         *memoryHistoryTable
}

// History returns the history table.
func (db *memoryServiceDB) History() HistoryTable {
	return db.history
}

// Table returns a table by name, creating it if it doesn't exist.
func (db *memoryServiceDB) Table(name string) Table {
	fullKey := db.serviceName + ":" + name
	return db.storage.getOrCreateTable(fullKey)
}

// CircuitBreakerStore returns the shared circuit breaker store.
func (db *memoryServiceDB) CircuitBreakerStore() gobreaker.SharedDataStore {
	return db.storage.cbStore
}

// Close releases resources held by this service DB.
func (db *memoryServiceDB) Close() {
}

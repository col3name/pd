package store

import (
	"context"
	"sync"
	"time"
)

// MemoryStore is an in-process bounded store with TTL. Eviction removes the
// oldest inserted entries once capacity is exceeded. Inserts are amortized
// O(1): the insertion-order slice is consumed via a cursor and compacted only
// periodically, never rescanning live entries per request.
type MemoryStore struct {
	mu       sync.Mutex
	items    map[string]*memItem
	order    []string // insertion order (append-only, consumed by cursor)
	consumed int      // count of order prefixes already evicted/expired
	ttl      time.Duration
	capacity int
}

type memItem struct {
	entry   Entry
	expires time.Time
}

// NewMemory returns a bounded in-memory store with the given TTL and capacity.
func NewMemory(ttl time.Duration, capacity int) *MemoryStore {
	if capacity <= 0 {
		capacity = 1 << 16
	}
	return &MemoryStore{items: make(map[string]*memItem, capacity), ttl: ttl, capacity: capacity}
}

func (s *MemoryStore) Save(_ context.Context, id string, e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[id] = &memItem{entry: e, expires: time.Now().Add(s.ttl)}
	s.order = append(s.order, id)
	s.evictLocked()
	return nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[id]
	if !ok {
		return Entry{}, false, nil
	}
	if time.Now().After(it.expires) {
		delete(s.items, id)
		return Entry{}, false, nil
	}
	return it.entry, true, nil
}

// evictLocked drives len(items) down to capacity in FIFO order. The cursor
// `consumed` marks how many prefix entries of order no longer matter (evicted,
// expired, or shadowed by a duplicate id), so each such entry is examined once
// across the store's lifetime. Compaction is amortized: it only runs once
// consumed exceeds half the order length.
func (s *MemoryStore) evictLocked() {
	if len(s.items) <= s.capacity {
		s.compactLocked()
		return
	}
	now := time.Now()
	for len(s.items) > s.capacity && s.consumed < len(s.order) {
		id := s.order[s.consumed]
		s.consumed++
		it, ok := s.items[id]
		if !ok || now.After(it.expires) {
			delete(s.items, id)
			continue // was already evicted/expired; cursor advanced past it
		}
		delete(s.items, id) // oldest live entry
	}
	s.compactLocked()
}

// compactLocked drops the consumed prefix of order to bound memory.
func (s *MemoryStore) compactLocked() {
	if s.consumed > len(s.order)/2 && s.consumed > 1024 {
		s.order = append([]string(nil), s.order[s.consumed:]...)
		s.consumed = 0
	}
}
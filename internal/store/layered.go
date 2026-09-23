package store

import (
	"context"
	"log/slog"
)

// LayeredStore persists entries to Redis (source of truth) and a local
// in-memory cache (fast path + fallback). A circuit breaker guards Redis: when
// it is open, Save writes only to the local cache and Get serves only local
// hits. Masking never fails; unmask degrades to local-only.
type LayeredStore struct {
	redis   *RedisStore
	local   *MemoryStore
	breaker *Breaker
}

func NewLayered(redis *RedisStore, local *MemoryStore, breaker *Breaker) *LayeredStore {
	return &LayeredStore{redis: redis, local: local, breaker: breaker}
}

// RedisUp reports whether the breaker is closed (Redis reachable).
func (s *LayeredStore) RedisUp() bool { return s.breaker.State() }

func (s *LayeredStore) Save(ctx context.Context, id string, e Entry) error {
	// Always write to local cache first (fast path + fallback).
	if err := s.local.Save(ctx, id, e); err != nil {
		return err
	}
	if !s.breaker.Allow() {
		return nil // Redis open; local write is enough
	}
	if err := s.redis.Save(ctx, id, e); err != nil {
		s.breaker.Failure()
		slog.Warn("layered: redis save failed", "error", err)
		return nil // masking must not fail
	}
	s.breaker.Success()
	return nil
}

func (s *LayeredStore) Get(ctx context.Context, id string) (Entry, bool, error) {
	if e, ok, err := s.local.Get(ctx, id); err == nil && ok {
		return e, true, nil
	}
	if !s.breaker.Allow() {
		return Entry{}, false, nil // Redis open; no local hit
	}
	e, ok, err := s.redis.Get(ctx, id)
	if err != nil {
		s.breaker.Failure()
		slog.Warn("layered: redis get failed", "error", err)
		return Entry{}, false, nil
	}
	s.breaker.Success()
	if ok {
		_ = s.local.Save(ctx, id, e) // populate local cache
	}
	return e, ok, nil
}
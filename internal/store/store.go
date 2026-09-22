package store

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Store persists original payloads keyed by payload_id for unmasking.
type Store struct {
	client *redis.Client
	ttl    time.Duration
}

// New returns a Store with the given TTL for saved keys.
func New(client *redis.Client, ttl time.Duration) *Store {
	return &Store{client: client, ttl: ttl}
}

// Save stores text under id with the configured TTL.
func (s *Store) Save(ctx context.Context, id, text string) error {
	return s.client.Set(ctx, id, text, s.ttl).Err()
}

// Get returns the text stored under id. ok is false when the key is absent.
func (s *Store) Get(ctx context.Context, id string) (string, bool, error) {
	val, err := s.client.Get(ctx, id).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

// Ping checks Redis connectivity.
func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}
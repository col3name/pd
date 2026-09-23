package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStore persists entries in Redis with a TTL.
type RedisStore struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedis returns a Redis-backed store.
func NewRedis(client *redis.Client, ttl time.Duration) *RedisStore {
	return &RedisStore{client: client, ttl: ttl}
}

func (s *RedisStore) Save(ctx context.Context, id string, e Entry) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, id, data, s.ttl).Err()
}

func (s *RedisStore) Get(ctx context.Context, id string) (Entry, bool, error) {
	data, err := s.client.Get(ctx, id).Bytes()
	if err == redis.Nil {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, err
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return Entry{}, false, err
	}
	return e, true, nil
}
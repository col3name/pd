package store

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newLayered(t *testing.T) (*LayeredStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	redisStore := NewRedis(client, time.Hour)
	local := NewMemory(time.Hour, 1000)
	breaker := NewBreaker(1, time.Minute)
	return NewLayered(redisStore, local, breaker), mr
}

func TestLayeredSaveGet(t *testing.T) {
	s, _ := newLayered(t)
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, "id-1", Entry{Original: "orig"}))
	got, ok, err := s.Get(ctx, "id-1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "orig", got.Original)
}

func TestLayeredRedisDownMaskStillWorks(t *testing.T) {
	s, mr := newLayered(t)
	ctx := context.Background()
	// Save while Redis is up.
	require.NoError(t, s.Save(ctx, "id-2", Entry{Original: "orig2"}))
	// Kill Redis.
	mr.Close()
	// Mask path: Save must not error (falls back to local cache).
	require.NoError(t, s.Save(ctx, "id-3", Entry{Original: "orig3"}))
	// Unmask path: local cache hit still works.
	got, ok, err := s.Get(ctx, "id-2")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "orig2", got.Original)
	require.False(t, s.RedisUp())
}

func TestLayeredRedisDownNoLocalMiss(t *testing.T) {
	s, mr := newLayered(t)
	ctx := context.Background()
	// Save to Redis only (bypass local by using a fresh layered store sharing
	// the same Redis but empty local).
	redisStore := NewRedis(redis.NewClient(&redis.Options{Addr: mr.Addr()}), time.Hour)
	require.NoError(t, redisStore.Save(ctx, "id-4", Entry{Original: "remote"}))
	mr.Close()
	// Local cache empty + Redis down -> miss with ErrRedisUnavailable.
	_, ok, err := s.Get(ctx, "id-4")
	require.ErrorIs(t, err, ErrRedisUnavailable)
	require.False(t, ok)
}

func TestLayeredRedisDownNoLocalHitReturnsErrRedisUnavailable(t *testing.T) {
	s, mr := newLayered(t)
	ctx := context.Background()
	// No entry anywhere; kill Redis so the breaker opens.
	mr.Close()
	// Local cache empty + Redis down -> ErrRedisUnavailable.
	_, ok, err := s.Get(ctx, "missing")
	require.ErrorIs(t, err, ErrRedisUnavailable)
	require.False(t, ok)
	require.False(t, s.RedisUp())
}
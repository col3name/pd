package store

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) (*Store, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return New(client, time.Hour), mr
}

func TestSaveGet(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, "id-1", "original text"))
	got, ok, err := s.Get(ctx, "id-1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "original text", got)
}

func TestGetUnknown(t *testing.T) {
	s, _ := newTestStore(t)
	_, ok, err := s.Get(context.Background(), "missing")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestTTL(t *testing.T) {
	s, mr := newTestStore(t)
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, "id-ttl", "x"))
	require.True(t, mr.TTL("id-ttl") > 0)
}
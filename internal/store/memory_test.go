package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMemorySaveGet(t *testing.T) {
	s := NewMemory(time.Hour, 100)
	require.NoError(t, s.Save(context.Background(), "id-1", Entry{Original: "паспорт 4509 123456"}))
	got, ok, err := s.Get(context.Background(), "id-1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "паспорт 4509 123456", got.Original)
}

func TestMemoryMissing(t *testing.T) {
	s := NewMemory(time.Hour, 100)
	_, ok, err := s.Get(context.Background(), "nope")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestMemoryExpiry(t *testing.T) {
	s := NewMemory(50*time.Millisecond, 100)
	require.NoError(t, s.Save(context.Background(), "id-1", Entry{Original: "x"}))
	time.Sleep(100 * time.Millisecond)
	_, ok, err := s.Get(context.Background(), "id-1")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestMemoryCapacityEvictsOldest(t *testing.T) {
	s := NewMemory(time.Hour, 2)
	require.NoError(t, s.Save(context.Background(), "a", Entry{Original: "1"}))
	require.NoError(t, s.Save(context.Background(), "b", Entry{Original: "2"}))
	require.NoError(t, s.Save(context.Background(), "c", Entry{Original: "3"}))
	_, ok, _ := s.Get(context.Background(), "a")
	require.False(t, ok, "first inserted must be evicted at capacity")
}

func TestMemoryConcurrent(t *testing.T) {
	s := NewMemory(time.Hour, 1000)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = s.Save(context.Background(), string(rune('a'+i%5)), Entry{Original: "x"})
			_, _, _ = s.Get(context.Background(), string(rune('a'+i%5)))
		}(i)
	}
	wg.Wait()
}
package ratelimit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAllowWithinRate(t *testing.T) {
	l := New(100, 100)
	for i := 0; i < 100; i++ {
		ok, _ := l.Allow()
		require.True(t, ok, "request %d should be allowed", i)
	}
	// Bucket exhausted.
	ok, retryAfter := l.Allow()
	require.False(t, ok)
	require.GreaterOrEqual(t, retryAfter, time.Second)
}

func TestRefill(t *testing.T) {
	l := New(10, 10)
	// Drain the bucket.
	for i := 0; i < 10; i++ {
		l.Allow()
	}
	ok, _ := l.Allow()
	require.False(t, ok)
	// Wait for refill.
	time.Sleep(200 * time.Millisecond)
	ok, _ = l.Allow()
	require.True(t, ok, "should refill after waiting")
}

func TestBurst(t *testing.T) {
	// rate 10/sec, burst 50 -> allows 50 immediately.
	l := New(10, 50)
	for i := 0; i < 50; i++ {
		ok, _ := l.Allow()
		require.True(t, ok, "burst request %d", i)
	}
	ok, _ := l.Allow()
	require.False(t, ok)
}
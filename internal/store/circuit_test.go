package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBreakerOpensAfterFailures(t *testing.T) {
	b := NewBreaker(3, time.Minute)
	require.True(t, b.Allow())
	b.Failure()
	b.Failure()
	require.True(t, b.Allow())
	b.Failure()
	require.False(t, b.Allow(), "breaker must open after 3 failures")
	require.False(t, b.State())
}

func TestBreakerRecoversAfterCooldown(t *testing.T) {
	b := NewBreaker(2, 50*time.Millisecond)
	b.Failure()
	b.Failure()
	require.False(t, b.Allow())
	time.Sleep(60 * time.Millisecond)
	require.True(t, b.Allow(), "breaker must close after cooldown")
	b.Success()
	require.True(t, b.State())
}

package db

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAuthFlow(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()

	require.NoError(t, r.SeedAdmin(ctx, "admin", "secret123"))

	ok, err := r.VerifyAdmin(ctx, "admin", "secret123")
	require.NoError(t, err)
	require.True(t, ok)

	ok, _ = r.VerifyAdmin(ctx, "admin", "wrong")
	require.False(t, ok)

	tokenHash := "abc123hash"
	require.NoError(t, r.CreateSession(ctx, tokenHash, "admin", time.Hour))
	valid, err := r.ValidateSession(ctx, tokenHash)
	require.NoError(t, err)
	require.True(t, valid)

	require.NoError(t, r.DeleteSession(ctx, tokenHash))
	valid, _ = r.ValidateSession(ctx, tokenHash)
	require.False(t, valid)
}
package db

import (
	"context"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// SeedAdmin inserts an admin with a bcrypt-hashed password if the login does
// not already exist. Used to seed the default admin from YAML on first start.
func (r *Repo) SeedAdmin(ctx context.Context, login, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO admins (login, password_hash) VALUES ($1,$2) ON CONFLICT (login) DO NOTHING`,
		login, string(hash))
	return err
}

// VerifyAdmin checks a login/password against the stored bcrypt hash.
func (r *Repo) VerifyAdmin(ctx context.Context, login, password string) (bool, error) {
	var hash string
	err := r.pool.QueryRow(ctx, `SELECT password_hash FROM admins WHERE login=$1`, login).Scan(&hash)
	if err != nil {
		return false, nil // no such admin
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return false, nil
	}
	return true, nil
}

// CreateSession stores a token hash with an expiry.
func (r *Repo) CreateSession(ctx context.Context, tokenHash, login string, ttl time.Duration) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO sessions (token_hash, login, expires_at) VALUES ($1,$2,$3)`,
		tokenHash, login, time.Now().Add(ttl))
	return err
}

// ValidateSession reports whether a token hash is present and unexpired.
func (r *Repo) ValidateSession(ctx context.Context, tokenHash string) (bool, error) {
	var expires time.Time
	err := r.pool.QueryRow(ctx,
		`SELECT expires_at FROM sessions WHERE token_hash=$1`, tokenHash).Scan(&expires)
	if err != nil {
		return false, nil
	}
	return time.Now().Before(expires), nil
}

// DeleteSession removes a token.
func (r *Repo) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, tokenHash)
	return err
}

// CleanupSessions removes expired sessions.
func (r *Repo) CleanupSessions(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`)
	return err
}
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo is the PostgreSQL repository for systems, rules, combinations and auth.
type Repo struct {
	pool *pgxpool.Pool
}

// New connects to Postgres, runs migrations and returns a Repo.
func New(ctx context.Context, dsn string) (*Repo, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	r := &Repo{pool: pool}
	if err := r.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return r, nil
}

// Close releases the connection pool.
func (r *Repo) Close() error {
	r.pool.Close()
	return nil
}

func (r *Repo) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS systems (
			name          text PRIMARY KEY,
			api_key_hash  text NOT NULL DEFAULT '',
			enabled       boolean NOT NULL DEFAULT true,
			allow_unmask  boolean NOT NULL DEFAULT false,
			require_key   boolean NOT NULL DEFAULT false,
			masking       text NOT NULL DEFAULT '',
			pii           text[] NOT NULL DEFAULT '{}',
			created_at    timestamptz NOT NULL DEFAULT now(),
			updated_at    timestamptz NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS rules (
			id          serial PRIMARY KEY,
			type        text NOT NULL,
			regex       text NOT NULL DEFAULT '',
			priority    int  NOT NULL DEFAULT 0,
			context     text NOT NULL DEFAULT '',
			capture     text NOT NULL DEFAULT '',
			keyword     text NOT NULL DEFAULT '',
			confidence  real NOT NULL DEFAULT 0.99
		)`,
		`CREATE TABLE IF NOT EXISTS combinations (
			id        serial PRIMARY KEY,
			type      text NOT NULL,
			requires  text[] NOT NULL,
			"window"  int NOT NULL DEFAULT 80
		)`,
		`CREATE TABLE IF NOT EXISTS admins (
			login         text PRIMARY KEY,
			password_hash text NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			token_hash text PRIMARY KEY,
			login      text NOT NULL REFERENCES admins(login),
			expires_at timestamptz NOT NULL
		)`,
		`ALTER TABLE systems ADD COLUMN IF NOT EXISTS require_key boolean NOT NULL DEFAULT false`,
	}
	for _, s := range stmts {
		if _, err := r.pool.Exec(ctx, s); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

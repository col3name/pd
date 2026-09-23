package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/kind-earthquake/pii-module/internal/config"
	"github.com/kind-earthquake/pii-module/internal/detector"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ListSystems returns all consumer systems.
func (r *Repo) ListSystems(ctx context.Context) ([]config.SystemConfig, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT name, api_key_hash, enabled, allow_unmask, masking, pii FROM systems ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []config.SystemConfig
	for rows.Next() {
		var s config.SystemConfig
		var pii []string
		if err := rows.Scan(&s.Name, &s.APIKey, &s.Enabled, &s.AllowUnmask, &s.Masking, &pii); err != nil {
			return nil, err
		}
		s.PII = toTypes(pii)
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetSystem returns one system by name.
func (r *Repo) GetSystem(ctx context.Context, name string) (*config.SystemConfig, error) {
	var s config.SystemConfig
	var pii []string
	err := r.pool.QueryRow(ctx,
		`SELECT name, api_key_hash, enabled, allow_unmask, masking, pii FROM systems WHERE name=$1`, name).
		Scan(&s.Name, &s.APIKey, &s.Enabled, &s.AllowUnmask, &s.Masking, &pii)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.PII = toTypes(pii)
	return &s, nil
}

// CreateSystem inserts a new system.
func (r *Repo) CreateSystem(ctx context.Context, s config.SystemConfig) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO systems (name, api_key_hash, enabled, allow_unmask, masking, pii)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		s.Name, s.APIKey, s.Enabled, s.AllowUnmask, s.Masking, fromTypes(s.PII))
	return err
}

// UpdateSystem updates an existing system by name.
func (r *Repo) UpdateSystem(ctx context.Context, name string, s config.SystemConfig) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE systems SET enabled=$2, allow_unmask=$3, masking=$4, pii=$5, updated_at=now() WHERE name=$1`,
		name, s.Enabled, s.AllowUnmask, s.Masking, fromTypes(s.PII))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteSystem removes a system by name.
func (r *Repo) DeleteSystem(ctx context.Context, name string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM systems WHERE name=$1`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetAPIKeyHash updates only the api_key_hash column.
func (r *Repo) SetAPIKeyHash(ctx context.Context, name, hash string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE systems SET api_key_hash=$2, updated_at=now() WHERE name=$1`, name, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func toTypes(in []string) []detector.Type {
	out := make([]detector.Type, 0, len(in))
	for _, s := range in {
		out = append(out, detector.Type(s))
	}
	return out
}

func fromTypes(in []detector.Type) []string {
	out := make([]string, 0, len(in))
	for _, t := range in {
		out = append(out, string(t))
	}
	return out
}
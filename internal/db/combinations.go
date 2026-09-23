package db

import (
	"context"

	"github.com/kind-earthquake/pii-module/internal/config"
)

// ListCombinations returns all combination rules.
func (r *Repo) ListCombinations(ctx context.Context) ([]config.CombinationConfig, error) {
	rows, err := r.pool.Query(ctx, `SELECT type, requires, window FROM combinations ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []config.CombinationConfig
	for rows.Next() {
		var c config.CombinationConfig
		var req []string
		if err := rows.Scan(&c.Type, &req, &c.Window); err != nil {
			return nil, err
		}
		c.Requires = toTypes(req)
		out = append(out, c)
	}
	return out, rows.Err()
}
package db

import (
	"context"

	"github.com/kind-earthquake/pii-module/internal/config"
)

// ListRules returns all overlay rules.
func (r *Repo) ListRules(ctx context.Context) ([]config.RuleConfig, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT type, regex, priority, context, capture, keyword, confidence FROM rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []config.RuleConfig
	for rows.Next() {
		var rc config.RuleConfig
		if err := rows.Scan(&rc.Type, &rc.Regex, &rc.Priority, &rc.Context, &rc.Capture, &rc.Keyword, &rc.Confidence); err != nil {
			return nil, err
		}
		out = append(out, rc)
	}
	return out, rows.Err()
}
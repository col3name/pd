package db

import (
	"context"
	"fmt"

	"github.com/kind-earthquake/pii-module/internal/config"
)

// SeedFromConfig seeds empty tables from the YAML config. It is a no-op for
// any table that already has rows, so the DB becomes the source of truth
// after the first start.
func (r *Repo) SeedFromConfig(ctx context.Context, cfg *config.Config) error {
	if err := r.seedSystems(ctx, cfg.Systems); err != nil {
		return err
	}
	if err := r.seedRules(ctx, cfg.Rules); err != nil {
		return err
	}
	return r.seedCombinations(ctx, cfg.Combinations)
}

func (r *Repo) seedSystems(ctx context.Context, systems []config.SystemConfig) error {
	var n int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM systems`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	for _, s := range systems {
		if err := r.CreateSystem(ctx, s); err != nil {
			return fmt.Errorf("seed system %s: %w", s.Name, err)
		}
	}
	return nil
}

func (r *Repo) seedRules(ctx context.Context, rules []config.RuleConfig) error {
	var n int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM rules`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	for _, rc := range rules {
		if _, err := r.pool.Exec(ctx,
			`INSERT INTO rules (type, regex, priority, context, capture, keyword, confidence)
			 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			rc.Type, rc.Regex, rc.Priority, rc.Context, rc.Capture, rc.Keyword, rc.Confidence); err != nil {
			return fmt.Errorf("seed rule %s: %w", rc.Type, err)
		}
	}
	return nil
}

func (r *Repo) seedCombinations(ctx context.Context, combos []config.CombinationConfig) error {
	var n int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM combinations`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	for _, c := range combos {
		if _, err := r.pool.Exec(ctx,
			`INSERT INTO combinations (type, requires, window) VALUES ($1,$2,$3)`,
			c.Type, fromTypes(c.Requires), c.Window); err != nil {
			return fmt.Errorf("seed combination %s: %w", c.Type, err)
		}
	}
	return nil
}
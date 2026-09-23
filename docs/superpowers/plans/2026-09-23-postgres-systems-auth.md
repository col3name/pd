# PostgreSQL Systems + Admin Auth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move consumer systems (and rules/combinations) storage from YAML to PostgreSQL, add CRUD for systems with one-time accessKey generation (SHA-256 hash stored), and add login/password auth for the admin UI (session token).

**Architecture:** New `internal/db` package (pgx/v5) owns migrations, YAML seed, systems CRUD, rules/combinations reads, and auth (admins + sessions). `control.Manager` gains a `Reload()` method that re-reads systems/rules/combinations from the DB and rebuilds pipelines; `Apply(cfg)` is removed. New REST endpoints: `/v1/auth/login|logout`, `/v1/systems` CRUD, `/v1/config` (read-only). The React UI is reduced to a login screen + Systems tab.

**Tech Stack:** Go 1.25, chi/v5, pgx/v5, golang.org/x/crypto/bcrypt, React 18 + TanStack Query + TelegramUI.

## Global Constraints

- Module path: `github.com/kind-earthquake/pii-module`; Go 1.25.0.
- Tests use `github.com/stretchr/testify/require`.
- Git identity for commits: `git -c user.name="col3name" -c user.email="col3name@users.noreply.github.com" commit -m "..."`
- `CGO_ENABLED=0 go build ./...` must pass (Docker build path). NER is cgo-gated already.
- Do NOT change: detectors, pipeline, resolve, context, whitelist, masker, NER, payload store (memory/redis), `/process` masking logic.
- Access keys: random 32 hex chars, shown once, stored as `sha256` hex.
- Admin session token: random 32 hex chars, stored as `sha256` hex in `sessions`, TTL 24h.
- Admin password: bcrypt hash in `admins`.
- YAML is seed-only: if a table is empty at startup, seed from `config.yaml`; DB is source of truth afterward.
- UI: only Systems tab + login screen. Remove Rules/Combinations/Config tabs and their API (`PUT /v1/config`, `GET /v1/config/rules`, `X-Admin-Key`).

---

### Task 1: Add pgx + bcrypt deps and DatabaseConfig

**Files:**
- Modify: `go.mod`, `go.sum`
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.DatabaseConfig{DSN string}` field on `config.Config`; `config.AdminConfig` gains `Login` and `Password` fields.

- [ ] **Step 1: Add dependencies**

Run:
```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
go get github.com/jackc/pgx/v5@latest
go get golang.org/x/crypto/bcrypt@latest
```
Expected: go.mod/go.sum updated.

- [ ] **Step 2: Add DatabaseConfig and extend AdminConfig**

In `internal/config/config.go`, add after `StoreConfig`:

```go
// DatabaseConfig controls the PostgreSQL connection for systems/rules/combos.
type DatabaseConfig struct {
	DSN string `yaml:"dsn"`
}
```

Change `AdminConfig` to:

```go
// AdminConfig guards the admin UI/API (login/password, seeded to DB).
type AdminConfig struct {
	Key      string `yaml:"key"`
	Login    string `yaml:"login"`
	Password string `yaml:"password"`
}
```

Add `Database DatabaseConfig` field to `Config` struct (after `Store`):

```go
	Store          StoreConfig         `yaml:"store"`
	Database       DatabaseConfig      `yaml:"database"`
```

In `Default()`, add defaults:

```go
		Admin:          AdminConfig{Key: "pii-admin-key", Login: "admin", Password: "admin123"},
```

- [ ] **Step 3: Write the test**

In `internal/config/config_test.go`, add:

```go
func TestDefaultAdminAndDatabase(t *testing.T) {
	cfg := Default()
	require.Equal(t, "admin", cfg.Admin.Login)
	require.Equal(t, "admin123", cfg.Admin.Password)
	require.Equal(t, "", cfg.Database.DSN)
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go test ./internal/config/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add go.mod go.sum internal/config/config.go internal/config/config_test.go
git -c user.name="col3name" -c user.email="col3name@users.noreply.github.com" commit -m "config: add database dsn and admin login/password fields"
```

---

### Task 2: internal/db package — migrations, seed, systems CRUD

**Files:**
- Create: `internal/db/db.go`, `internal/db/systems.go`, `internal/db/rules.go`, `internal/db/combinations.go`, `internal/db/seed.go`
- Test: `internal/db/db_test.go`, `internal/db/systems_test.go`

**Interfaces:**
- Consumes: `config.Config` (for seed), `config.SystemConfig`, `config.RuleConfig`, `config.CombinationConfig`.
- Produces:
  - `db.New(ctx, dsn string) (*Repo, error)` — connects, migrates, returns Repo.
  - `(*Repo).Close() error`
  - `(*Repo).SeedFromConfig(cfg *config.Config) error` — seeds empty tables.
  - `(*Repo).ListSystems() ([]config.SystemConfig, error)`
  - `(*Repo).GetSystem(name string) (*config.SystemConfig, error)`
  - `(*Repo).CreateSystem(s config.SystemConfig) error`
  - `(*Repo).UpdateSystem(name string, s config.SystemConfig) error`
  - `(*Repo).DeleteSystem(name string) error`
  - `(*Repo).ListRules() ([]config.RuleConfig, error)`
  - `(*Repo).ListCombinations() ([]config.CombinationConfig, error)`
  - `(*Repo).SetAPIKeyHash(name, hash string) error`

- [ ] **Step 1: Write the failing test (systems CRUD)**

Create `internal/db/systems_test.go`:

```go
package db

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/config"
)

// newTestRepo connects to a real Postgres. Skip if PII_TEST_DSN is unset.
func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	dsn := tEnv("PII_TEST_DSN", "postgres://pii:pii@localhost:5432/pii?sslmode=disable")
	ctx := context.Background()
	r, err := New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func tEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func TestSystemsCRUD(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	_ = r.DeleteSystem(ctx, "test-sys")

	err := r.CreateSystem(ctx, config.SystemConfig{Name: "test-sys", Enabled: true, AllowUnmask: true, Masking: "token", PII: []detector.Type{detector.TypePhone}})
	require.NoError(t, err)

	s, err := r.GetSystem(ctx, "test-sys")
	require.NoError(t, err)
	require.Equal(t, "test-sys", s.Name)
	require.True(t, s.Enabled)
	require.True(t, s.AllowUnmask)
	require.Equal(t, "token", s.Masking)
	require.Equal(t, []detector.Type{detector.TypePhone}, s.PII)

	err = r.UpdateSystem(ctx, "test-sys", config.SystemConfig{Name: "test-sys", Enabled: false})
	require.NoError(t, err)
	s2, _ := r.GetSystem(ctx, "test-sys")
	require.False(t, s2.Enabled)

	err = r.DeleteSystem(ctx, "test-sys")
	require.NoError(t, err)
	_, err = r.GetSystem(ctx, "test-sys")
	require.Error(t, err)
}
```

Note: this test needs `os` and `detector` imports. Add them.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go test ./internal/db/`
Expected: FAIL — package `db` does not exist.

- [ ] **Step 3: Write db.go (connection + migrations)**

Create `internal/db/db.go`:

```go
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
			window    int NOT NULL DEFAULT 80
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
	}
	for _, s := range stmts {
		if _, err := r.pool.Exec(ctx, s); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}
```

- [ ] **Step 4: Write systems.go**

Create `internal/db/systems.go`:

```go
package db

import (
	"context"
	"errors"
	"fmt"

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
```

- [ ] **Step 5: Write rules.go and combinations.go**

Create `internal/db/rules.go`:

```go
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
```

Create `internal/db/combinations.go`:

```go
package db

import (
	"context"

	"github.com/kind-earthquake/pii-module/internal/config"
	"github.com/kind-earthquake/pii-module/internal/detector"
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
```

- [ ] **Step 6: Write seed.go**

Create `internal/db/seed.go`:

```go
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
```

- [ ] **Step 7: Run tests**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go test ./internal/db/`
Expected: PASS if `PII_TEST_DSN` points at a running Postgres; otherwise SKIP (tests skip when DB unreachable — see note).

Note: The test in Step 1 calls `New` which pings Postgres. If no Postgres is running, `New` returns an error and the test FAILS. To make tests skip gracefully, wrap `newTestRepo` to skip on connection error:

```go
func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	dsn := tEnv("PII_TEST_DSN", "postgres://pii:pii@localhost:5432/pii?sslmode=disable")
	ctx := context.Background()
	r, err := New(ctx, dsn)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}
```

- [ ] **Step 8: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add internal/db/
git -c user.name="col3name" -c user.email="col3name@users.noreply.github.com" commit -m "db: postgres repo with migrations, seed and systems/rules/combinations"
```

---

### Task 3: internal/db auth — admins + sessions

**Files:**
- Create: `internal/db/auth.go`
- Test: `internal/db/auth_test.go`

**Interfaces:**
- Consumes: `config.AdminConfig` (seed), `db.Repo`.
- Produces:
  - `(*Repo).SeedAdmin(ctx, login, password string) error` — bcrypt-hash and insert if not exists.
  - `(*Repo).VerifyAdmin(ctx, login, password string) (bool, error)` — bcrypt compare.
  - `(*Repo).CreateSession(ctx, tokenHash, login string, ttl time.Duration) error`
  - `(*Repo).ValidateSession(ctx, tokenHash string) (bool, error)` — true if token valid and not expired.
  - `(*Repo).DeleteSession(ctx, tokenHash string) error`
  - `(*Repo).CleanupSessions(ctx) error` — delete expired.

- [ ] **Step 1: Write the failing test**

Create `internal/db/auth_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go test ./internal/db/ -run TestAuthFlow`
Expected: FAIL — `SeedAdmin` undefined.

- [ ] **Step 3: Write auth.go**

Create `internal/db/auth.go`:

```go
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
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go test ./internal/db/`
Expected: PASS (or SKIP if no Postgres).

- [ ] **Step 5: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add internal/db/auth.go internal/db/auth_test.go
git -c user.name="col3name" -c user.email="col3name@users.noreply.github.com" commit -m "db: admin auth with bcrypt and session tokens"
```

---

### Task 4: Manager refactor — Reload() from DB, remove Apply()

**Files:**
- Modify: `internal/control/manager.go`
- Test: `internal/control/manager_test.go`

**Interfaces:**
- Consumes: `db.Repo` (ListSystems, ListRules, ListCombinations).
- Produces:
  - `control.WithRepo(repo *db.Repo) Option`
  - `(*Manager).Reload() error` — re-reads systems/rules/combos from repo, rebuilds, bumps rev.
  - `(*Manager).System(name string) *config.SystemConfig` — now reads from in-memory cache (name → SystemConfig).
  - `(*Manager).SystemHash(name string) string` — returns api_key_hash for a system (for /process auth).
  - `(*Manager).KnownTypes() []string` — unchanged behavior but reads rules from repo cache.

- [ ] **Step 1: Write the failing test**

In `internal/control/manager_test.go`, add:

```go
func TestManagerReloadFromRepo(t *testing.T) {
	// Uses a fake repo via an interface to avoid needing Postgres.
	// See Step 3 for the interface.
}
```

Note: To test without Postgres, define a minimal interface in the test:

```go
type fakeRepo struct {
	systems []config.SystemConfig
	rules   []config.RuleConfig
	combos  []config.CombinationConfig
}

func (f *fakeRepo) ListSystems(ctx context.Context) ([]config.SystemConfig, error) { return f.systems, nil }
func (f *fakeRepo) ListRules(ctx context.Context) ([]config.RuleConfig, error)     { return f.rules, nil }
func (f *fakeRepo) ListCombinations(ctx context.Context) ([]config.CombinationConfig, error) { return f.combos, nil }
```

But `Manager` holds a concrete `*db.Repo`. To make it testable, define a small interface in `control`:

```go
// SystemSource is the subset of db.Repo the Manager needs.
type SystemSource interface {
	ListSystems(ctx context.Context) ([]config.SystemConfig, error)
	ListRules(ctx context.Context) ([]config.RuleConfig, error)
	ListCombinations(ctx context.Context) ([]config.CombinationConfig, error)
}
```

`Manager.repo` becomes `SystemSource`. `db.Repo` satisfies it implicitly.

Write the full test:

```go
func TestManagerReloadFromRepo(t *testing.T) {
	ctx := context.Background()
	repo := &fakeRepo{
		systems: []config.SystemConfig{
			{Name: "chat", Enabled: true, AllowUnmask: true, Masking: "token"},
		},
	}
	m, err := New(WithConfig(config.Default()), WithStore(store.NewMemory(time.Hour, 1000)), WithRepo(repo))
	require.NoError(t, err)

	// System present after New (buildLocked reads repo).
	s := m.System("chat")
	require.NotNil(t, s)
	require.True(t, s.Enabled)

	// Reload picks up a change.
	repo.systems[0].Enabled = false
	require.NoError(t, m.Reload())
	s2 := m.System("chat")
	require.NotNil(t, s2)
	require.False(t, s2.Enabled)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go test ./internal/control/ -run TestManagerReloadFromRepo`
Expected: FAIL — `WithRepo` undefined.

- [ ] **Step 3: Refactor manager.go**

In `internal/control/manager.go`:

Add import `"context"` and `"github.com/kind-earthquake/pii-module/internal/config"` (already imported).

Add the `SystemSource` interface and change `Manager`:

```go
// SystemSource is the subset of db.Repo the Manager needs to build pipelines.
type SystemSource interface {
	ListSystems(ctx context.Context) ([]config.SystemConfig, error)
	ListRules(ctx context.Context) ([]config.RuleConfig, error)
	ListCombinations(ctx context.Context) ([]config.CombinationConfig, error)
}

type Manager struct {
	mu           sync.RWMutex
	cfgPath      string
	cfg          *config.Config
	repo         SystemSource
	store        store.Store
	detectorOpts []detector.Option
	detector     *detector.Detector
	context      *context.Resolver
	whitelist    *whitelist.Whitelist
	base         *pipeline.Pipeline
	systems      map[string]*pipeline.Pipeline
	systemCfg    map[string]config.SystemConfig // name -> config for auth
	limiter      *ratelimit.Limiter
	rev          uint64
}
```

Add option:

```go
// WithRepo sets the system/rules/combinations source (Postgres).
func WithRepo(repo SystemSource) Option {
	return func(m *Manager) { m.repo = repo }
}
```

In `New`, initialize `systemCfg`:

```go
	m := &Manager{systems: make(map[string]*pipeline.Pipeline), systemCfg: make(map[string]config.SystemConfig), cfgPath: defaultConfigPath()}
```

In `buildLocked`, replace the systems/rules/combos reading from `m.cfg` with repo reads:

```go
func (m *Manager) buildLocked() error {
	ctxR := context.New(nil, nil)
	if m.cfg.Context.Enabled {
		ctxR = context.New(m.cfg.Context.Boost, m.cfg.Context.Penalty)
	}
	var w *whitelist.Whitelist
	if m.cfg.Whitelist.Enabled {
		w = whitelist.New(m.cfg.Whitelist.Persons, m.cfg.Whitelist.Addresses, m.cfg.Whitelist.Organizations)
	}

	var rules []config.RuleConfig
	var combos []config.CombinationConfig
	var systems []config.SystemConfig
	if m.repo != nil {
		var err error
		systems, err = m.repo.ListSystems(context.Background())
		if err != nil {
			return fmt.Errorf("list systems: %w", err)
		}
		rules, err = m.repo.ListRules(context.Background())
		if err != nil {
			return fmt.Errorf("list rules: %w", err)
		}
		combos, err = m.repo.ListCombinations(context.Background())
		if err != nil {
			return fmt.Errorf("list combinations: %w", err)
		}
	} else {
		systems = m.cfg.Systems
		rules = m.cfg.Rules
		combos = m.cfg.Combinations
	}

	core := detector.StructuredRules()
	extra, err := m.buildOverlayRules(rules)
	if err != nil {
		return err
	}
	allRules := append(core, extra...)
	d := detector.New(allRules, m.detectorOpts...)
	m.detector = d
	m.context = ctxR
	m.whitelist = w
	priority := m.cfg.Resolve.Priority
	if priority == nil {
		priority = copyPriority(resolve.DefaultPriority)
	}
	pc := make([]pipeline.Combination, 0, len(combos))
	for _, c := range combos {
		pc = append(pc, pipeline.Combination{Type: c.Type, Requires: c.Requires, Window: c.Window})
	}
	opts := pipeline.Options{
		Mode:         m.cfg.Masking.Mode,
		Sensitive:    m.cfg.SensitiveTypes,
		Combinations: pc,
	}
	m.base = pipeline.New(d, ctxR, w, priority, opts)
	m.systems = make(map[string]*pipeline.Pipeline, len(systems))
	m.systemCfg = make(map[string]config.SystemConfig, len(systems))
	for i := range systems {
		s := systems[i]
		m.systemCfg[s.Name] = s
		if !s.Enabled {
			continue
		}
		mode := s.Masking
		if mode == "" {
			mode = opts.Mode
		}
		so := opts
		so.Mode = mode
		so.AllowedTypes = s.PII
		m.systems[s.Name] = pipeline.New(d, ctxR, w, priority, so)
	}
	if m.cfg.RateLimit.RPS > 0 {
		m.limiter = ratelimit.New(m.cfg.RateLimit.RPS, m.cfg.RateLimit.Burst)
	} else {
		m.limiter = nil
	}
	return nil
}
```

Change `buildOverlayRules` to take rules as a param:

```go
func (m *Manager) buildOverlayRules(rules []config.RuleConfig) ([]detector.Rule, error) {
	if len(rules) == 0 {
		return nil, nil
	}
	out := make([]detector.Rule, 0, len(rules))
	for _, rc := range rules {
		r, err := detector.RuleFromConfig(rc.Type, rc.Regex, rc.Context, rc.Capture, rc.Priority, rc.Confidence, rc.Keyword)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
```

Change `System` to read from cache:

```go
func (m *Manager) System(name string) *config.SystemConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.systemCfg[name]; ok {
		return &s
	}
	return nil
}
```

Add `SystemHash`:

```go
// SystemHash returns the api_key_hash for a system ("" if absent).
func (m *Manager) SystemHash(name string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.systemCfg[name]; ok {
		return s.APIKey
	}
	return ""
}
```

Change `KnownTypes` to read rules from the repo cache. Since `buildLocked` no longer stores rules on the Manager, add a `rules []config.RuleConfig` field:

Add to Manager struct: `rules []config.RuleConfig`.

In `buildLocked`, after reading rules: `m.rules = rules`.

Change `KnownTypes`:

```go
func (m *Manager) KnownTypes() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	types := make([]string, 0, len(detector.KnownTypes())+len(m.rules))
	for _, t := range detector.KnownTypes() {
		types = append(types, string(t))
	}
	for _, r := range m.rules {
		found := false
		for _, k := range types {
			if k == r.Type {
				found = true
				break
			}
		}
		if !found {
			types = append(types, r.Type)
		}
	}
	return types
}
```

Replace `Apply` with `Reload`:

```go
// Reload re-reads systems/rules/combinations from the repo and rebuilds all
// runtime components under the write lock. On error the previous state is
// restored untouched and rev does not change.
func (m *Manager) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	oldDet, oldCtx, oldW, oldBase, oldSys, oldSysCfg, oldLim, oldRules :=
		m.detector, m.context, m.whitelist, m.base, m.systems, m.systemCfg, m.limiter, m.rules
	if err := m.buildLocked(); err != nil {
		m.detector, m.context, m.whitelist, m.base, m.systems, m.systemCfg, m.limiter, m.rules =
			oldDet, oldCtx, oldW, oldBase, oldSys, oldSysCfg, oldLim, oldRules
		return err
	}
	m.rev++
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go test ./internal/control/`
Expected: PASS. Existing tests that used `Apply` must be updated to use `Reload` or removed (see Step 5).

- [ ] **Step 5: Update existing tests that used Apply**

Search for `Apply(` in `internal/control/manager_test.go` and `internal/api/handlers/config_test.go`. Replace `mgr.Apply(cfg)` with `mgr.Reload()` where the intent is a DB-driven reload, or remove tests that exercised `PUT /v1/config` (those are removed in Task 5).

- [ ] **Step 6: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add internal/control/manager.go internal/control/manager_test.go
git -c user.name="col3name" -c user.email="col3name@users.noreply.github.com" commit -m "control: reload systems/rules/combinations from repo, remove Apply"
```

---

### Task 5: Auth + systems CRUD handlers; remove PUT /v1/config

**Files:**
- Create: `internal/api/handlers/auth.go`, `internal/api/handlers/systems.go`
- Modify: `internal/api/handlers/config.go`, `internal/api/handlers/process.go`
- Test: `internal/api/handlers/auth_test.go`, `internal/api/handlers/systems_test.go`, `internal/api/handlers/process_test.go`

**Interfaces:**
- Consumes: `db.Repo` (auth methods), `control.Manager` (Reload, SystemHash, KnownTypes).
- Produces:
  - `handlers.Handler` gains `Repo *db.Repo` field.
  - `handlers.Login(w, r)`, `handlers.Logout(w, r)`
  - `handlers.ListSystems(w, r)`, `handlers.CreateSystem(w, r)`, `handlers.GetSystem(w, r)`, `handlers.UpdateSystem(w, r)`, `handlers.DeleteSystem(w, r)`, `handlers.RegenerateKey(w, r)`
  - `handlers.RequireAuth(next http.HandlerFunc) http.HandlerFunc` middleware.
  - `handlers.GenerateAccessKey() string` — 32 hex chars.
  - `handlers.HashKey(key string) string` — sha256 hex.

- [ ] **Step 1: Write the failing test (auth)**

Create `internal/api/handlers/auth_test.go`:

```go
package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoginLogout(t *testing.T) {
	h := newDBTestHandler(t)
	// Seed an admin directly on the repo.
	require.NoError(t, h.Repo.SeedAdmin(t.Context(), "admin", "secret"))

	// Login with wrong password -> 401.
	body, _ := json.Marshal(map[string]string{"login": "admin", "password": "nope"})
	req := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Login(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	// Login with correct password -> 200 + token.
	body2, _ := json.Marshal(map[string]string{"login": "admin", "password": "secret"})
	req2 := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(body2))
	rec2 := httptest.NewRecorder()
	h.Login(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)
	var resp struct{ Token string `json:"token"` }
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Token)

	// Logout with the token -> 200.
	req3 := httptest.NewRequest("POST", "/v1/auth/logout", nil)
	req3.Header.Set("Authorization", "Bearer "+resp.Token)
	rec3 := httptest.NewRecorder()
	h.Logout(rec3, req3)
	require.Equal(t, http.StatusOK, rec3.Code)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go test ./internal/api/handlers/ -run TestLoginLogout`
Expected: FAIL — `h.Repo` undefined.

- [ ] **Step 3: Add Repo to Handler and update newTestHandler**

In `internal/api/handlers/process.go`, change `Handler`:

```go
type Handler struct {
	Mgr  *control.Manager
	Repo *db.Repo
}
```

Add import `"github.com/kind-earthquake/pii-module/internal/db"`.

The `process.go` handler does NOT use `Repo` directly (auth uses the pure `HashKey` function and `s.APIKey`). So process tests must NOT require Postgres. Keep `newTestHandler` in `process_test.go` as-is (no Repo), and add a separate helper for auth/systems tests:

In `process_test.go`, keep `newTestHandler` unchanged (it returns `&Handler{Mgr: m}` — Repo stays nil, which is fine for process tests).

Create `internal/api/handlers/db_test_helper.go`:

```go
package handlers

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/config"
	"github.com/kind-earthquake/pii-module/internal/control"
	"github.com/kind-earthquake/pii-module/internal/db"
	"github.com/kind-earthquake/pii-module/internal/store"
)

func tEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// newDBTestHandler builds a Handler with a real Postgres repo. Skips if the
// DB is unreachable (PII_TEST_DSN, default localhost).
func newDBTestHandler(t *testing.T, mutate ...func(*config.Config)) *Handler {
	t.Helper()
	cfg := config.Default()
	for _, fn := range mutate {
		fn(cfg)
	}
	m, err := control.New(control.WithConfig(cfg), control.WithStore(store.NewMemory(time.Hour, 1000)))
	require.NoError(t, err)
	repo, err := db.New(t.Context(), tEnv("PII_TEST_DSN", "postgres://pii:pii@localhost:5432/pii?sslmode=disable"))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return &Handler{Mgr: m, Repo: repo}
}
```

Update `auth_test.go` and `systems_test.go` to use `newDBTestHandler` instead of `newTestHandler`.

- [ ] **Step 4: Write auth.go**

Create `internal/api/handlers/auth.go`:

```go
package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/kind-earthquake/pii-module/internal/db"
)

const sessionTTL = 24 * time.Hour

// Login handles POST /v1/auth/login.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ok, err := h.Repo.VerifyAdmin(r.Context(), in.Login, in.Password)
	if err != nil || !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	token := GenerateAccessKey()
	if err := h.Repo.CreateSession(r.Context(), HashKey(token), in.Login, sessionTTL); err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"token": token})
}

// Logout handles POST /v1/auth/logout.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if token != "" {
		_ = h.Repo.DeleteSession(r.Context(), HashKey(token))
	}
	w.WriteHeader(http.StatusOK)
}

// RequireAuth is middleware that rejects requests without a valid session token.
func (h *Handler) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ok, err := h.Repo.ValidateSession(r.Context(), HashKey(token))
		if err != nil || !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) > len(prefix) && h[:len(prefix)] == prefix {
		return h[len(prefix):]
	}
	return ""
}

// GenerateAccessKey returns a random 32-hex-char key.
func GenerateAccessKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// HashKey returns the sha256 hex digest of a key.
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

var _ = db.ErrNotFound // keep import if unused
```

- [ ] **Step 5: Write systems.go**

Create `internal/api/handlers/systems.go`:

```go
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kind-earthquake/pii-module/internal/config"
	"github.com/kind-earthquake/pii-module/internal/db"
	"github.com/kind-earthquake/pii-module/internal/detector"
)

// systemView is the JSON view of a system (no key value, only a flag).
type systemView struct {
	Name        string          `json:"name"`
	APIKeySet   bool            `json:"api_key_set"`
	Enabled     bool            `json:"enabled"`
	Masking     string          `json:"masking"`
	AllowUnmask bool            `json:"allow_unmask"`
	PII         []detector.Type `json:"pii"`
}

func toView(s config.SystemConfig) systemView {
	return systemView{
		Name:        s.Name,
		APIKeySet:   s.APIKey != "",
		Enabled:     s.Enabled,
		Masking:     s.Masking,
		AllowUnmask: s.AllowUnmask,
		PII:         s.PII,
	}
}

// ListSystems handles GET /v1/systems.
func (h *Handler) ListSystems(w http.ResponseWriter, r *http.Request) {
	systems, err := h.Repo.ListSystems(r.Context())
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	out := make([]systemView, 0, len(systems))
	for _, s := range systems {
		out = append(out, toView(s))
	}
	writeJSON(w, out)
}

// CreateSystem handles POST /v1/systems. Returns the one-time access key.
func (h *Handler) CreateSystem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string          `json:"name"`
		Enabled     *bool           `json:"enabled"`
		AllowUnmask *bool           `json:"allow_unmask"`
		Masking     string          `json:"masking"`
		PII         []detector.Type `json:"pii"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if in.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	key := GenerateAccessKey()
	s := config.SystemConfig{
		Name:        in.Name,
		APIKey:      HashKey(key),
		Enabled:     derefBool(in.Enabled, true),
		AllowUnmask: derefBool(in.AllowUnmask, false),
		Masking:     in.Masking,
		PII:         in.PII,
	}
	if err := h.Repo.CreateSystem(r.Context(), s); err != nil {
		http.Error(w, "create failed", http.StatusConflict)
		return
	}
	if err := h.Mgr.Reload(); err != nil {
		http.Error(w, "reload failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"name": in.Name, "access_key": key})
}

// GetSystem handles GET /v1/systems/{name}.
func (h *Handler) GetSystem(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	s, err := h.Repo.GetSystem(r.Context(), name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, toView(*s))
}

// UpdateSystem handles PUT /v1/systems/{name}.
func (h *Handler) UpdateSystem(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var in struct {
		Enabled     *bool           `json:"enabled"`
		AllowUnmask *bool           `json:"allow_unmask"`
		Masking     *string         `json:"masking"`
		PII         []detector.Type `json:"pii"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	cur, err := h.Repo.GetSystem(r.Context(), name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if in.Enabled != nil {
		cur.Enabled = *in.Enabled
	}
	if in.AllowUnmask != nil {
		cur.AllowUnmask = *in.AllowUnmask
	}
	if in.Masking != nil {
		cur.Masking = *in.Masking
	}
	if in.PII != nil {
		cur.PII = in.PII
	}
	if err := h.Repo.UpdateSystem(r.Context(), name, *cur); err != nil {
		http.Error(w, "update failed", http.StatusInternalServerError)
		return
	}
	if err := h.Mgr.Reload(); err != nil {
		http.Error(w, "reload failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]uint64{"rev": h.Mgr.Rev()})
}

// DeleteSystem handles DELETE /v1/systems/{name}.
func (h *Handler) DeleteSystem(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.Repo.DeleteSystem(r.Context(), name); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := h.Mgr.Reload(); err != nil {
		http.Error(w, "reload failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RegenerateKey handles POST /v1/systems/{name}/regenerate-key.
func (h *Handler) RegenerateKey(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	key := GenerateAccessKey()
	if err := h.Repo.SetAPIKeyHash(r.Context(), name, HashKey(key)); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := h.Mgr.Reload(); err != nil {
		http.Error(w, "reload failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"access_key": key})
}
```

- [ ] **Step 6: Update process.go for hash-based auth**

In `internal/api/handlers/process.go`, change `authorize` to use the hash:

```go
// authorize enforces the per-system API key via the X-API-Key header. Systems
// without a configured key are authorized implicitly.
func (h *Handler) authorize(r *http.Request, s *config.SystemConfig) error {
	if s == nil || s.APIKey == "" {
		return nil
	}
	if HashKey(r.Header.Get("X-API-Key")) == s.APIKey {
		return nil
	}
	return config.ErrUnauthorized
}
```

Note: `s.APIKey` now holds the sha256 hash (from DB). `HashKey` is defined in auth.go.

- [ ] **Step 7: Simplify config.go to read-only GET**

In `internal/api/handlers/config.go`, remove `PutConfig`, `GetConfigRules`, `adminAuthorized`, `mgrConfigSnapshot`, `derefBool` (moved to systems.go usage — keep `derefBool` since systems.go uses it). Keep `GetConfig` (read-only) and `CORS`, `writeJSON`.

Update `GetConfig` to read systems from the repo:

```go
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := h.Mgr.Config()
	systems, err := h.Repo.ListSystems(r.Context())
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	view := configView{
		Rev:          h.Mgr.Rev(),
		Masking:      cfg.Masking.Mode,
		KnownTypes:   h.Mgr.KnownTypes(),
	}
	for _, s := range systems {
		view.Systems = append(view.Systems, toView(s))
	}
	rules, _ := h.Repo.ListRules(r.Context())
	combos, _ := h.Repo.ListCombinations(r.Context())
	view.Rules = rules
	view.Combinations = combos
	writeJSON(w, view)
}
```

Remove `PutConfig`, `GetConfigRules`, `adminAuthorized`, `mgrConfigSnapshot`. Keep `derefBool` (used by systems.go).

- [ ] **Step 8: Update config_test.go**

Remove tests for `PutConfig` and `GetConfigRules`. Keep/adapt `GetConfig` test to use the repo.

- [ ] **Step 9: Run all handler tests**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go test ./internal/api/handlers/`
Expected: PASS (or SKIP where Postgres unavailable).

- [ ] **Step 10: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add internal/api/handlers/
git -c user.name="col3name" -c user.email="col3name@users.noreply.github.com" commit -m "api: auth login/logout, systems CRUD, read-only config, hash-based system auth"
```

---

### Task 6: Wire main.go — DB init, seed, routes

**Files:**
- Modify: `cmd/server/main.go`
- Modify: `configs/config.yaml`

**Interfaces:**
- Consumes: `db.New`, `db.Repo.SeedFromConfig`, `db.Repo.SeedAdmin`, `handlers.Handler{Repo}`, `handlers.RequireAuth`.
- Produces: running server with new routes.

- [ ] **Step 1: Update config.yaml**

Add to `configs/config.yaml`:

```yaml
database:
  dsn: "postgres://pii:pii@localhost:5432/pii?sslmode=disable"

admin:
  login: "admin"
  password: "admin123"
```

Replace the existing `admin:` block (currently `key: "pii-admin-key"`).

- [ ] **Step 2: Update main.go**

In `cmd/server/main.go`:

Add imports: `"github.com/kind-earthquake/pii-module/internal/db"`.

After building the store, initialize the DB repo:

```go
	ctx := context.Background()
	var repo *db.Repo
	if cfg.Database.DSN != "" {
		repo, err = db.New(ctx, cfg.Database.DSN)
		if err != nil {
			slog.Error("failed to connect database", "error", err)
			os.Exit(1)
		}
		defer repo.Close()
		if err := repo.SeedFromConfig(ctx, cfg); err != nil {
			slog.Error("failed to seed database", "error", err)
			os.Exit(1)
		}
		if err := repo.SeedAdmin(ctx, cfg.Admin.Login, cfg.Admin.Password); err != nil {
			slog.Error("failed to seed admin", "error", err)
			os.Exit(1)
		}
	}
```

Pass repo to Manager:

```go
	mgr, err := control.New(
		control.WithConfig(cfg),
		control.WithStore(st),
		control.WithDetectorOpts(detectorOpts...),
		control.WithRepo(repo),
	)
```

Update Handler:

```go
	h := &handlers.Handler{Mgr: mgr, Repo: repo}
```

Update routes:

```go
	r.Route("/v1", func(r chi.Router) {
		r.Use(handlers.CORS(adminOrigin()))
		r.Post("/auth/login", h.Login)
		r.Post("/auth/logout", h.RequireAuth(h.Logout))
		r.Group(func(r chi.Router) {
			r.Use(h.RequireAuth)
			r.Get("/config", h.GetConfig)
			r.Get("/systems", h.ListSystems)
			r.Post("/systems", h.CreateSystem)
			r.Get("/systems/{name}", h.GetSystem)
			r.Put("/systems/{name}", h.UpdateSystem)
			r.Delete("/systems/{name}", h.DeleteSystem)
			r.Post("/systems/{name}/regenerate-key", h.RegenerateKey)
		})
	})
```

Note: `r.Group` inside `r.Route` — chi supports nested groups. The `RequireAuth` middleware wraps all admin endpoints except login.

- [ ] **Step 3: Build**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go build ./...`
Expected: PASS.

- [ ] **Step 4: Run all tests**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go test ./...`
Expected: PASS (DB tests skip if no Postgres).

- [ ] **Step 5: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add cmd/server/main.go configs/config.yaml
git -c user.name="col3name" -c user.email="col3name@users.noreply.github.com" commit -m "server: wire postgres repo, seed, auth and systems routes"
```

---

### Task 7: Frontend — login screen + auth token in api.ts

**Files:**
- Modify: `web/src/api.ts`
- Create: `web/src/LoginScreen.tsx`
- Modify: `web/src/App.tsx`

**Interfaces:**
- Consumes: `POST /v1/auth/login`, `POST /v1/auth/logout`.
- Produces:
  - `api.login(login, password) → {token}`
  - `api.logout()`
  - `setToken(token)`, `getToken()`
  - `LoginScreen` component (login/password form).
  - `App` shows LoginScreen when no token, else SystemsTab.

- [ ] **Step 1: Update api.ts**

Replace the admin-key logic with token logic:

```ts
let token = '';

export function setToken(t: string) {
  token = t;
}

export function getToken() {
  return token;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(init?.headers as Record<string, string>),
  };
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  const res = await fetch(path, { ...init, headers });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`${res.status}: ${text.slice(0, 200)}`);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  login: (login: string, password: string) =>
    request<{ token: string }>('/v1/auth/login', { method: 'POST', body: JSON.stringify({ login, password }) }),
  logout: () => request<void>('/v1/auth/logout', { method: 'POST' }),
  getConfig: () => request<ConfigView>('/v1/config'),
  listSystems: () => request<SystemInfo[]>('/v1/systems'),
  createSystem: (body: CreateSystemBody) =>
    request<{ name: string; access_key: string }>('/v1/systems', { method: 'POST', body: JSON.stringify(body) }),
  updateSystem: (name: string, body: UpdateSystemBody) =>
    request<{ rev: number }>(`/v1/systems/${encodeURIComponent(name)}`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteSystem: (name: string) => request<void>(`/v1/systems/${encodeURIComponent(name)}`, { method: 'DELETE' }),
  regenerateKey: (name: string) =>
    request<{ access_key: string }>(`/v1/systems/${encodeURIComponent(name)}/regenerate-key`, { method: 'POST' }),
};
```

Add types:

```ts
export interface CreateSystemBody {
  name: string;
  enabled?: boolean;
  allow_unmask?: boolean;
  masking?: string;
  pii?: string[];
}

export interface UpdateSystemBody {
  enabled?: boolean;
  allow_unmask?: boolean;
  masking?: string;
  pii?: string[];
}
```

Remove `setAdminKey`, `getAdminKey`, `putConfig`, `getRules`, `AdminKeyInput` usage.

- [ ] **Step 2: Create LoginScreen.tsx**

```tsx
import { useState } from 'react';
import { Button, Input, Section } from '@telegram-apps/telegram-ui';
import { api, setToken } from './api';

export default function LoginScreen({ onLogin }: { onLogin: () => void }) {
  const [login, setLogin] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');

  const submit = async () => {
    try {
      const res = await api.login(login, password);
      setToken(res.token);
      onLogin();
    } catch (e) {
      setError(String(e));
    }
  };

  return (
    <Section header="Вход в админку">
      <Input title="Логин" value={login} onChange={(e) => setLogin(e.target.value)} placeholder="admin" />
      <Input title="Пароль" type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="••••••" />
      <Button onClick={submit}>Войти</Button>
      {error && <div style={{ color: 'red' }}>{error}</div>}
    </Section>
  );
}
```

- [ ] **Step 3: Update App.tsx**

```tsx
import { useState } from 'react';
import { Button, Cell, List, Section } from '@telegram-apps/telegram-ui';
import SystemsTab from './SystemsTab';
import LoginScreen from './LoginScreen';
import { getToken, setToken, api } from './api';

export default function App() {
  const [authed, setAuthed] = useState(() => getToken() !== '');

  if (!authed) {
    return (
      <div style={{ maxWidth: 720, margin: '0 auto', padding: 16 }}>
        <LoginScreen onLogin={() => setAuthed(true)} />
      </div>
    );
  }

  return (
    <div style={{ maxWidth: 720, margin: '0 auto', padding: 16 }}>
      <Section header="PII Gateway — Админка">
        <List>
          <Cell subtitle="системы-потребители">Системы</Cell>
        </List>
      </Section>
      <SystemsTab />
      <Button onClick={async () => { try { await api.logout(); } finally { setToken(''); setAuthed(false); } }}>
        Выйти
      </Button>
    </div>
  );
}
```

- [ ] **Step 4: Typecheck + build**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2/web && npx tsc --noEmit && npm run build`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add web/src/api.ts web/src/LoginScreen.tsx web/src/App.tsx
git -c user.name="col3name" -c user.email="col3name@users.noreply.github.com" commit -m "web: login screen and bearer token auth"
```

---

### Task 8: Frontend — SystemsTab rewrite (CRUD + copy key)

**Files:**
- Modify: `web/src/SystemsTab.tsx`
- Delete: `web/src/RulesTab.tsx`, `web/src/CombinationsTab.tsx`, `web/src/ConfigTab.tsx`, `web/src/AdminKeyInput.tsx`

**Interfaces:**
- Consumes: `api.listSystems`, `api.createSystem`, `api.updateSystem`, `api.deleteSystem`, `api.regenerateKey`.
- Produces: SystemsTab with list, create form, enable/disable switch, allow_unmask switch, copy-key button.

- [ ] **Step 1: Rewrite SystemsTab.tsx**

```tsx
import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Cell, Input, List, Section, Switch } from '@telegram-apps/telegram-ui';
import { api, type SystemInfo } from './api';

export default function SystemsTab() {
  const qc = useQueryClient();
  const { data: systems, isLoading } = useQuery<SystemInfo[]>({ queryKey: ['systems'], queryFn: api.listSystems });
  const [name, setName] = useState('');
  const [newKey, setNewKey] = useState('');

  const invalidate = () => qc.invalidateQueries({ queryKey: ['systems'] });

  const create = useMutation({
    mutationFn: (n: string) => api.createSystem({ name: n }),
    onSuccess: (res) => { setNewKey(res.access_key); setName(''); invalidate(); },
  });
  const update = useMutation({
    mutationFn: ({ name, body }: { name: string; body: { enabled?: boolean; allow_unmask?: boolean } }) =>
      api.updateSystem(name, body),
    onSuccess: invalidate,
  });
  const remove = useMutation({ mutationFn: (n: string) => api.deleteSystem(n), onSuccess: invalidate });
  const regen = useMutation({
    mutationFn: (n: string) => api.regenerateKey(n),
    onSuccess: (res) => setNewKey(res.access_key),
  });

  if (isLoading) return <div>Загрузка…</div>;

  const copy = (v: string) => navigator.clipboard?.writeText(v);

  return (
    <Section header="Системы-потребители">
      <List>
        {(systems ?? []).map((s) => (
          <Section key={s.name} header={s.name}>
            <Cell
              subtitle={`allow_unmask: ${s.allow_unmask ? 'да' : 'нет'} · режим: ${s.masking || 'глобальный'}`}
              after={<Switch checked={s.enabled} onChange={(e) => update.mutate({ name: s.name, body: { enabled: e.target.checked } })} />}
            >
              {s.enabled ? 'Включена' : 'Отключена'}
            </Cell>
            <Cell
              subtitle="Демаскирование"
              after={<Switch checked={s.allow_unmask} onChange={(e) => update.mutate({ name: s.name, body: { allow_unmask: e.target.checked } })} />}
            >
              {s.allow_unmask ? 'Разрешено' : 'Запрещено'}
            </Cell>
            <Cell subtitle="Разрешённые типы ПДН">
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                {(s.pii ?? []).map((t) => <span key={t}>{t}</span>)}
              </div>
            </Cell>
            <Cell>
              <Button size="s" onClick={() => regen.mutate(s.name)}>Новый ключ</Button>
              <Button size="s" mode="danger" onClick={() => remove.mutate(s.name)}>Удалить</Button>
            </Cell>
          </Section>
        ))}
      </List>

      <Section header="Новая система">
        <Input title="Имя" value={name} onChange={(e) => setName(e.target.value)} placeholder="chat" />
        <Button onClick={() => create.mutate(name)} disabled={!name || create.isPending}>Создать</Button>
      </Section>

      {newKey && (
        <Section header="Новый ключ (показывается один раз)">
          <Cell subtitle="скопируйте и сохраните">
            <code>{newKey}</code>
          </Cell>
          <Button onClick={() => copy(newKey)}>Копировать</Button>
        </Section>
      )}

      {(create.isError || update.isError || remove.isError || regen.isError) && (
        <div style={{ color: 'red' }}>Ошибка: {String(create.error || update.error || remove.error || regen.error)}</div>
      )}
    </Section>
  );
}
```

- [ ] **Step 2: Delete unused tab files**

Run:
```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2/web
rm src/RulesTab.tsx src/CombinationsTab.tsx src/ConfigTab.tsx src/AdminKeyInput.tsx
```

- [ ] **Step 3: Typecheck + build**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2/web && npx tsc --noEmit && npm run build`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add web/src/
git -c user.name="col3name" -c user.email="col3name@users.noreply.github.com" commit -m "web: systems CRUD tab with copy-key, remove rules/combos/config tabs"
```

---

### Task 9: Docker — postgres service + compose wiring

**Files:**
- Modify: `docker-compose.yml`

**Interfaces:**
- Produces: `postgres` service; gateway connects via DSN.

- [ ] **Step 1: Add postgres service**

In `docker-compose.yml`, add:

```yaml
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: pii
      POSTGRES_PASSWORD: pii
      POSTGRES_DB: pii
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data
    restart: unless-stopped

volumes:
  pgdata:
```

Add `depends_on: [postgres]` to the `pii-module-v2` service.

- [ ] **Step 2: Validate YAML**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && python3 -c "import yaml; d=yaml.safe_load(open('docker-compose.yml')); print(list(d['services']))"`
Expected: includes `postgres`.

- [ ] **Step 3: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add docker-compose.yml
git -c user.name="col3name" -c user.email="col3name@users.noreply.github.com" commit -m "docker: add postgres service for systems storage"
```

---

### Task 10: End-to-end verification

**Files:**
- Verify only (no new files unless a fix is needed).

- [ ] **Step 1: Build and run all Go tests**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go build ./... && go test ./...`
Expected: ALL PASS (DB tests skip if no Postgres).

- [ ] **Step 2: Start Postgres (if available)**

Run: `docker compose up -d postgres` (if Docker available) OR use an existing Postgres. If no Postgres, note that DB-backed flows cannot be verified and report as a concern.

- [ ] **Step 3: Rebuild and restart the gateway**

Run:
```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
go build -o /tmp/pii-server ./cmd/server
pkill -f "pii-server" || true
sleep 1
nohup /tmp/pii-server -config configs/config.yaml > /tmp/pii-server.log 2>&1 &
sleep 1
curl -s localhost:8080/health
```
Expected: `ok`.

- [ ] **Step 4: Verify auth + systems CRUD over HTTP**

Run:
```bash
# Login
TOKEN=$(curl -s -X POST localhost:8080/v1/auth/login -H 'Content-Type: application/json' -d '{"login":"admin","password":"admin123"}' | python3 -c "import sys,json;print(json.load(sys.stdin)['token'])")
echo "token=$TOKEN"

# Create system
curl -s -X POST localhost:8080/v1/systems -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"name":"chat","enabled":true,"allow_unmask":true,"masking":"token"}'
echo

# List systems
curl -s localhost:8080/v1/systems -H "Authorization: Bearer $TOKEN"
echo

# Unauthorized without token
curl -s -o /dev/null -w "%{http_code}\n" localhost:8080/v1/systems
```
Expected: login returns token; create returns access_key; list returns the system; no-token returns 401.

- [ ] **Step 5: Verify /process still works with hash auth**

Run:
```bash
# Use the access_key from Step 4 as X-API-Key
curl -s -X POST localhost:8080/process -H "X-API-Key: <access_key>" -d '{"payload":"паспорт 4509 123456","payload_id":"e2e-1","system":"chat"}'
```
Expected: masked result.

- [ ] **Step 6: Verify UI**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2/web && npm run dev`
Open http://localhost:5173, log in with admin/admin123, verify Systems tab lists systems, create/delete works.

- [ ] **Step 7: Final commit (if any fixes)**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add -A
git -c user.name="col3name" -c user.email="col3name@users.noreply.github.com" commit -m "chore: postgres systems + auth fixes from e2e verification"
```
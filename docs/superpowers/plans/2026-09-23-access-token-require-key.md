# access_token + require-key Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Accept an optional `access_token` in the `/process` body (same system API key) and add a per-system "Требовать access key" flag that forces authentication.

**Architecture:** Add `RequireKey bool` to `SystemConfig` (config + DB column + API view). Update `/process` `authorize` to accept the key from `X-API-Key` header (priority) or body `access_token`, and reject when `require_key` is set without a valid key. Add a checkbox in the TeamCard UI.

**Tech Stack:** Go 1.25, pgx/v5, chi/v5, React 18 + TanStack Query + TelegramUI.

## Global Constraints

- Module `github.com/kind-earthquake/pii-module`, Go 1.25.0.
- `access_token` is the SAME system API key (sha256 vs `api_key_hash`), no separate token store.
- Key priority: `X-API-Key` header wins; if absent, use body `access_token`.
- By default `/process` is open to all (no key required) unless `require_key` is set.
- Auth matrix:
  - `require_key=true`, no system key → 401 always.
  - `require_key=true`, key valid → 200.
  - `require_key=true`, key missing/invalid → 401.
  - `require_key=false`, no key → 200 (open).
  - `require_key=false`, valid key → 200.
  - `require_key=false`, invalid key → 401.
- Tests use `github.com/stretchr/testify/require`.
- Git identity: `git -c user.name="col3name" -c user.email="col3name@users.noreply.github.com" commit -m "..."`

---

### Task 1: Config + DB — add RequireKey

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/db/db.go`, `internal/db/systems.go`
- Test: `internal/db/systems_test.go`

**Interfaces:**
- Produces: `config.SystemConfig.RequireKey bool`; DB `systems.require_key` column; `ListSystems`/`GetSystem`/`CreateSystem`/`UpdateSystem` read/write it.

- [ ] **Step 1: Add RequireKey to SystemConfig**

In `internal/config/config.go`, add `RequireKey bool` to `SystemConfig`:

```go
type SystemConfig struct {
	Name        string          `yaml:"name"`
	APIKey      string          `yaml:"api_key"`
	Enabled     bool            `yaml:"enabled"`
	PII         []detector.Type `yaml:"pii"`
	Masking     string          `yaml:"masking"`
	AllowUnmask bool            `yaml:"allow_unmask"`
	RequireKey  bool            `yaml:"require_key"`
}
```

- [ ] **Step 2: Add require_key to the migration**

In `internal/db/db.go`, update the `systems` CREATE TABLE to add the column:

```go
`CREATE TABLE IF NOT EXISTS systems (
	name          text PRIMARY KEY,
	api_key_hash  text NOT NULL DEFAULT '',
	enabled       boolean NOT NULL DEFAULT true,
	allow_unmask  boolean NOT NULL DEFAULT false,
	masking       text NOT NULL DEFAULT '',
	pii           text[] NOT NULL DEFAULT '{}',
	require_key   boolean NOT NULL DEFAULT false,
	created_at    timestamptz NOT NULL DEFAULT now(),
	updated_at    timestamptz NOT NULL DEFAULT now()
)`,
```

Note: for an existing DB, add an idempotent `ALTER TABLE` after the CREATE TABLE statements:

```go
`ALTER TABLE systems ADD COLUMN IF NOT EXISTS require_key boolean NOT NULL DEFAULT false`,
```

- [ ] **Step 3: Update systems.go CRUD**

In `internal/db/systems.go`:

`ListSystems` — add `require_key` to SELECT and Scan:
```go
rows, err := r.pool.Query(ctx,
	`SELECT name, api_key_hash, enabled, allow_unmask, masking, pii, require_key FROM systems ORDER BY name`)
...
if err := rows.Scan(&s.Name, &s.APIKey, &s.Enabled, &s.AllowUnmask, &s.Masking, &pii, &s.RequireKey); err != nil {
```

`GetSystem` — add `require_key` to SELECT and Scan:
```go
err := r.pool.QueryRow(ctx,
	`SELECT name, api_key_hash, enabled, allow_unmask, masking, pii, require_key FROM systems WHERE name=$1`, name).
	Scan(&s.Name, &s.APIKey, &s.Enabled, &s.AllowUnmask, &s.Masking, &pii, &s.RequireKey)
```

`CreateSystem` — add `require_key` to INSERT:
```go
_, err := r.pool.Exec(ctx,
	`INSERT INTO systems (name, api_key_hash, enabled, allow_unmask, masking, pii, require_key)
	 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
	s.Name, s.APIKey, s.Enabled, s.AllowUnmask, s.Masking, fromTypes(s.PII), s.RequireKey)
```

`UpdateSystem` — add `require_key` to UPDATE:
```go
tag, err := r.pool.Exec(ctx,
	`UPDATE systems SET enabled=$2, allow_unmask=$3, masking=$4, pii=$5, require_key=$6, updated_at=now() WHERE name=$1`,
	name, s.Enabled, s.AllowUnmask, s.Masking, fromTypes(s.PII), s.RequireKey)
```

- [ ] **Step 4: Write the failing test**

In `internal/db/systems_test.go`, extend `TestSystemsCRUD` to assert `RequireKey` round-trips:

```go
err := r.CreateSystem(ctx, config.SystemConfig{Name: "test-sys", Enabled: true, AllowUnmask: true, Masking: "token", RequireKey: true, PII: []detector.Type{detector.TypePhone}})
require.NoError(t, err)

s, err := r.GetSystem(ctx, "test-sys")
require.NoError(t, err)
require.True(t, s.RequireKey)
```

- [ ] **Step 5: Run tests**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go test ./internal/db/`
Expected: PASS (DB tests run against `pii_test`; skip if unreachable).

- [ ] **Step 6: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add internal/config/config.go internal/db/db.go internal/db/systems.go internal/db/systems_test.go
git -c user.name="col3name" -c user.email="col3name@users.noreply.github.com" commit -m "config+db: add require_key to systems"
```

---

### Task 2: API — expose require_key in systems handlers

**Files:**
- Modify: `internal/api/handlers/systems.go`
- Test: `internal/api/handlers/systems_test.go`

**Interfaces:**
- Consumes: `config.SystemConfig.RequireKey`.
- Produces: `systemView.RequireKey bool` (json `require_key`); create/update accept `require_key`.

- [ ] **Step 1: Add RequireKey to systemView and toView**

In `internal/api/handlers/systems.go`:

```go
type systemView struct {
	Name        string          `json:"name"`
	APIKeySet   bool            `json:"api_key_set"`
	Enabled     bool            `json:"enabled"`
	Masking     string          `json:"masking"`
	AllowUnmask bool            `json:"allow_unmask"`
	RequireKey  bool            `json:"require_key"`
	PII         []detector.Type `json:"pii"`
}

func toView(s config.SystemConfig) systemView {
	return systemView{
		Name:        s.Name,
		APIKeySet:   s.APIKey != "",
		Enabled:     s.Enabled,
		Masking:     s.Masking,
		AllowUnmask: s.AllowUnmask,
		RequireKey:  s.RequireKey,
		PII:         s.PII,
	}
}
```

- [ ] **Step 2: Add RequireKey to create and update request structs**

In `CreateSystem`, add `RequireKey *bool` to the request struct and set it:

```go
var in struct {
	Name        string          `json:"name"`
	Enabled     *bool           `json:"enabled"`
	AllowUnmask *bool           `json:"allow_unmask"`
	RequireKey  *bool           `json:"require_key"`
	Masking     string          `json:"masking"`
	PII         []detector.Type `json:"pii"`
}
...
s := config.SystemConfig{
	Name:        in.Name,
	APIKey:      HashKey(key),
	Enabled:     derefBool(in.Enabled, true),
	AllowUnmask: derefBool(in.AllowUnmask, false),
	RequireKey:  derefBool(in.RequireKey, false),
	Masking:     in.Masking,
	PII:         in.PII,
}
```

In `UpdateSystem`, add `RequireKey *bool` to the request struct and apply it:

```go
var in struct {
	Enabled     *bool           `json:"enabled"`
	AllowUnmask *bool           `json:"allow_unmask"`
	RequireKey  *bool           `json:"require_key"`
	Masking     *string         `json:"masking"`
	PII         []detector.Type `json:"pii"`
}
...
if in.RequireKey != nil {
	cur.RequireKey = *in.RequireKey
}
```

- [ ] **Step 3: Write the failing test**

In `internal/api/handlers/systems_test.go`, add a test that creates a system with `require_key` and verifies it round-trips:

```go
func TestCreateSystemRequireKey(t *testing.T) {
	h := newDBTestHandler(t)
	body, _ := json.Marshal(map[string]any{"name": "rk-sys", "require_key": true})
	req := httptest.NewRequest("POST", "/v1/systems", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.CreateSystem(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	s, err := h.Repo.GetSystem(t.Context(), "rk-sys")
	require.NoError(t, err)
	require.True(t, s.RequireKey)
}
```

Add `"bytes"` and `"encoding/json"` imports if not present.

- [ ] **Step 4: Run tests**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go test ./internal/api/handlers/`
Expected: PASS (DB tests run against `pii_test`; skip if unreachable).

- [ ] **Step 5: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add internal/api/handlers/systems.go internal/api/handlers/systems_test.go
git -c user.name="col3name" -c user.email="col3name@users.noreply.github.com" commit -m "api: expose require_key in systems handlers"
```

---

### Task 3: /process — access_token body + require_key auth

**Files:**
- Modify: `internal/api/handlers/process.go`
- Test: `internal/api/handlers/process_test.go`

**Interfaces:**
- Consumes: `config.SystemConfig.RequireKey`, `HashKey`.
- Produces: `ProcessRequest.AccessToken string` (json `access_token`); updated `authorize`.

- [ ] **Step 1: Add AccessToken to ProcessRequest**

In `internal/api/handlers/process.go`:

```go
type ProcessRequest struct {
	Payload     string `json:"payload"`
	PayloadID   string `json:"payload_id"`
	System      string `json:"system"`
	AccessToken string `json:"access_token"`
}
```

- [ ] **Step 2: Update authorize**

Replace the `authorize` function:

```go
// authorize enforces the per-system API key. The key may come from the
// X-API-Key header (priority) or the body access_token. When RequireKey is
// set, a valid key is mandatory; otherwise a missing key is allowed but a
// wrong key is still rejected.
func (h *Handler) authorize(r *http.Request, s *config.SystemConfig, bodyToken string) error {
	if s == nil {
		return nil
	}
	key := r.Header.Get("X-API-Key")
	if key == "" {
		key = bodyToken
	}
	if s.RequireKey {
		if s.APIKey == "" {
			return config.ErrUnauthorized
		}
		if key == "" || HashKey(key) != s.APIKey {
			return config.ErrUnauthorized
		}
		return nil
	}
	if key == "" {
		return nil
	}
	if HashKey(key) != s.APIKey {
		return config.ErrUnauthorized
	}
	return nil
}
```

- [ ] **Step 3: Update the authorize call site**

In `Process`, change the call to pass the body token:

```go
if err := h.authorize(r, system, req.AccessToken); err != nil {
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return
}
```

- [ ] **Step 4: Write the failing tests**

In `internal/api/handlers/process_test.go`, add:

```go
func TestProcessAccessTokenBody(t *testing.T) {
	h := newTestHandler(t, func(c *config.Config) {
		c.Systems = []config.SystemConfig{
			{Name: "chat", Enabled: true, APIKey: HashKey("sekret"), AllowUnmask: true},
		}
	})
	// access_token in body authorizes.
	body, _ := json.Marshal(ProcessRequest{Payload: "паспорт 4509 123456", PayloadID: "at-1", System: "chat", AccessToken: "sekret"})
	req := httptest.NewRequest("POST", "/process", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Process(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ProcessResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp.Result, "[ПАСПОРТ]")
}

func TestProcessHeaderPriorityOverBody(t *testing.T) {
	h := newTestHandler(t, func(c *config.Config) {
		c.Systems = []config.SystemConfig{
			{Name: "chat", Enabled: true, APIKey: HashKey("sekret"), AllowUnmask: true},
		}
	})
	// Header wins: valid header + wrong body token -> 200.
	body, _ := json.Marshal(ProcessRequest{Payload: "паспорт 4509 123456", PayloadID: "hp-1", System: "chat", AccessToken: "wrong"})
	req := httptest.NewRequest("POST", "/process", bytes.NewReader(body))
	req.Header.Set("X-API-Key", "sekret")
	rec := httptest.NewRecorder()
	h.Process(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestProcessRequireKey(t *testing.T) {
	h := newTestHandler(t, func(c *config.Config) {
		c.Systems = []config.SystemConfig{
			{Name: "chat", Enabled: true, APIKey: HashKey("sekret"), RequireKey: true},
		}
	})
	// require_key + no key -> 401.
	rec := doProcessSystem(t, h, "паспорт 4509 123456", "rk-1", "chat", "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	// require_key + wrong key -> 401.
	rec2 := doProcessSystem(t, h, "паспорт 4509 123456", "rk-1", "chat", "wrong")
	require.Equal(t, http.StatusUnauthorized, rec2.Code)
	// require_key + valid key -> 200.
	rec3 := doProcessSystem(t, h, "паспорт 4509 123456", "rk-1", "chat", "sekret")
	require.Equal(t, http.StatusOK, rec3.Code)
}

func TestProcessRequireKeyNoSystemKey(t *testing.T) {
	h := newTestHandler(t, func(c *config.Config) {
		c.Systems = []config.SystemConfig{
			{Name: "chat", Enabled: true, RequireKey: true}, // no APIKey
		}
	})
	// require_key + no system key -> 401 always.
	rec := doProcessSystem(t, h, "паспорт 4509 123456", "rk-2", "chat", "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	rec2 := doProcessSystem(t, h, "паспорт 4509 123456", "rk-2", "chat", "anything")
	require.Equal(t, http.StatusUnauthorized, rec2.Code)
}

func TestProcessOpenByDefault(t *testing.T) {
	h := newTestHandler(t, func(c *config.Config) {
		c.Systems = []config.SystemConfig{
			{Name: "chat", Enabled: true}, // no key, no require_key
		}
	})
	// No key -> 200 (open).
	rec := doProcessSystem(t, h, "паспорт 4509 123456", "od-1", "chat", "")
	require.Equal(t, http.StatusOK, rec.Code)
	// Wrong key -> 401.
	rec2 := doProcessSystem(t, h, "паспорт 4509 123456", "od-1", "chat", "wrong")
	require.Equal(t, http.StatusUnauthorized, rec2.Code)
}
```

- [ ] **Step 5: Run tests**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go test ./internal/api/handlers/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add internal/api/handlers/process.go internal/api/handlers/process_test.go
git -c user.name="col3name" -c user.email="col3name@users.noreply.github.com" commit -m "process: accept access_token body and enforce require_key"
```

---

### Task 4: UI — require_key checkbox in TeamCard

**Files:**
- Modify: `web/src/api.ts`, `web/src/TeamCard.tsx`

**Interfaces:**
- Consumes: `api.updateSystem`, `SystemInfo`.
- Produces: `require_key` in `SystemInfo`/`CreateSystemBody`/`UpdateSystemBody`; checkbox in TeamCard.

- [ ] **Step 1: Add require_key to api.ts types**

In `web/src/api.ts`:

```ts
export interface SystemInfo {
  name: string;
  api_key_set: boolean;
  enabled: boolean;
  masking: string;
  allow_unmask: boolean;
  require_key: boolean;
  pii: string[];
}

export interface CreateSystemBody {
  name: string;
  enabled?: boolean;
  allow_unmask?: boolean;
  require_key?: boolean;
  masking?: string;
  pii?: string[];
}

export interface UpdateSystemBody {
  enabled?: boolean;
  allow_unmask?: boolean;
  require_key?: boolean;
  masking?: string;
  pii?: string[];
}
```

- [ ] **Step 2: Add the checkbox to TeamCard**

In `web/src/TeamCard.tsx`, add a Cell with a Switch for `require_key` after the enabled switch:

```tsx
<Cell
  subtitle={team.require_key ? 'Требуется access key' : 'Доступ без ключа'}
  after={<Switch checked={team.require_key} onChange={(e) => update.mutate({ require_key: e.target.checked })} />}
>
  {team.require_key ? 'Требовать access key' : 'Не требовать access key'}
</Cell>
```

- [ ] **Step 3: Typecheck + build**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2/web && npx tsc --noEmit && npm run build`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add web/src/api.ts web/src/TeamCard.tsx
git -c user.name="col3name" -c user.email="col3name@users.noreply.github.com" commit -m "web: require access key checkbox in team card"
```

---

### Task 5: End-to-end verification

**Files:**
- Verify only (no new files unless a fix is needed).

- [ ] **Step 1: Build and run all Go tests**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go build ./... && go test ./...`
Expected: ALL PASS.

- [ ] **Step 2: Rebuild and restart the gateway**

Run:
```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
go build -o /tmp/pii-server ./cmd/server
pkill -f "pii-server" || true
sleep 1
nohup /tmp/pii-server -config configs/config.yaml > /tmp/pii-server.log 2>&1 &
sleep 2
curl -s localhost:8080/health
```
Expected: `ok`.

- [ ] **Step 3: Verify require_key over HTTP**

Run:
```bash
TOKEN=$(curl -s -X POST localhost:8080/v1/auth/login -H 'Content-Type: application/json' -d '{"login":"admin","password":"admin123"}' | python3 -c "import sys,json;print(json.load(sys.stdin)['token'])")
# Create a system with require_key
curl -s -X POST localhost:8080/v1/systems -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"name":"rk-demo","require_key":true}'
echo
# /process without key -> 401
curl -s -o /dev/null -w "%{http_code}\n" -X POST localhost:8080/process -d '{"payload":"паспорт 4509 123456","payload_id":"rk-1","system":"rk-demo"}'
# /process with access_token in body -> 200
KEY=$(curl -s -X POST localhost:8080/v1/systems -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"name":"rk-demo2","require_key":true}' | python3 -c "import sys,json;print(json.load(sys.stdin)['access_key'])")
curl -s -X POST localhost:8080/process -H 'Content-Type: application/json' -d "{\"payload\":\"паспорт 4509 123456\",\"payload_id\":\"rk-2\",\"system\":\"rk-demo2\",\"access_token\":\"$KEY\"}"
echo
# cleanup
curl -s -o /dev/null -w "%{http_code}\n" -X DELETE localhost:8080/v1/systems/rk-demo -H "Authorization: Bearer $TOKEN"
curl -s -o /dev/null -w "%{http_code}\n" -X DELETE localhost:8080/v1/systems/rk-demo2 -H "Authorization: Bearer $TOKEN"
```
Expected: create returns access_key; no-key → 401; body access_token → 200; deletes → 204.

- [ ] **Step 4: Verify UI**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2/web && npm run build`
Expected: PASS. (Visual browser check deferred to human demo.)

- [ ] **Step 5: Final commit (if any fixes)**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add -A
git -c user.name="col3name" -c user.email="col3name@users.noreply.github.com" commit -m "chore: access_token + require-key fixes from verification"
```
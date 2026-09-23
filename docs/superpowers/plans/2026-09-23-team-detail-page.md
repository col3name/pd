# Team Detail Page + require_key Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a home page listing teams as clickable cards, a detail page per team with all settings and access management, and implement the `require_key` backend field plus `access_token` body auth for `/process`.

**Architecture:** Backend first: add `require_key` to `SystemConfig`, DB schema, systems API, and `/process` auth (accept `access_token` in body, enforce `require_key`). Then frontend: add `react-router-dom`, split into `HomePage` (list) and `TeamDetailPage` (settings), remove `TeamCard`.

**Tech Stack:** Go (chi, pgx), React 18, react-router-dom, TanStack Query, TelegramUI, TypeScript.

## Global Constraints

- `pii` empty = all types masked.
- `known_types` comes from `api.getConfig()` (`ConfigView.known_types`).
- `require_key` true + no key configured → all `/process` requests for that system rejected (401).
- `access_token` in `/process` body is the same system API key as `X-API-Key` header.
- `go test ./...` and `cd web && npm run build` must pass.
- Git identity: `git -c user.name="col3name" -c user.email="mmikushov@instaloper.com" commit -m "..."`
- DB tests skip if Postgres unavailable (`PII_TEST_DSN`, default `postgres://pii:pii@localhost:5432/pii_test?sslmode=disable`).

---

### Task 1: Add `require_key` to SystemConfig

**Files:**
- Modify: `internal/config/config.go:96-103`

**Interfaces:**
- Consumes: nothing new.
- Produces: `SystemConfig.RequireKey bool` field (yaml `require_key`).

- [ ] **Step 1: Add the field**

In `internal/config/config.go`, add `RequireKey bool` to `SystemConfig`:

```go
type SystemConfig struct {
	Name        string          `yaml:"name"`
	APIKey      string          `yaml:"api_key"` // non-empty => require X-API-Key
	Enabled     bool            `yaml:"enabled"` // false => system rejected (403)
	PII         []detector.Type `yaml:"pii"`     // empty => all supported types
	Masking     string          `yaml:"masking"` // "redact" | "token" | "synthetic"; empty => global
	AllowUnmask bool            `yaml:"allow_unmask"`
	RequireKey  bool            `yaml:"require_key"` // true => /process requires a valid key
}
```

- [ ] **Step 2: Build**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go build ./...`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add internal/config/config.go
git -c user.name="col3name" -c user.email="mmikushov@instaloper.com" commit -m "config: add require_key to SystemConfig"
```

---

### Task 2: Add `require_key` column to DB schema and queries

**Files:**
- Modify: `internal/db/db.go:41-50`
- Modify: `internal/db/systems.go`

**Interfaces:**
- Consumes: `SystemConfig.RequireKey` from Task 1.
- Produces: `systems.require_key` column read/written by `ListSystems`, `GetSystem`, `CreateSystem`, `UpdateSystem`.

- [ ] **Step 1: Add column to migration**

In `internal/db/db.go`, add `require_key boolean NOT NULL DEFAULT false` to the `systems` CREATE TABLE:

```go
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
```

- [ ] **Step 2: Update ListSystems**

In `internal/db/systems.go`, update the SELECT and Scan:

```go
rows, err := r.pool.Query(ctx,
	`SELECT name, api_key_hash, enabled, allow_unmask, require_key, masking, pii FROM systems ORDER BY name`)
...
if err := rows.Scan(&s.Name, &s.APIKey, &s.Enabled, &s.AllowUnmask, &s.RequireKey, &s.Masking, &pii); err != nil {
```

- [ ] **Step 3: Update GetSystem**

```go
err := r.pool.QueryRow(ctx,
	`SELECT name, api_key_hash, enabled, allow_unmask, require_key, masking, pii FROM systems WHERE name=$1`, name).
	Scan(&s.Name, &s.APIKey, &s.Enabled, &s.AllowUnmask, &s.RequireKey, &s.Masking, &pii)
```

- [ ] **Step 4: Update CreateSystem**

```go
_, err := r.pool.Exec(ctx,
	`INSERT INTO systems (name, api_key_hash, enabled, allow_unmask, require_key, masking, pii)
	 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
	s.Name, s.APIKey, s.Enabled, s.AllowUnmask, s.RequireKey, s.Masking, fromTypes(s.PII))
```

- [ ] **Step 5: Update UpdateSystem**

```go
tag, err := r.pool.Exec(ctx,
	`UPDATE systems SET enabled=$2, allow_unmask=$3, require_key=$4, masking=$5, pii=$6, updated_at=now() WHERE name=$1`,
	name, s.Enabled, s.AllowUnmask, s.RequireKey, s.Masking, fromTypes(s.PII))
```

- [ ] **Step 6: Build**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go build ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add internal/db/db.go internal/db/systems.go
git -c user.name="col3name" -c user.email="mmikushov@instaloper.com" commit -m "db: add require_key column to systems"
```

---

### Task 3: Expose `require_key` in systems API

**Files:**
- Modify: `internal/api/handlers/systems.go`

**Interfaces:**
- Consumes: `SystemConfig.RequireKey` from Task 1.
- Produces: `systemView.RequireKey bool` (json `require_key`); create/update request structs accept `require_key *bool`.

- [ ] **Step 1: Add to systemView and toView**

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

- [ ] **Step 2: Add to CreateSystem request**

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

- [ ] **Step 3: Add to UpdateSystem request**

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

- [ ] **Step 4: Build**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add internal/api/handlers/systems.go
git -c user.name="col3name" -c user.email="mmikushov@instaloper.com" commit -m "api: expose require_key in systems CRUD"
```

---

### Task 4: Accept `access_token` in /process and enforce `require_key`

**Files:**
- Modify: `internal/api/handlers/process.go`
- Test: `internal/api/handlers/process_test.go`

**Interfaces:**
- Consumes: `SystemConfig.RequireKey` from Task 1.
- Produces: `ProcessRequest.AccessToken string` (json `access_token`); `authorize` accepts key from header OR body; `require_key` enforcement.

- [ ] **Step 1: Write the failing tests**

Append to `internal/api/handlers/process_test.go`:

```go
func doProcessSystemBody(t *testing.T, h *Handler, payload, id, system, accessToken string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(ProcessRequest{Payload: payload, PayloadID: id, System: system, AccessToken: accessToken})
	req := httptest.NewRequest("POST", "/process", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Process(rec, req)
	return rec
}

func TestProcessSystemAccessTokenInBody(t *testing.T) {
	h := newTestHandler(t, func(c *config.Config) {
		c.Systems = []config.SystemConfig{
			{Name: "chat", Enabled: true, APIKey: HashKey("sekret"), AllowUnmask: true},
		}
	})

	// Без токена -> 401.
	rec := doProcessSystemBody(t, h, "паспорт 4509 123456", "tok-1", "chat", "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	// Неверный токен -> 401.
	rec2 := doProcessSystemBody(t, h, "паспорт 4509 123456", "tok-1", "chat", "wrong")
	require.Equal(t, http.StatusUnauthorized, rec2.Code)

	// Верный токен в теле -> 200.
	rec3 := doProcessSystemBody(t, h, "паспорт 4509 123456", "tok-1", "chat", "sekret")
	require.Equal(t, http.StatusOK, rec3.Code)
	var resp3 ProcessResponse
	require.NoError(t, json.Unmarshal(rec3.Body.Bytes(), &resp3))
	require.Contains(t, resp3.Result, "[ПАСПОРТ]")
}

func TestProcessSystemRequireKey(t *testing.T) {
	h := newTestHandler(t, func(c *config.Config) {
		c.Systems = []config.SystemConfig{
			{Name: "locked", Enabled: true, RequireKey: true, AllowUnmask: true},
		}
	})

	// require_key=true, но ключ не задан -> все запросы 401.
	rec := doProcessSystem(t, h, "паспорт 4509 123456", "rk-1", "locked", "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	rec2 := doProcessSystemBody(t, h, "паспорт 4509 123456", "rk-1", "locked", "sekret")
	require.Equal(t, http.StatusUnauthorized, rec2.Code)
}

func TestProcessSystemRequireKeyWithKey(t *testing.T) {
	h := newTestHandler(t, func(c *config.Config) {
		c.Systems = []config.SystemConfig{
			{Name: "locked", Enabled: true, APIKey: HashKey("sekret"), RequireKey: true, AllowUnmask: true},
		}
	})

	// require_key=true + ключ задан: без ключа 401, с верным ключом 200.
	rec := doProcessSystem(t, h, "паспорт 4509 123456", "rk-2", "locked", "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	rec2 := doProcessSystemBody(t, h, "паспорт 4509 123456", "rk-2", "locked", "sekret")
	require.Equal(t, http.StatusOK, rec2.Code)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go test ./internal/api/handlers/ -run 'TestProcessSystemAccessTokenInBody|TestProcessSystemRequireKey' -v`
Expected: FAIL (AccessToken field not yet handled; require_key not enforced).

- [ ] **Step 3: Implement**

In `internal/api/handlers/process.go`:

Add `AccessToken` to `ProcessRequest`:

```go
type ProcessRequest struct {
	Payload     string `json:"payload"`
	PayloadID   string `json:"payload_id"`
	System      string `json:"system"`
	AccessToken string `json:"access_token"`
}
```

Update `authorize` to accept the key from either the header or the body token, and enforce `require_key`:

```go
func (h *Handler) authorize(r *http.Request, s *config.SystemConfig, bodyToken string) error {
	if s == nil {
		return nil
	}
	key := r.Header.Get("X-API-Key")
	if key == "" {
		key = bodyToken
	}
	if s.RequireKey {
		if s.APIKey == "" || key == "" || HashKey(key) != s.APIKey {
			return config.ErrUnauthorized
		}
		return nil
	}
	if s.APIKey == "" {
		return nil
	}
	if HashKey(key) == s.APIKey {
		return nil
	}
	return config.ErrUnauthorized
}
```

Update the call site in `Process`:

```go
if err := h.authorize(r, system, req.AccessToken); err != nil {
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go test ./internal/api/handlers/ -run 'TestProcessSystemAccessTokenInBody|TestProcessSystemRequireKey' -v`
Expected: PASS.

- [ ] **Step 5: Run full handler tests**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go test ./internal/api/handlers/`
Expected: PASS (existing `TestProcessSystemAPIKey` still passes — header path unchanged).

- [ ] **Step 6: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add internal/api/handlers/process.go internal/api/handlers/process_test.go
git -c user.name="col3name" -c user.email="mmikushov@instaloper.com" commit -m "api: accept access_token in /process and enforce require_key"
```

---

### Task 5: Update systems API tests for require_key

**Files:**
- Modify: `internal/api/handlers/systems_test.go`

**Interfaces:**
- Consumes: `systemView.RequireKey` from Task 3.
- Produces: test coverage for `require_key` in create/get/update.

- [ ] **Step 1: Add require_key assertions to TestSystemsCRUD**

In `internal/api/handlers/systems_test.go`, in `TestSystemsCRUD`, add `require_key` to the create body and assert it in the get view:

```go
body, _ := json.Marshal(map[string]any{
	"name":         "chat",
	"enabled":      true,
	"allow_unmask": true,
	"require_key":  true,
	"masking":      "token",
	"pii":          []detector.Type{detector.TypePhone, detector.TypeEmail},
})
```

After the first Get, add:

```go
require.True(t, view.RequireKey)
```

- [ ] **Step 2: Run tests**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go test ./internal/api/handlers/ -run TestSystemsCRUD -v`
Expected: PASS (skips if Postgres unavailable).

- [ ] **Step 3: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add internal/api/handlers/systems_test.go
git -c user.name="col3name" -c user.email="mmikushov@instaloper.com" commit -m "test: cover require_key in systems CRUD"
```

---

### Task 6: Add react-router-dom and update API types

**Files:**
- Modify: `web/package.json`
- Modify: `web/src/api.ts`

**Interfaces:**
- Consumes: nothing new.
- Produces: `react-router-dom` dependency; `SystemInfo.require_key`, `CreateSystemBody.require_key`, `UpdateSystemBody.require_key`.

- [ ] **Step 1: Install react-router-dom**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2/web && npm install react-router-dom`
Expected: adds `react-router-dom` to `package.json` and `package-lock.json`.

- [ ] **Step 2: Update api.ts types**

In `web/src/api.ts`, add `require_key` to the three interfaces:

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

- [ ] **Step 3: Typecheck**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2/web && npx tsc --noEmit`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add web/package.json web/package-lock.json web/src/api.ts
git -c user.name="col3name" -c user.email="mmikushov@instaloper.com" commit -m "web: add react-router-dom and require_key api types"
```

---

### Task 7: Create HomePage (team list)

**Files:**
- Create: `web/src/HomePage.tsx`

**Interfaces:**
- Consumes: `api.listSystems`, `api.createSystem`, `api.deleteSystem`; `SystemInfo`.
- Produces: `HomePage` — clickable team cards + create form + delete; navigates to `/teams/:name`.

- [ ] **Step 1: Write HomePage.tsx**

Create `web/src/HomePage.tsx`:

```tsx
import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { Button, Cell, Input, List, Section } from '@telegram-apps/telegram-ui';
import { api, type SystemInfo } from './api';

export default function HomePage() {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const { data: teams, isLoading } = useQuery<SystemInfo[]>({ queryKey: ['systems'], queryFn: api.listSystems });
  const [name, setName] = useState('');

  const invalidate = () => qc.invalidateQueries({ queryKey: ['systems'] });

  const create = useMutation({
    mutationFn: (n: string) => api.createSystem({ name: n }),
    onSuccess: () => { setName(''); invalidate(); },
  });
  const remove = useMutation({ mutationFn: (n: string) => api.deleteSystem(n), onSuccess: invalidate });

  if (isLoading) return <div>Загрузка…</div>;

  return (
    <Section header="Команды">
      <List>
        {(teams ?? []).map((t) => (
          <Cell
            key={t.name}
            subtitle={`${t.enabled ? 'включена' : 'отключена'} · режим: ${t.masking || 'глобальный'} · типов ПД: ${(t.pii ?? []).length === 0 ? 'все' : (t.pii ?? []).length}`}
            onClick={() => navigate(`/teams/${encodeURIComponent(t.name)}`)}
          >
            {t.name}
          </Cell>
        ))}
      </List>

      <Section header="Новая команда">
        <Input title="Имя" value={name} onChange={(e) => setName(e.target.value)} placeholder="chat" />
        <Button onClick={() => create.mutate(name)} disabled={!name || create.isPending}>Создать</Button>
      </Section>

      {(create.isError || remove.isError) && (
        <div style={{ color: 'red' }}>Ошибка: {String(create.error || remove.error)}</div>
      )}
    </Section>
  );
}
```

- [ ] **Step 2: Typecheck**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2/web && npx tsc --noEmit`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add web/src/HomePage.tsx
git -c user.name="col3name" -c user.email="mmikushov@instaloper.com" commit -m "web: home page with team list"
```

---

### Task 8: Create TeamDetailPage

**Files:**
- Create: `web/src/TeamDetailPage.tsx`

**Interfaces:**
- Consumes: `api.listSystems`, `api.getConfig`, `api.updateSystem`, `api.regenerateKey`, `api.deleteSystem`; `SystemInfo`, `ConfigView`.
- Produces: `TeamDetailPage` — full settings for one team (enabled, PII toggles, require_key, masking mode, allow_unmask, access key, usage examples, delete).

- [ ] **Step 1: Write TeamDetailPage.tsx**

Create `web/src/TeamDetailPage.tsx`:

```tsx
import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useParams } from 'react-router-dom';
import { Button, Cell, List, Section, Switch } from '@telegram-apps/telegram-ui';
import { api, type ConfigView, type SystemInfo } from './api';

export default function TeamDetailPage() {
  const { name = '' } = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { data: teams, isLoading } = useQuery<SystemInfo[]>({ queryKey: ['systems'], queryFn: api.listSystems });
  const { data: cfg } = useQuery<ConfigView>({ queryKey: ['config'], queryFn: api.getConfig });
  const [newKey, setNewKey] = useState('');

  const team = (teams ?? []).find((t) => t.name === name);
  const knownTypes = cfg?.known_types ?? [];

  const invalidate = () => qc.invalidateQueries({ queryKey: ['systems'] });

  const update = useMutation({
    mutationFn: (body: { enabled?: boolean; pii?: string[]; require_key?: boolean; masking?: string; allow_unmask?: boolean }) =>
      api.updateSystem(name, body),
    onSuccess: invalidate,
  });
  const regen = useMutation({
    mutationFn: () => api.regenerateKey(name),
    onSuccess: (res) => setNewKey(res.access_key),
  });
  const remove = useMutation({
    mutationFn: () => api.deleteSystem(name),
    onSuccess: () => navigate('/'),
  });

  if (isLoading) return <div>Загрузка…</div>;
  if (!team) return <div>Команда не найдена</div>;

  const copy = (v: string) => navigator.clipboard?.writeText(v);

  const enabledSet = new Set(team.pii ?? []);
  const allEnabled = (team.pii ?? []).length === 0;

  const toggleType = (type: string, on: boolean) => {
    if (on) {
      if (allEnabled) return;
      const next = [...(team.pii ?? []), type];
      const coversAll = knownTypes.every((t) => next.includes(t));
      update.mutate({ pii: coversAll ? [] : next });
    } else {
      const base = allEnabled ? knownTypes : (team.pii ?? []);
      const next = base.filter((t) => t !== type);
      update.mutate({ pii: next });
    }
  };

  const accessKey = newKey || (team.api_key_set ? '••••••••••••••••••••••••••••••••' : '');

  const curlMask = `curl -s -X POST http://localhost:8080/process \\
  -H "X-API-Key: ${accessKey}" \\
  -H 'Content-Type: application/json' \\
  -d '{"payload":"паспорт 4509 123456","payload_id":"demo1","system":"${team.name}"}'`;

  const fetchMask = `fetch('http://localhost:8080/process', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json', 'X-API-Key': '${accessKey}' },
  body: JSON.stringify({ payload: 'паспорт 4509 123456', payload_id: 'demo1', system: '${team.name}' })
})`;

  return (
    <Section header={team.name}>
      <Cell>
        <Button size="s" onClick={() => navigate('/')}>← Назад</Button>
      </Cell>

      <Cell
        subtitle={team.enabled ? 'Доступ включён' : 'Доступ отключён'}
        after={<Switch checked={team.enabled} onChange={(e) => update.mutate({ enabled: e.target.checked })} />}
      >
        {team.enabled ? 'Включена' : 'Отключена'}
      </Cell>

      <Section header="Обнаружение типов ПД">
        <List>
          {knownTypes.map((t) => (
            <Cell
              key={t}
              subtitle={allEnabled || enabledSet.has(t) ? 'маскируется' : 'не маскируется'}
              after={<Switch checked={allEnabled || enabledSet.has(t)} onChange={(e) => toggleType(t, e.target.checked)} />}
            >
              {t}
            </Cell>
          ))}
        </List>
      </Section>

      <Section header="Управление доступом">
        <Cell
          subtitle="требовать access key для /process"
          after={<Switch checked={team.require_key} onChange={(e) => update.mutate({ require_key: e.target.checked })} />}
        >
          Требовать access key
        </Cell>
        <Cell
          subtitle="разрешить демаскирование"
          after={<Switch checked={team.allow_unmask} onChange={(e) => update.mutate({ allow_unmask: e.target.checked })} />}
        >
          Разрешить демаскирование
        </Cell>
        <Cell subtitle="режим маскирования">
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            {['redact', 'token', 'synthetic'].map((m) => (
              <Button
                key={m}
                size="s"
                mode={team.masking === m ? 'filled' : 'bezeled'}
                onClick={() => update.mutate({ masking: m })}
              >
                {m}
              </Button>
            ))}
          </div>
        </Cell>
      </Section>

      <Section header="AccessKey">
        <Cell subtitle="ключ для вызовов /process">
          <code>{accessKey || 'Ключ не задан'}</code>
        </Cell>
        <Cell>
          <Button size="s" onClick={() => regen.mutate()}>Новый ключ</Button>
          {accessKey && <Button size="s" onClick={() => copy(accessKey)}>Копировать</Button>}
        </Cell>
      </Section>

      {accessKey && (
        <Section header="Примеры использования">
          <Cell subtitle="curl — маскирование">
            <pre style={{ fontSize: 11, whiteSpace: 'pre-wrap' }}>{curlMask}</pre>
          </Cell>
          <Cell subtitle="fetch — маскирование">
            <pre style={{ fontSize: 11, whiteSpace: 'pre-wrap' }}>{fetchMask}</pre>
          </Cell>
          <Cell subtitle="демаскирование — тот же запрос, но payload = результат маскирования, тот же payload_id">
            <pre style={{ fontSize: 11, whiteSpace: 'pre-wrap' }}>{`// повторный запрос с маской возвращает оригинал`}</pre>
          </Cell>
        </Section>
      )}

      <Cell>
        <Button size="s" onClick={() => remove.mutate()}>Удалить команду</Button>
      </Cell>

      {(update.isError || regen.isError || remove.isError) && (
        <div style={{ color: 'red' }}>Ошибка: {String(update.error || regen.error || remove.error)}</div>
      )}
    </Section>
  );
}
```

- [ ] **Step 2: Typecheck**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2/web && npx tsc --noEmit`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add web/src/TeamDetailPage.tsx
git -c user.name="col3name" -c user.email="mmikushov@instaloper.com" commit -m "web: team detail page with settings and access management"
```

---

### Task 9: Wire routes in App, remove TeamsTab/TeamCard

**Files:**
- Modify: `web/src/App.tsx`
- Delete: `web/src/TeamsTab.tsx`
- Delete: `web/src/TeamCard.tsx`

**Interfaces:**
- Consumes: `HomePage`, `TeamDetailPage`.
- Produces: `App` with `BrowserRouter` and routes `/` → `HomePage`, `/teams/:name` → `TeamDetailPage`.

- [ ] **Step 1: Update App.tsx**

Replace `web/src/App.tsx`:

```tsx
import { useState } from 'react';
import { BrowserRouter, Route, Routes } from 'react-router-dom';
import { Button } from '@telegram-apps/telegram-ui';
import LoginScreen from './LoginScreen';
import HomePage from './HomePage';
import TeamDetailPage from './TeamDetailPage';
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
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<HomePage />} />
          <Route path="/teams/:name" element={<TeamDetailPage />} />
        </Routes>
      </BrowserRouter>
      <Button onClick={async () => { try { await api.logout(); } finally { setToken(''); setAuthed(false); } }}>
        Выйти
      </Button>
    </div>
  );
}
```

- [ ] **Step 2: Delete TeamsTab.tsx and TeamCard.tsx**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2/web && rm src/TeamsTab.tsx src/TeamCard.tsx`

- [ ] **Step 3: Typecheck + build**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2/web && npx tsc --noEmit && npm run build`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add web/src/App.tsx
git rm web/src/TeamsTab.tsx web/src/TeamCard.tsx
git -c user.name="col3name" -c user.email="mmikushov@instaloper.com" commit -m "web: route home and team detail pages"
```

---

### Task 10: End-to-end verification

**Files:**
- Verify only (no new files unless a fix is needed).

- [ ] **Step 1: Backend tests**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2 && go test ./...`
Expected: PASS.

- [ ] **Step 2: Frontend build**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2/web && npm run build`
Expected: PASS.

- [ ] **Step 3: Verify UI in browser**

Run: `cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2/web && npm run dev`
Open http://localhost:5173, log in (admin/admin123), verify:
- Home page lists teams as clickable cards.
- Clicking a team opens `/teams/:name` with all settings.
- Toggling require_key / allow_unmask / masking persists (verify via `curl -s localhost:8080/v1/systems -H "Authorization: Bearer $TOKEN"`).
- Back button returns to the list.
- Delete team returns to the list.

- [ ] **Step 4: Final commit (if any fixes)**

```bash
cd /Users/mikha/Desktop/github/ai/prc-data-challenge-2026/pii-module/version2
git add -A
git -c user.name="col3name" -c user.email="mmikushov@instaloper.com" commit -m "chore: team detail page fixes from verification"
```
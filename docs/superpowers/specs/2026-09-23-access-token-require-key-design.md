# access_token in /process + per-system "require key" checkbox

## Problem

The `/process` endpoint authorizes consumer systems via the `X-API-Key` header.
The key is optional: a system with an empty `api_key` is authorized implicitly.
There is no way for an admin to force a system to authenticate, and no way for a
client to pass the key in the request body.

## Goal

1. Accept an optional `access_token` field in the `/process` request body. It is
   the same system API key, just passed in the body instead of the header.
2. Add a per-system checkbox "Требовать access key" in the admin UI. When
   enabled, `/process` rejects requests for that system unless a valid key is
   provided (401). If the checkbox is enabled but the system has no key, all
   requests are rejected until the admin generates a key.

## Non-goals

- No new key type or separate token store. `access_token` reuses the existing
  system API key.
- No changes to the admin session auth (`/v1/auth/*`).

## Design

### 1. Config — `internal/config/config.go`

Add `RequireKey bool` to `SystemConfig`:

```go
type SystemConfig struct {
    Name        string          `yaml:"name"`
    APIKey      string          `yaml:"api_key"`
    Enabled     bool            `yaml:"enabled"`
    PII         []detector.Type `yaml:"pii"`
    Masking     string          `yaml:"masking"`
    AllowUnmask bool            `yaml:"allow_unmask"`
    RequireKey  bool            `yaml:"require_key"` // new
}
```

### 2. DB — `internal/db/db.go`, `internal/db/systems.go`

- Add `require_key boolean NOT NULL DEFAULT false` to the `systems` table
  migration in `db.go`.
- Update `ListSystems`, `GetSystem`, `CreateSystem`, `UpdateSystem` in
  `systems.go` to read/write the new column.

### 3. API — `internal/api/handlers/systems.go`

- Add `RequireKey bool` to `systemView`.
- Add `RequireKey *bool` to the create and update request structs.
- `toView` sets `RequireKey: s.RequireKey`.

### 4. `/process` handler — `internal/api/handlers/process.go`

- Add `AccessToken string` to `ProcessRequest`.
- Update `authorize` to accept the key from either the `X-API-Key` header or the
  `access_token` body field.
- When `system.RequireKey` is true, reject (401) if no valid key is provided.
  A system with `RequireKey` true and no configured key rejects all requests.

### 5. Web UI — `web/src/api.ts`, `web/src/SystemsTab.tsx`

- Add `require_key?: boolean` to `SystemInfo`, `CreateSystemBody`,
  `UpdateSystemBody`.
- Add a checkbox "Требовать access key" in each system card that toggles
  `require_key` via `updateSystem`.

### 6. Tests

- Update `internal/api/handlers/process_test.go` for the new auth logic
  (body token, required-key rejection).
- Update `internal/api/handlers/systems_test.go` for the new field.

## Data flow

```
POST /process
  body: { payload, payload_id, system, access_token? }
  → authorize(system, header X-API-Key OR body access_token)
    → if system.RequireKey and no valid key → 401
    → if system.RequireKey and system has no key → 401
  → mask / unmask
```

## Verification

- `go test ./...` passes.
- `cd web && npm run build` passes.
- Manual: enable "Требовать access key" on a system, POST /process without a
  key → 401; with the correct key in `access_token` → 200.

## Confirmed decisions (brainstorming 2026-09-23)

- `access_token` in the body is the SAME system API key (sha256 vs `api_key_hash`),
  just an alternative way to pass it. No separate token store.
- Key priority: `X-API-Key` header wins; if absent, use body `access_token`.
- By default `/process` is open to all (no key required) unless the admin
  enables the "Требовать access key" flag.
- If `require_key=true` and the system has no key → all requests 401 until a
  key is generated.
- Auth matrix:
  - `require_key=true`, no system key → 401 always.
  - `require_key=true`, key provided and valid → 200.
  - `require_key=true`, key missing/invalid → 401.
  - `require_key=false`, no key → 200 (open).
  - `require_key=false`, valid key → 200.
  - `require_key=false`, invalid key → 401 (a wrong key is still rejected).
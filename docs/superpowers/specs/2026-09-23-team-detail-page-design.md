# Home page with team list + team detail page

## Problem

The admin UI currently renders all team settings inline on a single page
(`TeamsTab` + `TeamCard`). There is no navigation: the user sees every team's
full settings at once. There is also no way to manage access per team beyond
the existing toggles (enabled, PII types, access key).

## Goal

1. Add a home page that lists all teams as clickable cards.
2. Clicking a team opens a detail page with all settings and access management.
3. Add new per-team access fields on the detail page:
   - "Требовать access key" checkbox (`require_key`)
   - Masking mode selector (`redact` / `token` / `synthetic`)
   - "Разрешить демаскирование" checkbox (`allow_unmask`)

## Non-goals

- No changes to the `/process` masking logic beyond enforcing `require_key`.
- No new PII detectors.

## Design

### Frontend — `web/`

Use `react-router-dom` for navigation.

1. **Add dependency** `react-router-dom`.

2. **`App.tsx`** — wrap in `BrowserRouter` with routes:
   - `/` → `HomePage`
   - `/teams/:name` → `TeamDetailPage`
   - Auth gate stays at the top level.

3. **`HomePage.tsx`** (new) — fetches `api.listSystems()`, renders each team as
   a clickable card (name + summary: enabled, masking mode, PII count). Clicking
   navigates to `/teams/:name`. Includes a "Новая команда" form and delete.

4. **`TeamDetailPage.tsx`** (new) — fetches the team by `:name`, renders:
   - Back button (navigate to `/`)
   - Enabled toggle
   - PII type toggles (from `known_types`)
   - Access key (view / regenerate / copy)
   - Usage examples (curl / fetch)
   - **NEW** "Требовать access key" checkbox → `require_key`
   - **NEW** Masking mode selector → `masking`
   - **NEW** "Разрешить демаскирование" checkbox → `allow_unmask`

5. **`TeamCard.tsx`** — removed (its content moves to `TeamDetailPage`).

### Backend — `require_key` field

`require_key` does not exist yet. Implement it:

1. **`internal/config/config.go`** — add `RequireKey bool` to `SystemConfig`.

2. **`internal/db/db.go`** — add `require_key boolean NOT NULL DEFAULT false`
   to the `systems` table migration.

3. **`internal/db/systems.go`** — update `ListSystems`, `GetSystem`,
   `CreateSystem`, `UpdateSystem` to read/write the new column.

4. **`internal/api/handlers/systems.go`** — add `RequireKey bool` to
   `systemView`; add `RequireKey *bool` to create/update request structs;
   `toView` sets `RequireKey: s.RequireKey`.

5. **`internal/api/handlers/process.go`** — add `AccessToken string` to
   `ProcessRequest`; update `authorize` to accept the key from either the
   `X-API-Key` header or the `access_token` body field; when
   `system.RequireKey` is true, reject (401) if no valid key is provided.

### API types — `web/src/api.ts`

Add `require_key?: boolean`, `allow_unmask?: boolean`, `masking?: string` to
`SystemInfo`, `CreateSystemBody`, `UpdateSystemBody`.

### Tests

- Update `internal/api/handlers/process_test.go` for the new auth logic.
- Update `internal/api/handlers/systems_test.go` for the new field.

## Data flow

```
GET / (home) → list teams → click team → /teams/:name
  → TeamDetailPage fetches team + known_types
  → toggles call PUT /v1/systems/{name} with { enabled, pii, require_key, masking, allow_unmask }
  → Mgr.Reload() rebuilds the per-team pipeline
```

## Verification

- `go test ./...` passes.
- `cd web && npm run build` passes.
- Manual: home page lists teams; clicking opens detail; toggles persist and
  affect `/process` behavior.
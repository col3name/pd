# Synthetic Masking Mode

## Problem

Consumer systems currently support two masking modes: `redact` (replace PII with
a type placeholder like `[ФИО]`) and `token` (replace with a reversible token
like `[PERSON_001]`). There is no way to replace PII with realistic synthetic
values, which is useful for demos, test data, and systems that need
human-readable output without exposing real personal data.

## Goal

Add a third masking mode `synthetic` that replaces each detected PII span with a
realistic synthetic value (e.g. `Иванов Иван Иванович` → `Петров Пётр Петрович`,
`+7 999 123-45-67` → `+7 900 000-00-00`). Expose it in the admin UI as a
checkbox "Замена синтетическими данными" per consumer system.

## Non-goals

- No unmasking of synthetic values (synthetic output is not reversible).
- No ML/NER changes.
- No new PII detectors (identity-document detection is a separate task).

## Design

### 1. Synthetic generator — `internal/masker/synthetic.go` (new)

A `Synthetic(text string, spans []detector.Span) string` function that replaces
each span with a synthetic value based on its type. Spans must be sorted by
`Start` ascending; replacement is done left-to-right by building a new string so
byte offsets remain valid (same approach as `masker.Mask`).

Synthetic values per type:

| Type | Synthetic value |
|------|-----------------|
| ФИО | random Russian name (`Петров Пётр Петрович`) |
| ТЕЛЕФОН | `+7 900 000-00-00` |
| EMAIL | `user123@example.com` |
| КАРТА | valid Luhn 16-digit number |
| ПАСПОРТ / ВУ / ЗАГРАНПАСПОРТ / ВОЕННЫЙ_БИЛЕТ / СВИДЕТЕЛЬСТВО_О_РОЖДЕНИИ | random series + number |
| ИНН | random 10/12 digits |
| ДАТА / ДАТА_РОЖДЕНИЯ | random date `dd.mm.yyyy` |
| CVV | random 3 digits |
| ПИН | random 4 digits |
| АДРЕС | random street + city |
| ДЕРЖАТЕЛЬ | random name |
| fallback | `[TYPE]` placeholder |

Determinism: the synthetic value for a given span is derived from a hash of the
span's original text, so the same input always yields the same synthetic output.
This keeps the benchmark retry logic in `process.go` working (a re-sent payload
must produce the same masked result).

### 2. Pipeline — `internal/pipeline/pipeline.go`

Add a `synthetic` branch in `Process()`:

```go
if p.opts.Mode == "token" {
    masked, tokens = masker.Tokenize(text, kept)
} else if p.opts.Mode == "synthetic" {
    masked = masker.Synthetic(text, kept)
} else {
    masked = masker.Mask(text, kept)
}
```

### 3. Config — `internal/config/config.go`

Update the `SystemConfig.Masking` field comment to document the third mode:
`"redact" | "token" | "synthetic"`.

### 4. API — `internal/api/handlers/systems.go`

- Add `Synthetic bool` to `systemView` (derived from `Masking == "synthetic"`).
- Add `Synthetic *bool` to the create and update request structs. When set:
  - `true` → `Masking = "synthetic"`
  - `false` → `Masking = ""` (fall back to global mode)
- `toView` sets `Synthetic: s.Masking == "synthetic"`.

### 5. DB — `internal/db/systems.go`

No schema change. The synthetic flag is stored as `masking = 'synthetic'` in the
existing `masking` text column.

### 6. Web UI — `web/src/api.ts`, `web/src/SystemsTab.tsx`

- Add `synthetic?: boolean` to `SystemInfo`, `CreateSystemBody`, `UpdateSystemBody`.
- Add a checkbox "Замена синтетическими данными" in each system card that
  toggles `masking: "synthetic"` via `updateSystem`.

### 7. Tests

- `internal/masker/synthetic_test.go` — unit tests for each type and determinism.
- Update `internal/pipeline/pipeline_test.go` for the synthetic mode.

## Data flow

```
POST /process (system with masking=synthetic)
  → pipeline.Process
    → detect → resolve → whitelist → context → gate
    → masker.Synthetic(text, kept)   // replaces spans with synthetic values
  → store.Save(original, masked)
  → returns masked text
```

## Verification

- `go test ./...` passes.
- `cd web && npm run build` passes.
- Manual: create a system with the synthetic checkbox, POST a payload with PII,
  confirm the result contains synthetic values, not placeholders.
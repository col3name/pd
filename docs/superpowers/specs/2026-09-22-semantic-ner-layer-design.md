# Semantic NER Layer — Design

Date: 2026-09-22
Status: Approved

## Goal

Add a rule-based semantic NER layer to the existing PII detector so that
context-dependent personal data (FIO, address, birthplace, citizenship, issuer,
cardholder) is masked only when it is genuinely personal data, not when it is a
literary mention or a bank/office address. This is the "smart path" of the
two-contour architecture described in AGENTS.md.

## Background

The project is at V1/V2. The existing `internal/detector` package has regex
rules (`StructuredRules`) that detect structured PII (passport, INN, card, CVV,
PIN, email, phone, dates) and a few context-capture rules (birthplace,
citizenship, issuer, address, cardholder). Detection returns `[]Span` with
`Start`, `End`, `Type`, `Priority`. Masking replaces spans with type
placeholders.

The gap: the regex layer has no confidence scoring and no dictionary/context
disambiguation. It cannot tell "Клиент Иванов Иван Иванович" (PII) from
"Александр Пушкин написал..." (not PII), nor "адрес клиента" (PII) from
"адрес банка" (not PII).

## Approach

Rule-based semantic NER layer that **enriches** the spans produced by the regex
detector. It does not replace the regex layer. It:

1. Adds a `Confidence float32` field to each `Span`.
2. Computes confidence for every detected span from context keywords,
   dictionary hits, and negative context.
3. Adds new spans for entities the regex layer cannot catch (FIO, address
   without a keyword) using dictionaries + context.
4. Applies a 3-threshold confidence gate to decide whether a span is masked.

## Architecture

```
text
  │
  ▼
Regex Detector (existing, fast path) ──→ spans (no confidence)
  │
  ▼
Semantic NER (new, smart path) ──→ adds Confidence to each span + new spans
  │
  ▼
Confidence Gate (3-threshold) ──→ filter spans
  │
  ▼
Conflict Resolver (existing ResolveOverlaps)
  │
  ▼
Masker
```

The semantic layer runs inside `Detector.Detect()`. The handler applies the
confidence gate before masking.

## Confidence Scoring

Additive formula:

```
confidence = base (0.5)
           + dictionary_hit (0.3)
           + context_keyword (0.2 each, capped at +0.4)
           - negative_context (0.3)
```

Clamped to [0, 1].

### Context keywords (raise confidence)

- **FIO**: клиент, заёмщик, владелец, получатель, заявитель, паспорт, договор,
  ФИО, имя, фамилия, отчество, держатель
- **Address**: адрес клиента, домашний адрес, проживает, зарегистрирован,
  адрес регистрации, прописан, место жительства
- **Birthplace**: родился в, родилась в, место рождения
- **Citizenship**: гражданство, гражданин, гражданка
- **Issuer**: выдан, выдал, орган выдавший, кем выдан
- **Cardholder**: держатель карты, cardholder, имя держателя

### Negative context (lower confidence)

- **Address**: отделение, офис, банк, филиал, магазин, находится по адресу

### Dictionaries

- **First names**: Иван, Александр, Пётр, Сергей, Дмитрий, Андрей, Алексей,
  Николай, Михаил, Владимир, Павел, Артём, Максим, Елена, Ольга, Мария, Анна,
  Наталья, Татьяна, Ирина, Светлана
- **Surname patterns**: -ов, -ев, -ин, -ский, -цкий, -ко, -чук, -енко
- **Cities**: Москва, Санкт-Петербург, Новосибирск, Екатеринбург, Казань,
  Нижний Новгород, Челябинск, Самара, Омск, Ростов-на-Дону, Уфа, Красноярск,
  Воронеж, Пермь, Волгоград

## 3-Threshold Gate

- `confidence >= 0.95` → mask always
- `0.75 <= confidence < 0.95` → mask only if a context keyword is present
- `confidence < 0.75` → do not mask

### Worked examples

| Text | Type | Score | Mask? |
|------|------|-------|-------|
| "Клиент Иванов Иван Иванович" | FIO | 0.5 + 0.3 (surname) + 0.2 (клиент) = 1.0 | yes |
| "Александр Пушкин написал..." | FIO | 0.5 + 0.3 (name) = 0.8, no context | no |
| "Банк находится по адресу Москва, ул. Тверская, 10" | Address | 0.5 + 0.3 (city) - 0.3 (банк) = 0.5 | no |
| "адрес клиента: г. Москва, ул. Ленина, д. 10" | Address | 0.5 + 0.3 (city) + 0.2 (адрес клиента) = 1.0 | yes |

## FIO Detection Algorithm

1. Tokenize text into words (split on whitespace/punctuation).
2. For each word, check if it is a known first name (dictionary) or matches a
   surname pattern.
3. Group consecutive name tokens into a FIO candidate span.
4. Compute confidence: base + name/surname hits + context keywords in a window
   around the span.
5. Apply the 3-threshold gate.

## Address Detection

- Look for city names (dictionary) + street patterns (ул., улица, проспект,
  переулок, etc.) in a window.
- Compute confidence with context + negative context.

## Code Changes

- `internal/detector/span.go`: add `Confidence float32` field to `Span`.
- `internal/detector/semantic.go` (new): semantic layer — dictionaries, context
  keywords, confidence scoring, FIO/address detection.
- `internal/detector/detector.go`: run semantic layer after regex rules inside
  `Detect()`.
- `internal/api/handlers/process.go`: apply the 3-threshold confidence gate
  before masking.
- `internal/detector/accuracy_test.go`: extend with adversarial cases.

## Testing

Extend `accuracy_test.go` with adversarial cases:

- "Александр Пушкин написал..." → no FIO
- "Банк находится по адресу Москва, ул. Тверская, 10" → no address
- "Клиент Иванов Иван Иванович" → FIO
- "адрес клиента: г. Москва, ул. Ленина, д. 10" → address
- "пин 1234" → no PIN (already handled)
- "пин 1234, карта 4276 1234 5678 9012" → PIN

## Out of Scope

- ONNX / real ML NER model (future, optional).
- 100k-token chunking (separate V4 item).
- Grafana / demo UI (separate V4 item).
- Configurable per-system masking policies (V3).
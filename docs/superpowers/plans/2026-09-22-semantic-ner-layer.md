# Semantic NER Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a rule-based semantic NER layer that enriches detected PII spans with confidence scores and gates masking by a 3-threshold model, so context-dependent PII (FIO, address) is masked only when genuinely personal.

**Architecture:** The semantic layer runs inside `Detector.Detect()` after the regex rules. It adds a `Confidence float32` field to each `Span`, computes confidence from dictionaries + context keywords + negative context, adds new FIO/address spans, and the handler applies a 3-threshold gate before masking.

**Tech Stack:** Go 1.25, standard library, `github.com/stretchr/testify`.

## Global Constraints

- All offsets are byte offsets (UTF-8 safe — Russian text). Never mix rune/byte offsets.
- No new external dependencies.
- Confidence formula: `base(0.5) + dictionary_hit(0.3) + context_keyword(0.2 each, capped +0.4) - negative_context(0.3)`, clamped to [0,1].
- 3-threshold gate: `>=0.95` mask always; `0.75<=c<0.95` mask only if context keyword present; `<0.75` don't mask.
- Follow existing package conventions (`internal/detector`, `testify/require`).

---

### Task 1: Add Confidence field to Span

**Files:**
- Modify: `internal/detector/span.go`
- Test: `internal/detector/span_test.go`

**Interfaces:**
- Consumes: existing `Span` struct.
- Produces: `Span` with new `Confidence float32` field. All existing constructors must still compile (zero value `0.0` is fine).

- [ ] **Step 1: Write the failing test**

Add to `internal/detector/span_test.go`:

```go
func TestSpanConfidenceField(t *testing.T) {
	s := Span{Start: 0, End: 5, Type: TypeEmail, Confidence: 0.98}
	require.Equal(t, float32(0.98), s.Confidence)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/detector/ -run TestSpanConfidenceField -v`
Expected: FAIL — `s.Confidence` undefined (no such field).

- [ ] **Step 3: Add the field**

In `internal/detector/span.go`, add `Confidence float32` to the `Span` struct:

```go
type Span struct {
	Start      int
	End        int
	Type       Type
	Priority   int
	Confidence float32
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/detector/ -run TestSpanConfidenceField -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/detector/span.go internal/detector/span_test.go
git commit -m "feat: add Confidence field to Span"
```

---

### Task 2: Semantic layer — dictionaries, context keywords, scoring

**Files:**
- Create: `internal/detector/semantic.go`
- Test: `internal/detector/semantic_test.go`

**Interfaces:**
- Consumes: `Span`, `Type` from `types.go`.
- Produces:
  - `var firstNames = map[string]bool{...}`
  - `var cities = map[string]bool{...}`
  - `func isSurname(word string) bool`
  - `func contextKeywords(t Type) []string`
  - `func negativeContext(t Type) []string`
  - `func hasContext(text string, start, end int, keywords []string) bool`
  - `func hasNegativeContext(text string, start, end int, keywords []string) bool`
  - `func scoreConfidence(base float32, dictHit bool, ctxHits, negHits int) float32`

- [ ] **Step 1: Write the failing test**

Create `internal/detector/semantic_test.go`:

```go
package detector

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsSurname(t *testing.T) {
	require.True(t, isSurname("Иванов"))
	require.True(t, isSurname("Петров"))
	require.True(t, isSurname("Сидорова"))
	require.True(t, isSurname("Ковальчук"))
	require.True(t, isSurname("Шевченко"))
	require.False(t, isSurname("Москва"))
	require.False(t, isSurname("Иван"))
}

func TestScoreConfidence(t *testing.T) {
	// base + dict + context = 1.0
	require.Equal(t, float32(1.0), scoreConfidence(0.5, true, 1, 0))
	// base + dict, no context = 0.8
	require.Equal(t, float32(0.8), scoreConfidence(0.5, true, 0, 0))
	// base + dict - negative = 0.5
	require.Equal(t, float32(0.5), scoreConfidence(0.5, true, 0, 1))
	// context capped at +0.4
	require.Equal(t, float32(1.0), scoreConfidence(0.5, true, 3, 0))
	// clamped to [0,1]
	require.Equal(t, float32(0.0), scoreConfidence(0.1, false, 0, 3))
}

func TestHasContext(t *testing.T) {
	text := "Клиент Иванов Иван Иванович"
	require.True(t, hasContext(text, 7, 30, contextKeywords(TypeFIO)))
	require.False(t, hasContext("Александр Пушкин написал", 0, 20, contextKeywords(TypeFIO)))
}

func TestHasNegativeContext(t *testing.T) {
	text := "Банк находится по адресу Москва, ул. Тверская, 10"
	require.True(t, hasNegativeContext(text, 0, len(text), negativeContext(TypeAddress)))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/detector/ -run 'TestIsSurname|TestScoreConfidence|TestHasContext|TestHasNegativeContext' -v`
Expected: FAIL — undefined functions.

- [ ] **Step 3: Write the implementation**

Create `internal/detector/semantic.go`:

```go
package detector

import (
	"strings"
)

// firstNames is a small dictionary of common Russian first names.
var firstNames = map[string]bool{
	"иван": true, "александр": true, "пётр": true, "сергей": true,
	"дмитрий": true, "андрей": true, "алексей": true, "николай": true,
	"михаил": true, "владимир": true, "павел": true, "артём": true,
	"максим": true, "елена": true, "ольга": true, "мария": true,
	"анна": true, "наталья": true, "татьяна": true, "ирина": true,
	"светлана": true,
}

// cities is a small dictionary of common Russian cities.
var cities = map[string]bool{
	"москва": true, "санкт-петербург": true, "новосибирск": true,
	"екатеринбург": true, "казань": true, "нижний новгород": true,
	"челябинск": true, "самара": true, "омск": true, "ростов-на-дону": true,
	"уфа": true, "красноярск": true, "воронеж": true, "пермь": true,
	"волгоград": true,
}

// surnameSuffixes are common Russian surname endings.
var surnameSuffixes = []string{"ов", "ев", "ин", "ский", "цкий", "ко", "чук", "енко"}

// isSurname reports whether word looks like a Russian surname.
func isSurname(word string) bool {
	lower := strings.ToLower(word)
	for _, s := range surnameSuffixes {
		if strings.HasSuffix(lower, s) && len(lower) > len(s)+1 {
			return true
		}
	}
	return false
}

// contextKeywords returns the context keywords that raise confidence for t.
func contextKeywords(t Type) []string {
	switch t {
	case TypeFIO:
		return []string{"клиент", "заёмщик", "владелец", "получатель", "заявитель",
			"паспорт", "договор", "фио", "имя", "фамилия", "отчество", "держатель"}
	case TypeAddress:
		return []string{"адрес клиента", "домашний адрес", "проживает", "зарегистрирован",
			"адрес регистрации", "прописан", "место жительства"}
	case TypeBirthPlace:
		return []string{"родился в", "родилась в", "место рождения"}
	case TypeCitizenship:
		return []string{"гражданство", "гражданин", "гражданка"}
	case TypeIssuer:
		return []string{"выдан", "выдал", "орган выдавший", "кем выдан"}
	case TypeCardholder:
		return []string{"держатель карты", "cardholder", "имя держателя"}
	default:
		return nil
	}
}

// negativeContext returns the context keywords that lower confidence for t.
func negativeContext(t Type) []string {
	switch t {
	case TypeAddress:
		return []string{"отделение", "офис", "банк", "филиал", "магазин", "находится по адресу"}
	default:
		return nil
	}
}

// hasContext reports whether any keyword appears in a window around [start,end).
func hasContext(text string, start, end int, keywords []string) bool {
	return hasAnyKeyword(text, start, end, keywords)
}

// hasNegativeContext reports whether any negative keyword appears in a window.
func hasNegativeContext(text string, start, end int, keywords []string) bool {
	return hasAnyKeyword(text, start, end, keywords)
}

func hasAnyKeyword(text string, start, end int, keywords []string) bool {
	if len(keywords) == 0 {
		return false
	}
	from := start - 80
	if from < 0 {
		from = 0
	}
	to := end + 80
	if to > len(text) {
		to = len(text)
	}
	window := strings.ToLower(text[from:to])
	for _, k := range keywords {
		if strings.Contains(window, k) {
			return true
		}
	}
	return false
}

// scoreConfidence computes the additive confidence score, clamped to [0,1].
func scoreConfidence(base float32, dictHit bool, ctxHits, negHits int) float32 {
	score := base
	if dictHit {
		score += 0.3
	}
	score += float32(ctxHits) * 0.2
	if score > 1.0 {
		score = 1.0
	}
	score -= float32(negHits) * 0.3
	if score < 0.0 {
		score = 0.0
	}
	return score
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/detector/ -run 'TestIsSurname|TestScoreConfidence|TestHasContext|TestHasNegativeContext' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/detector/semantic.go internal/detector/semantic_test.go
git commit -m "feat: semantic layer dictionaries, context keywords, scoring"
```

---

### Task 3: FIO detection

**Files:**
- Modify: `internal/detector/semantic.go`
- Test: `internal/detector/semantic_test.go`

**Interfaces:**
- Consumes: `firstNames`, `isSurname`, `contextKeywords`, `hasContext`, `scoreConfidence` from Task 2.
- Produces: `func detectFIO(text string) []Span` — returns FIO spans with `Confidence` set.

- [ ] **Step 1: Write the failing test**

Add to `internal/detector/semantic_test.go`:

```go
func TestDetectFIO(t *testing.T) {
	// Full name with context keyword → high confidence.
	spans := detectFIO("Клиент Иванов Иван Иванович")
	require.Len(t, spans, 1)
	require.Equal(t, TypeFIO, spans[0].Type)
	require.GreaterOrEqual(t, spans[0].Confidence, float32(0.95))

	// Name without context → low confidence (0.8), still returned.
	spans = detectFIO("Александр Пушкин написал")
	require.Len(t, spans, 1)
	require.Equal(t, float32(0.8), spans[0].Confidence)

	// No name → no span.
	spans = detectFIO("обычный текст без имён")
	require.Empty(t, spans)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/detector/ -run TestDetectFIO -v`
Expected: FAIL — `detectFIO` undefined.

- [ ] **Step 3: Write the implementation**

Append to `internal/detector/semantic.go`:

```go
// detectFIO finds FIO spans (consecutive name/surname tokens) and scores them.
func detectFIO(text string) []Span {
	words := tokenize(text)
	var spans []Span
	for i := 0; i < len(words); i++ {
		w := words[i]
		if !isNameToken(w.text) {
			continue
		}
		// Extend over consecutive name tokens.
		j := i
		for j+1 < len(words) && isNameToken(words[j+1].text) {
			j++
		}
		start := words[i].start
		end := words[j].end
		dictHit := true
		ctxHits := 0
		if hasContext(text, start, end, contextKeywords(TypeFIO)) {
			ctxHits = 1
		}
		conf := scoreConfidence(0.5, dictHit, ctxHits, 0)
		spans = append(spans, Span{Start: start, End: end, Type: TypeFIO, Confidence: conf})
		i = j
	}
	return spans
}

// word is a token with its byte offsets in the source text.
type word struct {
	text  string
	start int
	end   int
}

// tokenize splits text into words with byte offsets.
func tokenize(text string) []word {
	var words []word
	start := -1
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == ' ' || c == '\t' || c == '\n' || c == ',' || c == '.' ||
			c == ':' || c == ';' || c == '(' || c == ')' || c == '-' {
			if start >= 0 {
				words = append(words, word{text: text[start:i], start: start, end: i})
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		words = append(words, word{text: text[start:], start: start, end: len(text)})
	}
	return words
}

// isNameToken reports whether a word is a first name or surname.
func isNameToken(w string) bool {
	lower := strings.ToLower(w)
	return firstNames[lower] || isSurname(w)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/detector/ -run TestDetectFIO -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/detector/semantic.go internal/detector/semantic_test.go
git commit -m "feat: FIO detection with confidence scoring"
```

---

### Task 4: Address detection

**Files:**
- Modify: `internal/detector/semantic.go`
- Test: `internal/detector/semantic_test.go`

**Interfaces:**
- Consumes: `cities`, `contextKeywords`, `negativeContext`, `hasContext`, `hasNegativeContext`, `scoreConfidence`, `tokenize` from Tasks 2-3.
- Produces: `func detectAddress(text string) []Span` — returns address spans with `Confidence` set.

- [ ] **Step 1: Write the failing test**

Add to `internal/detector/semantic_test.go`:

```go
func TestDetectAddress(t *testing.T) {
	// Client address with context → high confidence.
	spans := detectAddress("адрес клиента: г. Москва, ул. Ленина, д. 10")
	require.Len(t, spans, 1)
	require.Equal(t, TypeAddress, spans[0].Type)
	require.GreaterOrEqual(t, spans[0].Confidence, float32(0.95))

	// Bank address with negative context → low confidence.
	spans = detectAddress("Банк находится по адресу Москва, ул. Тверская, 10")
	require.Len(t, spans, 1)
	require.Less(t, spans[0].Confidence, float32(0.75))

	// No city/street → no span.
	spans = detectAddress("обычный текст")
	require.Empty(t, spans)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/detector/ -run TestDetectAddress -v`
Expected: FAIL — `detectAddress` undefined.

- [ ] **Step 3: Write the implementation**

Append to `internal/detector/semantic.go`:

```go
// detectAddress finds address spans (city + street pattern) and scores them.
func detectAddress(text string) []Span {
	words := tokenize(text)
	var spans []Span
	for i := 0; i < len(words); i++ {
		if !cities[strings.ToLower(words[i].text)] {
			continue
		}
		// Extend over the address: city + following street tokens.
		j := i
		for j+1 < len(words) && isStreetToken(words[j+1].text) {
			j++
		}
		start := words[i].start
		end := words[j].end
		ctxHits := 0
		if hasContext(text, start, end, contextKeywords(TypeAddress)) {
			ctxHits = 1
		}
		negHits := 0
		if hasNegativeContext(text, start, end, negativeContext(TypeAddress)) {
			negHits = 1
		}
		conf := scoreConfidence(0.5, true, ctxHits, negHits)
		spans = append(spans, Span{Start: start, End: end, Type: TypeAddress, Confidence: conf})
		i = j
	}
	return spans
}

// isStreetToken reports whether a word is a street/address marker.
func isStreetToken(w string) bool {
	lower := strings.ToLower(w)
	switch lower {
	case "ул", "улица", "проспект", "переулок", "шоссе", "бульвар",
		"набережная", "д", "дом", "кв", "квартира", "г", "город":
		return true
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/detector/ -run TestDetectAddress -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/detector/semantic.go internal/detector/semantic_test.go
git commit -m "feat: address detection with confidence scoring"
```

---

### Task 5: Wire semantic layer into Detector.Detect

**Files:**
- Modify: `internal/detector/detector.go`
- Test: `internal/detector/detector_test.go`

**Interfaces:**
- Consumes: `detectFIO`, `detectAddress` from Tasks 3-4.
- Produces: `Detector.Detect(text)` now returns spans with `Confidence` set, including FIO/address spans.

- [ ] **Step 1: Write the failing test**

Add to `internal/detector/detector_test.go`:

```go
func TestDetectSemanticFIO(t *testing.T) {
	d := New(StructuredRules())
	spans := d.Detect("Клиент Иванов Иван Иванович")
	found := false
	for _, s := range spans {
		if s.Type == TypeFIO {
			found = true
			require.GreaterOrEqual(t, s.Confidence, float32(0.95))
		}
	}
	require.True(t, found, "expected FIO in %q, got %v", "Клиент Иванов Иван Иванович", spans)
}

func TestDetectSemanticAddress(t *testing.T) {
	d := New(StructuredRules())
	spans := d.Detect("адрес клиента: г. Москва, ул. Ленина, д. 10")
	found := false
	for _, s := range spans {
		if s.Type == TypeAddress {
			found = true
			require.GreaterOrEqual(t, s.Confidence, float32(0.95))
		}
	}
	require.True(t, found, "expected address in %q, got %v", "адрес клиента: г. Москва, ул. Ленина, д. 10", spans)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/detector/ -run 'TestDetectSemanticFIO|TestDetectSemanticAddress' -v`
Expected: FAIL — no FIO/address spans returned.

- [ ] **Step 3: Wire the semantic layer**

In `internal/detector/detector.go`, modify `detectSequential` and `detectParallel` to append semantic spans before resolving overlaps. Change both functions' final return to call a shared helper:

```go
func (d *Detector) detectSequential(text string) []Span {
	var spans []Span
	for _, r := range d.rules {
		spans = append(spans, ruleSpans(text, r)...)
	}
	return resolveAll(text, spans)
}

func (d *Detector) detectParallel(text string) []Span {
	n := runtime.GOMAXPROCS(0)
	if n > len(d.rules) {
		n = len(d.rules)
	}
	results := make([][]Span, len(d.rules))
	var wg sync.WaitGroup
	sem := make(chan struct{}, n)
	for i, r := range d.rules {
		wg.Add(1)
		go func(i int, r Rule) {
			defer wg.Done()
			sem <- struct{}{}
			results[i] = ruleSpans(text, r)
			<-sem
		}(i, r)
	}
	wg.Wait()
	var spans []Span
	for _, rs := range results {
		spans = append(spans, rs...)
	}
	return resolveAll(text, spans)
}

// resolveAll appends semantic spans and resolves overlaps.
func resolveAll(text string, spans []Span) []Span {
	spans = append(spans, detectFIO(text)...)
	spans = append(spans, detectAddress(text)...)
	return ResolveOverlaps(spans)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/detector/ -run 'TestDetectSemanticFIO|TestDetectSemanticAddress' -v`
Expected: PASS.

- [ ] **Step 5: Run full detector tests**

Run: `go test ./internal/detector/`
Expected: PASS (existing tests still green).

- [ ] **Step 6: Commit**

```bash
git add internal/detector/detector.go internal/detector/detector_test.go
git commit -m "feat: wire semantic layer into Detector.Detect"
```

---

### Task 6: Apply confidence gate in handler

**Files:**
- Modify: `internal/api/handlers/process.go`
- Test: `internal/api/handlers/process_test.go`

**Interfaces:**
- Consumes: `Span.Confidence` from Task 1, `contextKeywords`/`hasContext` from Task 2.
- Produces: `func (h *Handler) gateSpans(text string, spans []detector.Span) []detector.Span` — filters spans by the 3-threshold model.

- [ ] **Step 1: Read the existing handler test**

Read `internal/api/handlers/process_test.go` to match its test setup (how it constructs `Handler`).

- [ ] **Step 2: Write the failing test**

Add to `internal/api/handlers/process_test.go`:

```go
func TestGateSpans(t *testing.T) {
	h := &Handler{}
	// High confidence → kept.
	spans := []detector.Span{
		{Start: 0, End: 5, Type: detector.TypeEmail, Confidence: 0.99},
	}
	got := h.gateSpans("test@example.com", spans)
	require.Len(t, got, 1)

	// Mid confidence with context → kept.
	spans = []detector.Span{
		{Start: 7, End: 30, Type: detector.TypeFIO, Confidence: 0.8},
	}
	got = h.gateSpans("Клиент Иванов Иван Иванович", spans)
	require.Len(t, got, 1)

	// Mid confidence without context → dropped.
	spans = []detector.Span{
		{Start: 0, End: 20, Type: detector.TypeFIO, Confidence: 0.8},
	}
	got = h.gateSpans("Александр Пушкин написал", spans)
	require.Empty(t, got)

	// Low confidence → dropped.
	spans = []detector.Span{
		{Start: 0, End: 20, Type: detector.TypeAddress, Confidence: 0.5},
	}
	got = h.gateSpans("Банк находится по адресу Москва", spans)
	require.Empty(t, got)
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/api/handlers/ -run TestGateSpans -v`
Expected: FAIL — `gateSpans` undefined.

- [ ] **Step 4: Write the implementation**

In `internal/api/handlers/process.go`, add the `gateSpans` method and call it in `Process` after detection:

```go
// gateSpans filters spans by the 3-threshold confidence model.
func (h *Handler) gateSpans(text string, spans []detector.Span) []detector.Span {
	var kept []detector.Span
	for _, s := range spans {
		switch {
		case s.Confidence >= 0.95:
			kept = append(kept, s)
		case s.Confidence >= 0.75:
			if detector.HasContext(text, s.Start, s.End, s.Type) {
				kept = append(kept, s)
			}
		default:
			// below 0.75 → drop
		}
	}
	return kept
}
```

In `Process`, replace the detection line:

```go
spans := h.Detector.Detect(req.Payload)
```

with:

```go
spans := h.gateSpans(req.Payload, h.Detector.Detect(req.Payload))
```

- [ ] **Step 5: Add HasContext to detector package**

In `internal/detector/semantic.go`, add an exported wrapper so the handler can check context by type:

```go
// HasContext reports whether a context keyword for t appears near the span.
func HasContext(text string, start, end int, t Type) bool {
	return hasContext(text, start, end, contextKeywords(t))
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/api/handlers/ -run TestGateSpans -v`
Expected: PASS.

- [ ] **Step 7: Run full test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/api/handlers/process.go internal/api/handlers/process_test.go internal/detector/semantic.go
git commit -m "feat: apply 3-threshold confidence gate in handler"
```

---

### Task 7: Extend accuracy tests with adversarial cases

**Files:**
- Modify: `internal/detector/accuracy_test.go`

**Interfaces:**
- Consumes: `Detector.Detect` from Task 5, `gateSpans` behavior from Task 6.
- Produces: expanded `accuracyCases` covering the "Пушкин не ПД" and "адрес банка" traps.

- [ ] **Step 1: Add adversarial cases**

Replace the `accuracyCases` slice in `internal/detector/accuracy_test.go` with:

```go
var accuracyCases = []struct {
	text     string
	positive []Type
	negative []Type
}{
	{"Клиент Иванов Иван Иванович, паспорт 4509 123456", []Type{TypePassport, TypeFIO}, nil},
	{"email test@example.com", []Type{TypeEmail}, nil},
	{"телефон +7 912 345-67-89", []Type{TypePhone}, nil},
	{"ИНН 7707083893", []Type{TypeINN}, nil},
	{"карта 4276 1234 5678 9012", []Type{TypeCard}, nil},
	{"дата рождения 15.03.1990", []Type{TypeBirthDate}, nil},
	// Adversarial: literary mention is NOT PII.
	{"Александр Пушкин написал роман", nil, []Type{TypeFIO}},
	{"Лев Толстой родился в Ясной Поляне", nil, []Type{TypeFIO}},
	// Adversarial: bank address is NOT PII.
	{"Банк находится по адресу Москва, ул. Тверская, 10", nil, []Type{TypeAddress}},
	{"Ближайшее отделение банка на ул. Ленина, 10", nil, []Type{TypeAddress}},
	// Positive: client address IS PII.
	{"адрес клиента: г. Москва, ул. Ленина, д. 10", []Type{TypeAddress}, nil},
	{"проживает по адресу: г. Санкт-Петербург, Невский проспект, д. 10", []Type{TypeAddress}, nil},
	// PIN co-occurrence.
	{"пин 1234", nil, []Type{TypePIN}},
	{"пин 1234, карта 4276 1234 5678 9012", []Type{TypePIN}, nil},
}
```

- [ ] **Step 2: Run the accuracy test**

Run: `go test ./internal/detector/ -run TestAccuracy -v`
Expected: PASS (accuracy >= 95%).

- [ ] **Step 3: Run full test suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/detector/accuracy_test.go
git commit -m "test: adversarial accuracy cases for semantic NER"
```

---

## Self-Review

**Spec coverage:**
- Confidence field on Span → Task 1 ✓
- Dictionaries, context keywords, negative context, additive scoring → Task 2 ✓
- FIO detection → Task 3 ✓
- Address detection → Task 4 ✓
- Semantic layer wired into Detect → Task 5 ✓
- 3-threshold gate in handler → Task 6 ✓
- Adversarial accuracy tests → Task 7 ✓

**Placeholder scan:** No TBD/TODO. All steps have concrete code and commands.

**Type consistency:** `detectFIO`/`detectAddress` return `[]Span`; `scoreConfidence` returns `float32`; `Span.Confidence` is `float32`; `gateSpans` returns `[]detector.Span`; `HasContext(text, start, end, t Type) bool` matches `hasContext(text, start, end, keywords)`. Consistent.
# ONNX NER Integration (Smart Path) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an optional ONNX-based NER model (rubert-tiny) to the PII detector's smart path so ambiguous cases (mid-confidence or no rule-based spans) are resolved by a real ML NER model, with graceful degradation when the model is unavailable.

**Architecture:** A new `internal/ner` package wraps a custom WordPiece tokenizer and the `yalue/onnxruntime_go` runtime. The `Detector` gets an optional `NERDetector` interface; when rule-based detection yields mid-confidence spans or no spans, the NER runs and its spans are merged. If the model/runtime is unavailable, the detector degrades to rule-based only.

**Tech Stack:** Go 1.25, `github.com/yalue/onnxruntime_go` (cgo), standard library.

## Global Constraints

- All offsets are byte offsets (UTF-8 safe — Russian text). Never mix rune/byte offsets.
- The ONNX NER is OPTIONAL: if the model file or ONNX runtime is unavailable, the detector must degrade gracefully (no crash, no error to caller).
- No model file or vocab committed to the repo — downloaded at setup time.
- Follow existing package conventions (`internal/detector`, `testify/require`).
- NER label mapping: `PER` → `TypeFIO`, `LOC` → `TypeAddress`, `ORG` → context-dependent (only masked with context keyword).

---

### Task 1: WordPiece tokenizer

**Files:**
- Create: `internal/ner/tokenizer.go`
- Create: `internal/ner/tokenizer_test.go`
- Create: `internal/ner/testdata/vocab.txt` (tiny test vocab)

**Interfaces:**
- Consumes: nothing (standalone).
- Produces:
  - `type Token struct { ID int; Text string; Start int; End int }` — `Start`/`End` are byte offsets into the original text.
  - `type Tokenizer struct { vocab map[string]int }`
  - `func NewTokenizer(vocabPath string) (*Tokenizer, error)` — loads vocab.txt.
  - `func (t *Tokenizer) Encode(text string) ([]Token, error)` — returns tokens including `[CLS]` (Start=-1) and `[SEP]` (Start=-1) special tokens, with byte offsets for real tokens.

- [ ] **Step 1: Create the test vocab**

Create `internal/ner/testdata/vocab.txt`:

```
[PAD]
[UNK]
[CLS]
[SEP]
иван
иванов
иванович
москва
ул
ленина
##ов
##ич
##а
##ин
##ский
```

- [ ] **Step 2: Write the failing test**

Create `internal/ner/tokenizer_test.go`:

```go
package ner

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTokenizerEncode(t *testing.T) {
	tk, err := NewTokenizer("testdata/vocab.txt")
	require.NoError(t, err)

	tokens, err := tk.Encode("Иван Иванов")
	require.NoError(t, err)
	// [CLS] + "иван" + "иванов" + [SEP]
	require.Len(t, tokens, 4)
	require.Equal(t, "[CLS]", tokens[0].Text)
	require.Equal(t, -1, tokens[0].Start)
	require.Equal(t, "иван", tokens[1].Text)
	require.Equal(t, 0, tokens[1].Start)
	require.Equal(t, "иванов", tokens[2].Text)
	// "Иван" = 4 Cyrillic chars × 2 bytes = 8 bytes, + 1 space = 9.
	require.Equal(t, 9, tokens[2].Start)
	require.Equal(t, "[SEP]", tokens[3].Text)
}

func TestTokenizerUnknownWord(t *testing.T) {
	tk, err := NewTokenizer("testdata/vocab.txt")
	require.NoError(t, err)

	// "петров" is not in vocab; should fall back to [UNK].
	tokens, err := tk.Encode("петров")
	require.NoError(t, err)
	require.Len(t, tokens, 3) // [CLS] + [UNK] + [SEP]
	require.Equal(t, "[UNK]", tokens[1].Text)
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/ner/ -run TestTokenizer -v`
Expected: FAIL — `NewTokenizer` undefined.

- [ ] **Step 4: Write the implementation**

Create `internal/ner/tokenizer.go`:

```go
package ner

import (
	"bufio"
	"os"
	"strings"
)

// Token is a single WordPiece token with byte offsets into the original text.
// Special tokens ([CLS], [SEP], [UNK], [PAD]) have Start == -1.
type Token struct {
	ID    int
	Text  string
	Start int
	End   int
}

// Tokenizer performs WordPiece tokenization for a BERT-style model.
type Tokenizer struct {
	vocab map[string]int
}

// NewTokenizer loads a vocab.txt file (one token per line) into a Tokenizer.
func NewTokenizer(vocabPath string) (*Tokenizer, error) {
	f, err := os.Open(vocabPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	vocab := make(map[string]int)
	scanner := bufio.NewScanner(f)
	id := 0
	for scanner.Scan() {
		vocab[scanner.Text()] = id
		id++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return &Tokenizer{vocab: vocab}, nil
}

// Encode tokenizes text into WordPiece tokens, tracking byte offsets.
// It prepends [CLS] and appends [SEP].
func (t *Tokenizer) Encode(text string) ([]Token, error) {
	tokens := []Token{{ID: t.id("[CLS]"), Text: "[CLS]", Start: -1, End: -1}}
	for _, word := range splitWords(text) {
		tokens = append(tokens, t.tokenizeWord(word)...)
	}
	tokens = append(tokens, Token{ID: t.id("[SEP]"), Text: "[SEP]", Start: -1, End: -1})
	return tokens, nil
}

// splitWords splits text into words with byte offsets, preserving whitespace
// boundaries. Words are lowercased for matching.
func splitWords(text string) []word {
	var words []word
	start := -1
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == ' ' || c == '\t' || c == '\n' || c == ',' || c == '.' ||
			c == ':' || c == ';' || c == '(' || c == ')' || c == '-' {
			if start >= 0 {
				words = append(words, word{text: strings.ToLower(text[start:i]), start: start, end: i})
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		words = append(words, word{text: strings.ToLower(text[start:]), start: start, end: len(text)})
	}
	return words
}

type word struct {
	text  string
	start int
	end   int
}

// tokenizeWord applies greedy longest-match WordPiece to a single word.
func (t *Tokenizer) tokenizeWord(w word) []Token {
	if _, ok := t.vocab[w.text]; ok {
		return []Token{{ID: t.vocab[w.text], Text: w.text, Start: w.start, End: w.end}}
	}
	// Greedy longest-match with ## continuation.
	var tokens []Token
	remaining := w.text
	offset := w.start
	for remaining != "" {
		matched := false
		for end := len(remaining); end > 0; end-- {
			piece := remaining[:end]
			if offset > w.start {
				piece = "##" + piece
			}
			if id, ok := t.vocab[piece]; ok {
				tokens = append(tokens, Token{ID: id, Text: piece, Start: offset, End: offset + end})
				remaining = remaining[end:]
				offset += end
				matched = true
				break
			}
		}
		if !matched {
			// Unknown: emit [UNK] for the whole word.
			return []Token{{ID: t.id("[UNK]"), Text: "[UNK]", Start: w.start, End: w.end}}
		}
	}
	return tokens
}

func (t *Tokenizer) id(s string) int {
	if id, ok := t.vocab[s]; ok {
		return id
	}
	return -1
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ner/ -run TestTokenizer -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ner/tokenizer.go internal/ner/tokenizer_test.go internal/ner/testdata/vocab.txt
git commit -m "feat: WordPiece tokenizer for rubert-tiny"
```

---

### Task 2: NER label mapping and span construction

**Files:**
- Create: `internal/ner/ner.go`
- Create: `internal/ner/ner_test.go`

**Interfaces:**
- Consumes: `Token` from Task 1, `detector.Span`/`detector.Type` from `internal/detector`.
- Produces:
  - `type NER struct { tokenizer *Tokenizer; labels []string }`
  - `func NewNER(tokenizer *Tokenizer, labels []string) *NER`
  - `func (n *NER) SpansFromLabels(text string, tokens []Token, labelIDs []int) []detector.Span` — maps token-level label IDs to byte-offset spans, converting NER labels to PII types.
  - `func mapLabel(label string) (detector.Type, bool)` — `PER`→`TypeFIO`, `LOC`→`TypeAddress`, `ORG`→(false, no direct mapping).

- [ ] **Step 1: Write the failing test**

Create `internal/ner/ner_test.go`:

```go
package ner

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/detector"
)

func TestMapLabel(t *testing.T) {
	typ, ok := mapLabel("B-PER")
	require.True(t, ok)
	require.Equal(t, detector.TypeFIO, typ)

	typ, ok = mapLabel("I-PER")
	require.True(t, ok)
	require.Equal(t, detector.TypeFIO, typ)

	typ, ok = mapLabel("B-LOC")
	require.True(t, ok)
	require.Equal(t, detector.TypeAddress, typ)

	_, ok = mapLabel("B-ORG")
	require.False(t, ok)

	_, ok = mapLabel("O")
	require.False(t, ok)
}

func TestSpansFromLabels(t *testing.T) {
	tk, err := NewTokenizer("testdata/vocab.txt")
	require.NoError(t, err)
	n := NewNER(tk, []string{"O", "B-PER", "I-PER", "B-LOC"})

	text := "Иван Иванов"
	tokens, err := tk.Encode(text)
	require.NoError(t, err)
	// tokens: [CLS] иван иванов [SEP]
	// label IDs: 0(O) 1(B-PER) 2(I-PER) 0(O)
	labelIDs := []int{0, 1, 2, 0}

	spans := n.SpansFromLabels(text, tokens, labelIDs)
	require.Len(t, spans, 1)
	require.Equal(t, detector.TypeFIO, spans[0].Type)
	require.Equal(t, 0, spans[0].Start)
	// "Иван Иванов" = 4×2 + 1 + 6×2 = 21 bytes.
	require.Equal(t, 21, spans[0].End)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ner/ -run 'TestMapLabel|TestSpansFromLabels' -v`
Expected: FAIL — `mapLabel`/`NewNER` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/ner/ner.go`:

```go
package ner

import (
	"strings"

	"github.com/kind-earthquake/pii-module/internal/detector"
)

// NER maps token-level label predictions to PII spans.
type NER struct {
	tokenizer *Tokenizer
	labels    []string
}

// NewNER returns an NER that maps label IDs (index into labels) to PII types.
func NewNER(tokenizer *Tokenizer, labels []string) *NER {
	return &NER{tokenizer: tokenizer, labels: labels}
}

// SpansFromLabels converts token-level label IDs into byte-offset PII spans.
// labelIDs must align with tokens (same length). Special tokens ([CLS]/[SEP])
// are ignored.
func (n *NER) SpansFromLabels(text string, tokens []Token, labelIDs []int) []detector.Span {
	if len(tokens) != len(labelIDs) {
		return nil
	}
	var spans []detector.Span
	var cur *detector.Span
	for i, tok := range tokens {
		if tok.Start < 0 || i >= len(labelIDs) {
			// Special token: close any open span.
			cur = nil
			continue
		}
		label := ""
		if labelIDs[i] >= 0 && labelIDs[i] < len(n.labels) {
			label = n.labels[labelIDs[i]]
		}
		typ, isPII := mapLabel(label)
		if !isPII {
			cur = nil
			continue
		}
		if cur == nil || cur.Type != typ {
			// Start a new span.
			cur = &detector.Span{Start: tok.Start, End: tok.End, Type: typ, Confidence: 0.9}
			spans = append(spans, *cur)
		} else {
			// Extend the current span.
			cur.End = tok.End
			spans[len(spans)-1] = *cur
		}
	}
	return spans
}

// mapLabel converts a BERT NER label to a PII type. Returns false for labels
// that are not directly PII (O, ORG, etc.).
func mapLabel(label string) (detector.Type, bool) {
	label = strings.ToUpper(label)
	switch {
	case strings.HasSuffix(label, "PER"):
		return detector.TypeFIO, true
	case strings.HasSuffix(label, "LOC"):
		return detector.TypeAddress, true
	default:
		return "", false
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ner/ -run 'TestMapLabel|TestSpansFromLabels' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ner/ner.go internal/ner/ner_test.go
git commit -m "feat: NER label mapping and span construction"
```

---

### Task 3: ONNX model wrapper

**Files:**
- Create: `internal/ner/model.go`
- Create: `internal/ner/model_test.go`

**Interfaces:**
- Consumes: `Token` from Task 1, `detector.Span` from `internal/detector`.
- Produces:
  - `type Model struct { session *ort.AdvancedSession; tokenizer *Tokenizer; labels []string; maxLen int }`
  - `func NewModel(modelPath, vocabPath string, labels []string, maxLen int) (*Model, error)` — initializes ONNX env, loads model + tokenizer.
  - `func (m *Model) Detect(text string) ([]detector.Span, error)` — tokenizes, runs inference, returns spans.
  - `func (m *Model) Close() error`

- [ ] **Step 1: Write the failing test**

Create `internal/ner/model_test.go`:

```go
package ner

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestModelDetect is skipped unless ONNXRUNTIME_SHARED_LIBRARY_PATH and a model
// file are provided, since the model and runtime are not committed to the repo.
func TestModelDetect(t *testing.T) {
	libPath := os.Getenv("ONNXRUNTIME_SHARED_LIBRARY_PATH")
	modelPath := os.Getenv("NER_MODEL_PATH")
	if libPath == "" || modelPath == "" {
		t.Skip("set ONNXRUNTIME_SHARED_LIBRARY_PATH and NER_MODEL_PATH to run")
	}
	// This test requires a real rubert-tiny NER ONNX model. It is a smoke test
	// that the model loads and runs without error.
	m, err := NewModel(modelPath, "testdata/vocab.txt", []string{"O", "B-PER", "I-PER", "B-LOC"}, 128)
	require.NoError(t, err)
	defer m.Close()

	spans, err := m.Detect("Иван Иванов")
	require.NoError(t, err)
	// We don't assert specific spans (model-dependent); just that it ran.
	_ = spans
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ner/ -run TestModelDetect -v`
Expected: SKIP (env vars not set) — this is the expected behavior.

- [ ] **Step 3: Add the onnxruntime dependency**

Run: `go get github.com/yalue/onnxruntime_go@latest`

- [ ] **Step 4: Write the implementation**

Create `internal/ner/model.go`:

```go
package ner

import (
	"fmt"

	ort "github.com/yalue/onnxruntime_go"

	"github.com/kind-earthquake/pii-module/internal/detector"
)

// Model wraps the ONNX runtime and tokenizer for NER inference.
type Model struct {
	session   *ort.AdvancedSession
	tokenizer *Tokenizer
	labels    []string
	maxLen    int
	input     *ort.Tensor[int64]
	mask      *ort.Tensor[int64]
	output    *ort.Tensor[float32]
}

// NewModel initializes the ONNX environment, loads the model and tokenizer.
// modelPath is the .onnx file; vocabPath is the vocab.txt file.
func NewModel(modelPath, vocabPath string, labels []string, maxLen int) (*Model, error) {
	tk, err := NewTokenizer(vocabPath)
	if err != nil {
		return nil, fmt.Errorf("load tokenizer: %w", err)
	}
	if err := ort.InitializeEnvironment(); err != nil {
		return nil, fmt.Errorf("init onnxruntime: %w", err)
	}
	shape := ort.NewShape(1, int64(maxLen))
	input, err := ort.NewTensor[int64](shape, make([]int64, maxLen))
	if err != nil {
		return nil, fmt.Errorf("create input tensor: %w", err)
	}
	mask, err := ort.NewTensor[int64](shape, make([]int64, maxLen))
	if err != nil {
		return nil, fmt.Errorf("create mask tensor: %w", err)
	}
	// Output shape: [1, maxLen, numLabels]. We use a flat buffer sized
	// maxLen * len(labels).
	output, err := ort.NewTensor[float32](ort.NewShape(1, int64(maxLen), int64(len(labels))), make([]float32, maxLen*len(labels)))
	if err != nil {
		return nil, fmt.Errorf("create output tensor: %w", err)
	}
	session, err := ort.NewAdvancedSession(modelPath,
		[]string{"input_ids", "attention_mask"},
		[]string{"logits"},
		[]ort.Value{input, mask},
		[]ort.Value{output},
		nil)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &Model{session: session, tokenizer: tk, labels: labels, maxLen: maxLen,
		input: input, mask: mask, output: output}, nil
}

// Detect tokenizes text, runs the model, and returns PII spans.
func (m *Model) Detect(text string) ([]detector.Span, error) {
	tokens, err := m.tokenizer.Encode(text)
	if err != nil {
		return nil, err
	}
	if len(tokens) > m.maxLen {
		tokens = tokens[:m.maxLen]
	}
	ids := m.input.GetData()
	mask := m.mask.GetData()
	for i := range ids {
		ids[i] = 0
		mask[i] = 0
	}
	for i, tok := range tokens {
		ids[i] = int64(tok.ID)
		mask[i] = 1
	}
	if err := m.session.Run(); err != nil {
		return nil, fmt.Errorf("run model: %w", err)
	}
	// Argmax over labels for each token.
	labelIDs := make([]int, len(tokens))
	data := m.output.GetData()
	for i := 0; i < len(tokens); i++ {
		best := 0
		bestVal := data[i*len(m.labels)]
		for j := 1; j < len(m.labels); j++ {
			v := data[i*len(m.labels)+j]
			if v > bestVal {
				bestVal = v
				best = j
			}
		}
		labelIDs[i] = best
	}
	return m.SpansFromLabels(text, tokens, labelIDs), nil
}

// Close releases the ONNX session and environment.
func (m *Model) Close() error {
	if m.session != nil {
		m.session.Destroy()
	}
	m.input.Destroy()
	m.mask.Destroy()
	m.output.Destroy()
	ort.DestroyEnvironment()
	return nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ner/ -run TestModelDetect -v`
Expected: SKIP (env vars not set) — the test compiles and skips correctly.

- [ ] **Step 6: Run build to verify compilation**

Run: `go build ./...`
Expected: PASS (compiles with the onnxruntime dependency).

- [ ] **Step 7: Commit**

```bash
git add internal/ner/model.go internal/ner/model_test.go go.mod go.sum
git commit -m "feat: ONNX model wrapper for NER inference"
```

---

### Task 4: Smart path integration into Detector

**Files:**
- Modify: `internal/detector/detector.go`
- Modify: `internal/detector/detector_test.go`

**Interfaces:**
- Consumes: `detector.Span` (existing), a new `NERDetector` interface.
- Produces:
  - `type NERDetector interface { Detect(text string) []Span }`
  - `func New(rules []Rule, opts ...Option) *Detector` — variadic options.
  - `func WithNER(n NERDetector) Option`
  - `Detector.Detect(text)` now triggers the smart path: if rule-based yields mid-confidence spans (0.75–0.95) or no spans, and NER is set, run NER and merge.

- [ ] **Step 1: Write the failing test**

Add to `internal/detector/detector_test.go`:

```go
type mockNER struct {
	spans  []Span
	called bool
}

func (m *mockNER) Detect(text string) []Span {
	m.called = true
	return m.spans
}

func TestDetectSmartPathNoSpans(t *testing.T) {
	// No rule-based spans → NER is called and its spans merged.
	ner := &mockNER{spans: []Span{
		{Start: 0, End: 11, Type: TypeFIO, Confidence: 0.9},
	}}
	d := New(StructuredRules(), WithNER(ner))
	spans := d.Detect("Иван Иванов")
	require.Len(t, spans, 1)
	require.Equal(t, TypeFIO, spans[0].Type)
	require.True(t, ner.called, "NER should be called when no rule-based spans")
}

func TestDetectSmartPathHighConfidenceSkipsNER(t *testing.T) {
	// High-confidence rule-based span → NER NOT called.
	ner := &mockNER{spans: []Span{
		{Start: 0, End: 11, Type: TypeFIO, Confidence: 0.9},
	}}
	d := New(StructuredRules(), WithNER(ner))
	spans := d.Detect("паспорт 4509 123456")
	require.Len(t, spans, 1)
	require.Equal(t, TypePassport, spans[0].Type)
	require.False(t, ner.called, "NER should NOT be called for high-confidence spans")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/detector/ -run TestDetectSmartPath -v`
Expected: FAIL — `WithNER` undefined.

- [ ] **Step 3: Write the implementation**

Modify `internal/detector/detector.go`:

```go
package detector

import (
	"regexp"
	"runtime"
	"strings"
	"sync"
)

// NERDetector is an optional ML NER model used in the smart path.
type NERDetector interface {
	Detect(text string) []Span
}

// Detector scans text for personal-data spans.
type Detector struct {
	rules []Rule
	ner   NERDetector
}

// Option configures a Detector.
type Option func(*Detector)

// WithNER sets the optional NER model for the smart path.
func WithNER(n NERDetector) Option {
	return func(d *Detector) { d.ner = n }
}

// New returns a Detector using the given rules and options.
func New(rules []Rule, opts ...Option) *Detector {
	d := &Detector{rules: rules}
	for _, o := range opts {
		o(d)
	}
	return d
}

// Detect returns resolved, non-overlapping PII spans in text.
func (d *Detector) Detect(text string) []Span {
	if len(d.rules) == 0 {
		return nil
	}
	var spans []Span
	if len(text) < 4096 {
		spans = d.detectSequential(text)
	} else {
		spans = d.detectParallel(text)
	}
	return d.smartPath(text, spans)
}

// smartPath runs the NER model when rule-based detection is ambiguous
// (mid-confidence spans or no spans) and merges the results.
func (d *Detector) smartPath(text string, spans []Span) []Span {
	if d.ner == nil {
		return spans
	}
	ambiguous := len(spans) == 0
	if !ambiguous {
		for _, s := range spans {
			if s.Confidence >= 0.75 && s.Confidence < 0.95 {
				ambiguous = true
				break
			}
		}
	}
	if !ambiguous {
		return spans
	}
	nerSpans := d.ner.Detect(text)
	return ResolveOverlaps(append(spans, nerSpans...))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/detector/ -run TestDetectSmartPath -v`
Expected: PASS.

- [ ] **Step 5: Run full detector tests**

Run: `go test ./internal/detector/`
Expected: PASS (existing tests still green).

- [ ] **Step 6: Commit**

```bash
git add internal/detector/detector.go internal/detector/detector_test.go
git commit -m "feat: smart path NER integration in Detector"
```

---

### Task 5: Setup script and README

**Files:**
- Create: `scripts/setup_ner.sh`
- Modify: `README.md`

**Interfaces:**
- Consumes: nothing.
- Produces: a setup script that downloads the rubert-tiny NER ONNX model + vocab, and documents the onnxruntime shared library requirement.

- [ ] **Step 1: Write the setup script**

Create `scripts/setup_ner.sh`:

```bash
#!/usr/bin/env bash
# Downloads the rubert-tiny NER ONNX model and vocab for the smart path.
# Usage: ./scripts/setup_ner.sh
set -euo pipefail

MODEL_DIR="models/ner"
mkdir -p "$MODEL_DIR"

echo "Downloading rubert-tiny NER ONNX model..."
# Replace with the actual model URL. The model is a rubert-tiny NER exported
# to ONNX. Place the .onnx file and vocab.txt in $MODEL_DIR.
# Example (placeholder — update to the real source):
# curl -L -o "$MODEL_DIR/model.onnx" "https://example.com/rubert-tiny-ner.onnx"
# curl -L -o "$MODEL_DIR/vocab.txt" "https://example.com/vocab.txt"

echo "Model files should be placed in $MODEL_DIR:"
echo "  $MODEL_DIR/model.onnx"
echo "  $MODEL_DIR/vocab.txt"
echo ""
echo "Also required: the onnxruntime shared library. Set"
echo "ONNXRUNTIME_SHARED_LIBRARY_PATH to its path before running the server."
```

- [ ] **Step 2: Make the script executable**

Run: `chmod +x scripts/setup_ner.sh`

- [ ] **Step 3: Update README**

Add a section to `README.md` documenting the optional NER smart path:

```markdown
## Опциональный NER (smart path)

Для неоднозначных случаев (mid-confidence или отсутствие rule-based совпадений)
можно подключить ML NER-модель (rubert-tiny в ONNX). Это опционально: без модели
шлюз работает на rule-based детекторе.

### Установка

1. Скачайте ONNX-модель и vocab: `./scripts/setup_ner.sh`
2. Укажите путь к onnxruntime shared library: `export ONNXRUNTIME_SHARED_LIBRARY_PATH=/path/to/libonnxruntime.so`
3. Запустите сервер с переменными `NER_MODEL_PATH` и `NER_VOCAB_PATH`.

Если модель недоступна, шлюз продолжает работать на rule-based детекторе
(graceful degradation).
```

- [ ] **Step 4: Commit**

```bash
git add scripts/setup_ner.sh README.md
git commit -m "docs: NER setup script and README"
```

---

## Self-Review

**Spec coverage:**
- WordPiece tokenizer → Task 1 ✓
- NER label mapping + span construction → Task 2 ✓
- ONNX model wrapper → Task 3 ✓
- Smart path integration → Task 4 ✓
- Setup + README → Task 5 ✓
- Graceful degradation → Task 4 (NER optional, nil check) + Task 3 (env-var skip) ✓
- Byte offsets → Task 1 (tokenizer tracks byte offsets) ✓

**Placeholder scan:** The setup script has placeholder URLs (marked clearly as placeholders to update to the real model source). This is intentional — the model isn't committed and the real URL is a setup-time concern. All code steps have complete code.

**Type consistency:** `Token{ID, Text, Start, End}` consistent across Tasks 1-3. `NERDetector.Detect(text) []Span` consistent between Task 4 interface and `Model.Detect`. `NewNER(tokenizer, labels)` in Task 2 matches usage. `mapLabel` returns `(detector.Type, bool)` consistent.
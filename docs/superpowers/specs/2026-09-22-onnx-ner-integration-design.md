# ONNX NER Integration (Smart Path) — Design

Date: 2026-09-22
Status: Approved

## Goal

Add an optional ONNX-based NER model (rubert-tiny) to the PII detector's "smart
path" so that ambiguous cases — where the rule-based semantic layer gives
mid-confidence (0.75–0.95) or finds nothing — can be resolved by a real ML NER
model. The fast path (regex + rule-based semantic layer) remains untouched and
fast; the ONNX NER runs only when needed.

## Background

The project is a Go PII-masking gateway. It has a rule-based detector
(`internal/detector`) with regex rules and a semantic layer (dictionaries +
context keywords + confidence scoring) that gates masking by a 3-threshold
model. The user chose to integrate a real ML NER model via ONNX + Go runtime
(`yalue/onnxruntime_go`) for the smart path, using a compact rubert-tiny NER
model, with a custom WordPiece tokenizer implemented in Go.

## Architecture

```
text
  │
  ▼
Rule-based Detector (existing) ──→ spans with Confidence
  │
  ├── high confidence (≥0.95) → keep
  ├── mid confidence (0.75-0.95) → ambiguous → ONNX NER
  └── no spans → ONNX NER (optional)
  │
  ▼
ONNX NER (new, smart path) ──→ adds spans
  │
  ▼
Merge + ResolveOverlaps
  │
  ▼
Masker
```

The ONNX NER is a separate package `internal/ner`. It is optional: if the model
file or ONNX runtime is unavailable, the detector degrades gracefully to
rule-based only (no crash, no error to the caller).

## Components

### 1. `internal/ner/tokenizer.go` — WordPiece tokenizer

- Loads `vocab.txt` (rubert-tiny vocabulary) into a `map[string]int`.
- Implements greedy longest-match WordPiece tokenization.
- Tracks byte offsets for each token so token-level NER predictions can be
  mapped back to byte offsets in the original UTF-8 Russian text.
- Handles `[CLS]` and `[SEP]` special tokens.

### 2. `internal/ner/model.go` — ONNX runtime wrapper

- Wraps `github.com/yalue/onnxruntime_go`.
- Loads the ONNX model file.
- Runs inference: input = token IDs + attention mask, output = token-level
  NER label logits/probabilities.
- Uses `AdvancedSession` for re-usable input/output tensors.

### 3. `internal/ner/ner.go` — high-level API

- `func New(modelPath, vocabPath string) (*NER, error)` — loads tokenizer + model.
- `func (n *NER) Detect(text string) []detector.Span` — tokenizes, runs model,
  maps token labels to byte-offset spans, converts NER labels to PII types.
- NER label mapping: `PER` → `TypeFIO`, `LOC` → `TypeAddress`, `ORG` →
  context-dependent (only masked with context keyword).

### 4. Integration — smart path trigger

- In the detector, when rule-based detection yields mid-confidence spans
  (0.75–0.95) or no spans, run the ONNX NER and merge its spans.
- The NER is injected as an optional dependency (interface), so the detector
  works without it.

## Key Design Decisions

- **Model**: rubert-tiny NER in ONNX format (~50MB), downloaded at setup time,
  not committed to the repo.
- **Tokenizer**: custom WordPiece in Go with byte-offset tracking for UTF-8
  Russian text.
- **Label mapping**: `PER` → FIO, `LOC` → ADDRESS, `ORG` → context-dependent.
- **Smart path trigger**: only for ambiguous cases (mid-confidence or no
  spans), keeping the fast path fast.
- **Graceful degradation**: if ONNX model/runtime unavailable, fall back to
  rule-based only.

## Error Handling

- Model file missing → log warning, disable NER, continue with rule-based.
- ONNX runtime init failure → same graceful degradation.
- Tokenizer errors → skip NER for that text.

## Testing

- Tokenizer unit tests (Russian text, byte offsets, special tokens).
- NER integration test with a small test model or mock.
- Smart path trigger test (mid-confidence → NER called; high-confidence → not).

## Out of Scope

- Training/fine-tuning the model.
- GPU acceleration (CPU only for hackathon).
- Multiple models.
- Model download automation (manual setup step documented in README).
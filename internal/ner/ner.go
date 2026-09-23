package ner

import (
	"strings"
	"sync/atomic"

	"github.com/kind-earthquake/pii-module/internal/detector"
)

// typeByLabelPtr is the default NER label-suffix → PII type mapping.
// SetTypeByLabel replaces it (config-driven); call it before serving requests.
// Stored behind an atomic.Pointer because SpansFromLabels runs on parallel
// goroutines in the detector smart path (detectParallel).
var typeByLabelPtr atomic.Pointer[map[string]detector.Type]

var defaultTypeByLabel = map[string]detector.Type{
	"PER": detector.TypeFIO,
	"LOC": detector.TypeAddress,
}

func init() {
	typeByLabelPtr.Store(&defaultTypeByLabel)
}

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

// mapLabel converts a BERT NER label to the default PII type. Returns false for
// labels that are not directly PII (O, ORG, etc. by default).
func mapLabel(label string) (detector.Type, bool) {
	return mapLabelWith(label, *typeByLabelPtr.Load())
}

// mapLabelWith converts label against an explicit suffix map. A nil/empty map
// returns false.
func mapLabelWith(label string, m map[string]detector.Type) (detector.Type, bool) {
	label = strings.ToUpper(label)
	for suffix, typ := range m {
		if strings.HasSuffix(label, suffix) {
			return typ, true
		}
	}
	return "", false
}

// SetTypeByLabel atomically replaces the default label→PII mapping.
// Call once at startup before serving requests.
func SetTypeByLabel(m map[string]detector.Type) {
	typeByLabelPtr.Store(&m)
}
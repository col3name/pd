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
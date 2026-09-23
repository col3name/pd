package masker

import (
	"fmt"
	"strings"

	"github.com/kind-earthquake/pii-module/internal/detector"
)

// Label returns the token label for a PII type.
func Label(t detector.Type) string {
	switch t {
	case detector.TypeFIO:
		return "PERSON"
	case detector.TypeBirthDate:
		return "DATE"
	case detector.TypePhone:
		return "PHONE"
	case detector.TypeEmail:
		return "EMAIL"
	case detector.TypeCard:
		return "CARD"
	case detector.TypePassport:
		return "PASSPORT"
	case detector.TypeINN:
		return "INN"
	case detector.TypeCVV:
		return "CVV"
	case detector.TypePIN:
		return "PIN"
	case detector.TypeAddress:
		return "ADDRESS"
	case detector.TypeCardholder:
		return "CARDHOLDER"
	default:
		return string(t)
	}
}

// Tokenize replaces spans with [LABEL_NNN] tokens and returns the token → value
// map. Spans must be sorted by Start. Numbering is a global counter over the
// sorted span order, so it is deterministic.
func Tokenize(text string, spans []detector.Span) (string, map[string]string) {
	tokens := make(map[string]string, len(spans))
	var b strings.Builder
	prev := 0
	for i, s := range spans {
		b.WriteString(text[prev:s.Start])
		tok := fmt.Sprintf("[%s_%03d]", Label(s.Type), i+1)
		b.WriteString(tok)
		tokens[tok] = text[s.Start:s.End]
		prev = s.End
	}
	b.WriteString(text[prev:])
	return b.String(), tokens
}

// Detokenize replaces every known token back to its original value.
func Detokenize(text string, tokens map[string]string) string {
	for tok, val := range tokens {
		text = strings.ReplaceAll(text, tok, val)
	}
	return text
}
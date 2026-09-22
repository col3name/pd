package masker

import (
	"strings"

	"github.com/kind-earthquake/pii-module/internal/detector"
)

// Mask replaces each span in text with its type placeholder. Spans must be
// sorted by Start ascending. Replacement is done left-to-right by building a
// new string, so byte offsets remain valid.
func Mask(text string, spans []detector.Span) string {
	if len(spans) == 0 {
		return text
	}
	var b strings.Builder
	prev := 0
	for _, s := range spans {
		b.WriteString(text[prev:s.Start])
		b.WriteString(s.Type.Placeholder())
		prev = s.End
	}
	b.WriteString(text[prev:])
	return b.String()
}
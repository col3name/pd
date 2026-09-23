package whitelist

import (
	"strings"

	"github.com/kind-earthquake/pii-module/internal/detector"
)

// Whitelist holds known non-PII entries that must never be masked.
type Whitelist struct {
	entries []string
}

// New returns a Whitelist combining all entry lists.
func New(persons, addresses, organizations []string) *Whitelist {
	entries := make([]string, 0, len(persons)+len(addresses)+len(organizations))
	for _, e := range persons {
		entries = append(entries, normalize(e))
	}
	for _, e := range addresses {
		entries = append(entries, normalize(e))
	}
	for _, e := range organizations {
		entries = append(entries, normalize(e))
	}
	return &Whitelist{entries: entries}
}

// Blocks reports whether the whitelist covers the span: an entry is contained
// in the span text or the span text is contained in an entry. A covered span
// is not PII regardless of detector confidence.
func (w *Whitelist) Blocks(text string, s detector.Span) bool {
	if len(w.entries) == 0 {
		return false
	}
	frag := normalize(text[s.Start:s.End])
	if frag == "" {
		return false
	}
	for _, e := range w.entries {
		if e == "" {
			continue
		}
		if strings.Contains(frag, e) || strings.Contains(e, frag) {
			return true
		}
	}
	return false
}

// normalize lowercases and collapses whitespace runs.
func normalize(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}
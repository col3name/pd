package detector

import "regexp"

// Detector scans text for personal-data spans.
type Detector struct {
	rules []Rule
}

// New returns a Detector using the given rules.
func New(rules []Rule) *Detector {
	return &Detector{rules: rules}
}

// Detect returns resolved, non-overlapping PII spans in text.
func (d *Detector) Detect(text string) []Span {
	var spans []Span
	for _, r := range d.rules {
		for _, loc := range r.Re.FindAllStringIndex(text, -1) {
			if r.ContextRe != nil && !contextMatches(text, loc[0], r.ContextRe) {
				continue
			}
			spans = append(spans, Span{Start: loc[0], End: loc[1], Type: r.Type, Priority: r.Priority})
		}
	}
	return ResolveOverlaps(spans)
}

// contextMatches reports whether the context regex matches in the text
// preceding the span start (within a bounded window).
func contextMatches(text string, start int, re *regexp.Regexp) bool {
	// Look back up to 60 bytes before the span for the context keyword.
	from := start - 60
	if from < 0 {
		from = 0
	}
	return re.MatchString(text[from:start])
}
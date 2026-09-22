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
		if r.CaptureRe != nil {
			spans = append(spans, captureSpans(text, r)...)
			continue
		}
		for _, loc := range r.Re.FindAllStringIndex(text, -1) {
			if r.ContextRe != nil && !contextMatches(text, loc[0], r.ContextRe) {
				continue
			}
			spans = append(spans, Span{Start: loc[0], End: loc[1], Type: r.Type, Priority: r.Priority})
		}
	}
	return ResolveOverlaps(spans)
}

// captureSpans extracts spans from a CaptureRe rule's group 1 matches.
func captureSpans(text string, r Rule) []Span {
	var spans []Span
	for _, m := range r.CaptureRe.FindAllStringSubmatchIndex(text, -1) {
		// m[2], m[3] are the bounds of group 1 (the captured value).
		if len(m) >= 4 && m[2] >= 0 {
			spans = append(spans, Span{Start: m[2], End: m[3], Type: r.Type, Priority: r.Priority})
		}
	}
	return spans
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
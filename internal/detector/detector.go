package detector

import (
	"regexp"
	"runtime"
	"strings"
	"sync"
)

// Detector scans text for personal-data spans.
type Detector struct {
	rules []Rule
}

// New returns a Detector using the given rules.
func New(rules []Rule) *Detector {
	return &Detector{rules: rules}
}

// Detect returns resolved, non-overlapping PII spans in text.
// Rules are independent, so detection is parallelized across available CPUs.
func (d *Detector) Detect(text string) []Span {
	if len(d.rules) == 0 {
		return nil
	}
	// For short inputs, sequential is faster (no goroutine overhead).
	if len(text) < 4096 {
		return d.detectSequential(text)
	}
	return d.detectParallel(text)
}

func (d *Detector) detectSequential(text string) []Span {
	var spans []Span
	for _, r := range d.rules {
		spans = append(spans, ruleSpans(text, r)...)
	}
	return ResolveOverlaps(spans)
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
	return ResolveOverlaps(spans)
}

// ruleSpans returns the spans produced by a single rule.
func ruleSpans(text string, r Rule) []Span {
	if r.CaptureRe != nil {
		// Cheap pre-check: if the keyword is absent, skip the expensive regex.
		if r.Keyword != "" && !strings.Contains(strings.ToLower(text), r.Keyword) {
			return nil
		}
		return captureSpans(text, r)
	}
	var spans []Span
	for _, loc := range r.Re.FindAllStringIndex(text, -1) {
		if r.ContextRe != nil && !contextMatches(text, loc[0], r.ContextRe) {
			continue
		}
		spans = append(spans, Span{Start: loc[0], End: loc[1], Type: r.Type, Priority: r.Priority})
	}
	return spans
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
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

// WithExtraRules appends user-defined rules to the built-in core rules.
func WithExtraRules(rules []Rule) Option {
	return func(d *Detector) { d.rules = append(d.rules, rules...) }
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
// Rules are independent, so detection is parallelized across available CPUs.
func (d *Detector) Detect(text string) []Span {
	if len(d.rules) == 0 {
		return nil
	}
	var spans []Span
	// For short inputs, sequential is faster (no goroutine overhead).
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
	return resolveAll(text, append(spans, nerSpans...))
}

func (d *Detector) detectSequential(text string) []Span {
	var spans []Span
	for _, r := range d.rules {
		spans = append(spans, ruleSpans(text, r)...)
	}
	return resolveAll(text, spans)
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
	return resolveAll(text, spans)
}

// resolveAll appends semantic spans and resolves overlaps.
func resolveAll(text string, spans []Span) []Span {
	// Detect address spans first so street names inside addresses are not
	// misclassified as FIO.
	addresses := detectAddress(text)
	// Filter any FIO spans (rule-based or NER) that overlap an address.
	var fio, rest []Span
	for _, s := range spans {
		if s.Type == TypeFIO {
			fio = append(fio, s)
		} else {
			rest = append(rest, s)
		}
	}
	rest = append(rest, addresses...)
	rest = append(rest, filterFIOOverlappingAddress(text, append(fio, detectFIO(text)...), addresses)...)
	return ResolveOverlaps(rest)
}

// filterFIOOverlappingAddress drops FIO spans that overlap an address span.
func filterFIOOverlappingAddress(text string, fio, addresses []Span) []Span {
	if len(addresses) == 0 {
		return fio
	}
	var kept []Span
	for _, f := range fio {
		overlap := false
		for _, a := range addresses {
			if f.Start < a.End && a.Start < f.End {
				overlap = true
				break
			}
		}
		if !overlap {
			kept = append(kept, f)
		}
	}
	return kept
}

// ruleSpans returns the spans produced by a single rule.
func ruleSpans(text string, r Rule) []Span {
	conf := r.Confidence
	if conf == 0 {
		conf = 1.0
	}
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
		spans = append(spans, Span{Start: loc[0], End: loc[1], Type: r.Type, Priority: r.Priority, Confidence: conf})
	}
	return spans
}

// captureSpans extracts spans from a CaptureRe rule's group 1 matches.
func captureSpans(text string, r Rule) []Span {
	conf := r.Confidence
	if conf == 0 {
		conf = 1.0
	}
	var spans []Span
	for _, m := range r.CaptureRe.FindAllStringSubmatchIndex(text, -1) {
		// m[2], m[3] are the bounds of group 1 (the captured value).
		if len(m) >= 4 && m[2] >= 0 {
			spans = append(spans, Span{Start: m[2], End: m[3], Type: r.Type, Priority: r.Priority, Confidence: conf})
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

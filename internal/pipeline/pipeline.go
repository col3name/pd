package pipeline

import (
	"github.com/kind-earthquake/pii-module/internal/context"
	"github.com/kind-earthquake/pii-module/internal/detector"
	"github.com/kind-earthquake/pii-module/internal/masker"
	"github.com/kind-earthquake/pii-module/internal/resolve"
	"github.com/kind-earthquake/pii-module/internal/whitelist"
)

// Options tunes pipeline behavior.
type Options struct {
	// Mode is "redact" (default) or "token".
	Mode string
	// Sensitive types are masked only when another PII span is present.
	Sensitive []detector.Type
	// ProximityWindow is the byte distance within which a plain DATE escalates
	// to ДАТА_РОЖДЕНИЯ when near FIO / МЕСТО_РОЖДЕНИЯ / АДРЕС / ПАСПОРТ.
	ProximityWindow int
	// Gate is the minimum confidence to mask (default 0.95).
	Gate float32
	// AllowedTypes restricts masking to the given PII types (empty = all).
	AllowedTypes []detector.Type
}

// Result is the outcome of a pipeline run.
type Result struct {
	Masked string
	Types  []string
	Tokens map[string]string
	Spans  []detector.Span
}

// Pipeline runs detect → resolve → whitelist → context → gate → mask.
type Pipeline struct {
	detector  *detector.Detector
	context   *context.Resolver
	whitelist *whitelist.Whitelist
	priority  map[detector.Type]int
	opts      Options
}

// New returns a Pipeline. A nil whitelist disables that stage.
func New(d *detector.Detector, c *context.Resolver, w *whitelist.Whitelist, priority map[detector.Type]int, opts Options) *Pipeline {
	if opts.Mode == "" {
		opts.Mode = "redact"
	}
	if opts.Gate == 0 {
		opts.Gate = 0.95
	}
	return &Pipeline{detector: d, context: c, whitelist: w, priority: priority, opts: opts}
}

// Detector returns the shared detector instance.
func (p *Pipeline) Detector() *detector.Detector { return p.detector }

// Context returns the shared context resolver instance.
func (p *Pipeline) Context() *context.Resolver { return p.context }

// Whitelist returns the shared whitelist instance.
func (p *Pipeline) Whitelist() *whitelist.Whitelist { return p.whitelist }

// Priority returns the type priority map.
func (p *Pipeline) Priority() map[detector.Type]int { return p.priority }

// ProximityWindow returns the configured date-escalation window.
func (p *Pipeline) ProximityWindow() int { return p.opts.ProximityWindow }

// Gate returns the configured confidence gate.
func (p *Pipeline) Gate() float32 { return p.opts.Gate }

// Process runs the full pipeline on text.
func (p *Pipeline) Process(text string) Result {
	spans := p.detector.Detect(text)
	spans = p.escalateDates(text, spans)
	spans = resolve.Resolve(spans, p.priority)

	kept := make([]detector.Span, 0, len(spans))
	for _, s := range spans {
		if p.whitelist != nil && p.whitelist.Blocks(text, s) {
			continue
		}
		conf := s.Confidence
		if p.context != nil {
			conf = p.context.Apply(text, s)
		}
		if conf < p.opts.Gate {
			continue
		}
		s.Confidence = conf
		kept = append(kept, s)
	}

	if len(kept) == 1 && p.isSensitive(kept[0].Type) {
		kept = nil
	}
	kept = p.filterAllowed(kept)

	var masked string
	var tokens map[string]string
	if p.opts.Mode == "token" {
		masked, tokens = masker.Tokenize(text, kept)
	} else {
		masked = masker.Mask(text, kept)
	}
	types := make([]string, 0, len(kept))
	for _, s := range kept {
		types = append(types, string(s.Type))
	}
	return Result{Masked: masked, Types: types, Tokens: tokens, Spans: kept}
}

// filterAllowed drops spans whose type is outside the configured AllowedTypes.
// An empty AllowedTypes keeps every span.
func (p *Pipeline) filterAllowed(spans []detector.Span) []detector.Span {
	if len(p.opts.AllowedTypes) == 0 {
		return spans
	}
	kept := make([]detector.Span, 0, len(spans))
	for _, s := range spans {
		for _, t := range p.opts.AllowedTypes {
			if s.Type == t {
				kept = append(kept, s)
				break
			}
		}
	}
	return kept
}

// escalateDates converts plain ДАТА spans to ДАТА_РОЖДЕНИЯ when they appear
// within ProximityWindow bytes of a personal-data span (ФИО, место рождения,
// адрес, паспорт). This fixes the scoring CRITICAL: "Иванов ..., 12.03.1998".
func (p *Pipeline) escalateDates(text string, spans []detector.Span) []detector.Span {
	window := p.opts.ProximityWindow
	if window == 0 {
		window = 80
	}
	var anchors []detector.Span
	for _, s := range spans {
		switch s.Type {
		case detector.TypeFIO, detector.TypeBirthPlace, detector.TypeAddress, detector.TypePassport:
			anchors = append(anchors, s)
		}
	}
	if len(anchors) == 0 {
		return spans
	}
	out := make([]detector.Span, 0, len(spans))
	for _, s := range spans {
		if s.Type == detector.TypeDate && p.nearAny(s, anchors, window) {
			s.Type = detector.TypeBirthDate
			s.Confidence = 1.0
		}
		out = append(out, s)
	}
	return out
}

func (p *Pipeline) nearAny(s detector.Span, anchors []detector.Span, window int) bool {
	for _, a := range anchors {
		// Distance between the closest edges.
		switch {
		case a.End <= s.Start && s.Start-a.End <= window:
			return true
		case s.End <= a.Start && a.Start-s.End <= window:
			return true
		}
	}
	return false
}

func (p *Pipeline) isSensitive(t detector.Type) bool {
	for _, s := range p.opts.Sensitive {
		if s == t {
			return true
		}
	}
	return false
}
package detector

import "sort"

// Span is a detected personal-data region in the source text.
type Span struct {
	Start      int
	End        int
	Type       Type
	Priority   int
	Confidence float32
}

// Len returns the span length in bytes.
func (s Span) Len() int { return s.End - s.Start }

// ResolveOverlaps returns a set of non-overlapping spans. When spans overlap,
// the longest wins; ties break by higher priority, then earliest start.
func ResolveOverlaps(spans []Span) []Span {
	if len(spans) == 0 {
		return nil
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].Len() != spans[j].Len() {
			return spans[i].Len() > spans[j].Len()
		}
		if spans[i].Priority != spans[j].Priority {
			return spans[i].Priority > spans[j].Priority
		}
		return spans[i].Start < spans[j].Start
	})
	var result []Span
	for _, s := range spans {
		overlap := false
		for _, r := range result {
			if s.Start < r.End && r.Start < s.End {
				overlap = true
				break
			}
		}
		if !overlap {
			result = append(result, s)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Start < result[j].Start })
	return result
}
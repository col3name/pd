package resolve

import (
	"sort"

	"github.com/kind-earthquake/pii-module/internal/detector"
)

// DefaultPriority orders PII types by specificity. Higher wins on overlap.
var DefaultPriority = map[detector.Type]int{
	detector.TypeCard:             100,
	detector.TypePassport:         95,
	detector.TypeDriverLicense:    95,
	detector.TypeForeignPassport:  95,
	detector.TypeMilitaryID:       95,
	detector.TypeBirthCertificate: 95,
	detector.TypePIN:              90,
	detector.TypeCVV:              85,
	detector.TypeFIO:              80,
	detector.TypeCardholder:       80,
	detector.TypeBirthDate:        70,
	detector.TypeAddress:          60,
	detector.TypeEmail:            50,
	detector.TypePhone:            50,
	detector.TypeINN:              40,
	detector.TypeDeptCode:         30,
}

// Resolve returns a non-overlapping set of spans. Overlapping spans are
// resolved by (typePriority desc, length desc, start asc); non-overlapping
// spans are kept regardless of priority.
func Resolve(spans []detector.Span, priority map[detector.Type]int) []detector.Span {
	if len(spans) == 0 {
		return nil
	}
	if priority == nil {
		priority = DefaultPriority
	}
	prio := func(s detector.Span) int {
		if p, ok := priority[s.Type]; ok {
			return p
		}
		return 0
	}
	sort.Slice(spans, func(i, j int) bool {
		pi, pj := prio(spans[i]), prio(spans[j])
		if pi != pj {
			return pi > pj
		}
		li, lj := spans[i].End-spans[i].Start, spans[j].End-spans[j].Start
		if li != lj {
			return li > lj
		}
		return spans[i].Start < spans[j].Start
	})
	var out []detector.Span
	for _, s := range spans {
		overlap := false
		for _, r := range out {
			if s.Start < r.End && r.Start < s.End {
				overlap = true
				break
			}
		}
		if !overlap {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

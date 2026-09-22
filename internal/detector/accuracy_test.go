package detector

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// accuracyCases is a small labeled dataset: each entry has text and the set of
// PII types that MUST be detected (positive) and MUST NOT be detected (negative).
var accuracyCases = []struct {
	text     string
	positive []Type
	negative []Type
}{
	{"Клиент Иванов Иван Иванович, паспорт 4509 123456", []Type{TypePassport, TypeFIO}, nil},
	{"email test@example.com", []Type{TypeEmail}, nil},
	{"телефон +7 912 345-67-89", []Type{TypePhone}, nil},
	{"ИНН 7707083893", []Type{TypeINN}, nil},
	{"карта 4276 1234 5678 9012", []Type{TypeCard}, nil},
	{"дата рождения 15.03.1990", []Type{TypeBirthDate}, nil},
	// Adversarial: a date without birth context is NOT PII.
	{"Банк открылся 12 марта 1998 года", nil, []Type{TypeBirthDate}},
	// Adversarial: literary mention is NOT PII.
	{"Александр Пушкин написал роман", nil, []Type{TypeFIO}},
	{"Лев Толстой родился в Ясной Поляне", nil, []Type{TypeFIO}},
	// Adversarial: bank address is NOT PII.
	{"Банк находится по адресу Москва, ул. Тверская, 10", nil, []Type{TypeAddress}},
	{"Ближайшее отделение банка на ул. Ленина, 10", nil, []Type{TypeAddress}},
	// Positive: client address IS PII.
	{"адрес клиента: г. Москва, ул. Ленина, д. 10", []Type{TypeAddress}, nil},
	{"проживает по адресу: г. Санкт-Петербург, Невский проспект, д. 10", []Type{TypeAddress}, nil},
	// PIN co-occurrence is covered by TestProcessSensitiveCooccurrence in the
	// handler package (the co-occurrence rule lives in the handler, not the
	// detector), so it is intentionally not asserted here.
}

func TestAccuracy(t *testing.T) {
	d := New(StructuredRules())
	total := 0
	correct := 0
	for _, c := range accuracyCases {
		// Apply the same 3-threshold confidence gate the handler uses, so the
		// adversarial "not PII" cases (literary FIO, bank address) reflect what
		// is actually masked rather than every raw candidate span.
		spans := gateForTest(c.text, d.Detect(c.text))
		got := map[Type]bool{}
		for _, s := range spans {
			got[s.Type] = true
		}
		for _, p := range c.positive {
			total++
			if got[p] {
				correct++
			} else {
				t.Errorf("expected %s in %q", p, c.text)
			}
		}
		for _, n := range c.negative {
			total++
			if !got[n] {
				correct++
			} else {
				t.Errorf("did not expect %s in %q", n, c.text)
			}
		}
	}
	require.GreaterOrEqual(t, total, 1)
	require.GreaterOrEqual(t, float64(correct)/float64(total), 0.95, "accuracy below 95%%")
}

// gateForTest mirrors the handler's 3-threshold confidence gate: spans below
// 0.75 are dropped, and mid-confidence spans (0.75-0.95) are kept only when a
// context keyword is present.
func gateForTest(text string, spans []Span) []Span {
	var kept []Span
	for _, s := range spans {
		switch {
		case s.Confidence >= 0.95:
			kept = append(kept, s)
		case s.Confidence >= 0.75:
			if HasContext(text, s.Start, s.End, s.Type) {
				kept = append(kept, s)
			}
		}
	}
	return kept
}
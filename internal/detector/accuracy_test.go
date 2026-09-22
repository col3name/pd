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
	{"Клиент Иванов Иван Иванович, паспорт 4509 123456", []Type{TypePassport}, []Type{TypeFIO}},
	{"email test@example.com", []Type{TypeEmail}, nil},
	{"телефон +7 912 345-67-89", []Type{TypePhone}, nil},
	{"ИНН 7707083893", []Type{TypeINN}, nil},
	{"карта 4276 1234 5678 9012", []Type{TypeCard}, nil},
	{"дата рождения 15.03.1990", []Type{TypeBirthDate}, nil},
}

func TestAccuracy(t *testing.T) {
	d := New(StructuredRules())
	total := 0
	correct := 0
	for _, c := range accuracyCases {
		spans := d.Detect(c.text)
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
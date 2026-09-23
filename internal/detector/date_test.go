package detector

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenericDateDetectedLowConfidence(t *testing.T) {
	d := New(StructuredRules())
	spans := DetectNoGate(t, d, "анкета от 12.03.1998 года")
	// The generic DATE rule produces a mid-confidence span.
	var found bool
	for _, s := range spans {
		if s.Type == TypeDate {
			found = true
			require.Less(t, s.Confidence, float32(0.95))
		}
	}
	require.True(t, found, "generic date must be detected as ДАТА")
}

func TestBirthContextDateIsBirthDate(t *testing.T) {
	d := New(StructuredRules())
	spans := DetectNoGate(t, d, "Дата рождения: 12.03.1998")
	require.Contains(t, spanTypes(spans), TypeBirthDate)
}

func DetectNoGate(t *testing.T, d *Detector, text string) []Span {
	t.Helper()
	return d.Detect(text)
}

func spanTypes(spans []Span) []Type {
	out := make([]Type, 0, len(spans))
	for _, s := range spans {
		out = append(out, s.Type)
	}
	return out
}

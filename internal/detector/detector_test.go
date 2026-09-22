package detector

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetectFindsSpans(t *testing.T) {
	d := New(StructuredRules())
	text := "Клиент Иванов, паспорт 4509 123456, email test@example.com"
	spans := d.Detect(text)
	require.NotEmpty(t, spans)
	// No overlaps after resolution.
	for i := 1; i < len(spans); i++ {
		require.LessOrEqual(t, spans[i-1].End, spans[i].Start)
	}
}

func TestDetectNoPII(t *testing.T) {
	d := New(StructuredRules())
	spans := d.Detect("обычный текст без персональных данных")
	require.Empty(t, spans)
}

func TestDetectCaseInsensitive(t *testing.T) {
	d := New(StructuredRules())
	spans := d.Detect("EMAIL TEST@EXAMPLE.COM")
	require.NotEmpty(t, spans)
	require.Equal(t, TypeEmail, spans[0].Type)
}

func TestDetectPassportVsDriverLicenseContext(t *testing.T) {
	d := New(StructuredRules())

	// Same digit pattern, disambiguated by context keyword.
	passport := d.Detect("паспорт 4509 123456")
	require.Len(t, passport, 1)
	require.Equal(t, TypePassport, passport[0].Type)

	license := d.Detect("водительское удостоверение 77 12 345678")
	require.Len(t, license, 1)
	require.Equal(t, TypeDriverLicense, license[0].Type)

	// Bare number with no context keyword is not detected as passport/license.
	bare := d.Detect("номер 4509 123456")
	require.Empty(t, bare)
}
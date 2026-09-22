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

func TestDetectContextDependentTypes(t *testing.T) {
	d := New(StructuredRules())

	cases := []struct {
		text string
		typ  Type
	}{
		{"место рождения: г. Москва", TypeBirthPlace},
		{"родился в городе Казань", TypeBirthPlace},
		{"гражданство: Российская Федерация", TypeCitizenship},
		{"гражданин России", TypeCitizenship},
		{"выдан ОВД района Хамовники", TypeIssuer},
		{"кем выдан: УФМС России по г. Москве", TypeIssuer},
		{"адрес: г. Москва, ул. Тверская, д. 1, кв. 5", TypeAddress},
		{"проживает по адресу: г. Санкт-Петербург, Невский проспект, д. 10", TypeAddress},
		{"держатель карты: Иван Петров", TypeCardholder},
		{"имя держателя: Мария Иванова", TypeCardholder},
	}
	for _, c := range cases {
		spans := d.Detect(c.text)
		found := false
		for _, s := range spans {
			if s.Type == c.typ {
				found = true
				break
			}
		}
		require.True(t, found, "expected %s in %q, got %v", c.typ, c.text, spans)
	}
}

func TestDetectDateFormats(t *testing.T) {
	d := New(StructuredRules())
	cases := []string{
		"дата рождения 15.03.1990",       // дд.мм.гггг
		"дата рождения 03.15.1990",       // мм.дд.гггг
		"дата рождения 1990.15.03",       // гггг.дд.мм
		"дата рождения 1990-15-03",       // гггг-дд-мм
		"дата рождения 15 марта 1990 года", // text date
		"дата рождения пятнадцатого марта 1990 года", // text date (words)
	}
	for _, c := range cases {
		spans := d.Detect(c)
		found := false
		for _, s := range spans {
			if s.Type == TypeBirthDate {
				found = true
				break
			}
		}
		require.True(t, found, "expected date in %q, got %v", c, spans)
	}
}
package masker

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/detector"
)

func TestSyntheticReplacesEachType(t *testing.T) {
	cases := []struct {
		name string
		text string
		typ  detector.Type
	}{
		{"fio", "Иванов Иван Иванович", detector.TypeFIO},
		{"phone", "+7 999 123-45-67", detector.TypePhone},
		{"email", "ivanov@test.ru", detector.TypeEmail},
		{"card", "4276 1234 5678 9012", detector.TypeCard},
		{"passport", "4509 123456", detector.TypePassport},
		{"driver license", "77 12 345678", detector.TypeDriverLicense},
		{"foreign passport", "71 1234567", detector.TypeForeignPassport},
		{"military id", "АА 123456", detector.TypeMilitaryID},
		{"birth certificate", "I-МЮ 123456", detector.TypeBirthCertificate},
		{"inn", "7707083893", detector.TypeINN},
		{"date", "12.03.1998", detector.TypeDate},
		{"birth date", "12.03.1998", detector.TypeBirthDate},
		{"cvv", "123", detector.TypeCVV},
		{"pin", "1234", detector.TypePIN},
		{"address", "г. Москва, ул. Ленина, д. 10", detector.TypeAddress},
		{"cardholder", "Иванов Иван", detector.TypeCardholder},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spans := []detector.Span{{Start: 0, End: len(tc.text), Type: tc.typ}}
			got := Synthetic(tc.text, spans)
			require.NotEqual(t, tc.text, got, "synthetic must differ from original")
			require.NotEqual(t, tc.typ.Placeholder(), got, "synthetic must not be a placeholder")
		})
	}
}

func TestSyntheticPhoneLike(t *testing.T) {
	got := Synthetic("+7 999 123-45-67", []detector.Span{{Start: 0, End: 16, Type: detector.TypePhone}})
	require.Regexp(t, regexp.MustCompile(`^\+7 \d{3} \d{3}-\d{2}-\d{2}$`), got)
}

func TestSyntheticCardIs16Digits(t *testing.T) {
	got := Synthetic("4276 1234 5678 9012", []detector.Span{{Start: 0, End: 19, Type: detector.TypeCard}})
	digits := regexp.MustCompile(`\d`).FindAllString(got, -1)
	require.Len(t, digits, 16)
	require.True(t, luhnValid(digits))
}

func TestSyntheticDateLike(t *testing.T) {
	got := Synthetic("12.03.1998", []detector.Span{{Start: 0, End: 10, Type: detector.TypeDate}})
	require.Regexp(t, regexp.MustCompile(`^\d{2}\.\d{2}\.\d{4}$`), got)
}

func TestSyntheticDeterministic(t *testing.T) {
	text := "Клиент Иванов Иван Иванович, тел +7 999 123-45-67, карта 4276 1234 5678 9012"
	spans := []detector.Span{
		{Start: 8, End: 30, Type: detector.TypeFIO},
		{Start: 36, End: 52, Type: detector.TypePhone},
		{Start: 60, End: 79, Type: detector.TypeCard},
	}
	first := Synthetic(text, spans)
	second := Synthetic(text, spans)
	require.Equal(t, first, second, "same input must yield same synthetic output")
}

func TestSyntheticFallbackPlaceholder(t *testing.T) {
	got := Synthetic("нечто", []detector.Span{{Start: 0, End: 10, Type: detector.Type("КАКОЙ-ТО_ТИП")}})
	require.Equal(t, "[КАКОЙ-ТО_ТИП]", got)
}

func TestSyntheticNoSpans(t *testing.T) {
	require.Equal(t, "просто текст", Synthetic("просто текст", nil))
}

func luhnValid(digits []string) bool {
	sum := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i][0] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

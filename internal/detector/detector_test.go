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
		"родился 12 марта 1998 года, телефон +7 999 123-45-67", // text date followed by comma
		"родился 12 марта 1998 года.", // text date followed by period
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

func TestDetectPINLatin(t *testing.T) {
	d := New(StructuredRules())
	// Latin "PIN" must be detected like Cyrillic "пин".
	spans := d.Detect("PIN 1234")
	found := false
	for _, s := range spans {
		if s.Type == TypePIN {
			found = true
			break
		}
	}
	require.True(t, found, "expected PIN in %q, got %v", "PIN 1234", spans)
}

func TestDetectSemanticFIO(t *testing.T) {
	d := New(StructuredRules())
	spans := d.Detect("Клиент Иванов Иван Иванович")
	found := false
	for _, s := range spans {
		if s.Type == TypeFIO {
			found = true
			require.GreaterOrEqual(t, s.Confidence, float32(0.95))
		}
	}
	require.True(t, found, "expected FIO in %q, got %v", "Клиент Иванов Иван Иванович", spans)
}

func TestDetectSemanticAddress(t *testing.T) {
	d := New(StructuredRules())
	spans := d.Detect("адрес клиента: г. Москва, ул. Ленина, д. 10")
	found := false
	for _, s := range spans {
		if s.Type == TypeAddress {
			found = true
			require.GreaterOrEqual(t, s.Confidence, float32(0.95))
		}
	}
	require.True(t, found, "expected address in %q, got %v", "адрес клиента: г. Москва, ул. Ленина, д. 10", spans)
}

type mockNER struct {
	spans  []Span
	called bool
}

func (m *mockNER) Detect(text string) []Span {
	m.called = true
	return m.spans
}

func TestDetectSmartPathNoSpans(t *testing.T) {
	// No rule-based spans → NER is called and its spans merged.
	ner := &mockNER{spans: []Span{
		{Start: 0, End: 11, Type: TypeFIO, Confidence: 0.9},
	}}
	d := New(StructuredRules(), WithNER(ner))
	spans := d.Detect("Иван Иванов")
	require.Len(t, spans, 1)
	require.Equal(t, TypeFIO, spans[0].Type)
	require.True(t, ner.called, "NER should be called when no rule-based spans")
}

func TestDetectSmartPathHighConfidenceSkipsNER(t *testing.T) {
	// High-confidence rule-based span → NER NOT called.
	ner := &mockNER{spans: []Span{
		{Start: 0, End: 11, Type: TypeFIO, Confidence: 0.9},
	}}
	d := New(StructuredRules(), WithNER(ner))
	spans := d.Detect("паспорт 4509 123456")
	require.Len(t, spans, 1)
	require.Equal(t, TypePassport, spans[0].Type)
	require.False(t, ner.called, "NER should NOT be called for high-confidence spans")
}

func TestDetectSmartPathFiltersNERFIOOverlappingAddress(t *testing.T) {
	// NER returns a FIO span that overlaps an address. resolveAll must filter
	// it out so street names inside addresses are not misclassified as FIO.
	text := "г. Москва, ул. Ленина, д. 10"
	// The address is detected with mid-confidence (no context keyword), which
	// triggers the smart path.
	ner := &mockNER{spans: []Span{
		{Start: 0, End: len(text), Type: TypeFIO, Confidence: 0.9},
	}}
	d := New(StructuredRules(), WithNER(ner))
	spans := d.Detect(text)
	require.True(t, ner.called, "NER should be called for mid-confidence spans")
	for _, s := range spans {
		require.NotEqual(t, TypeFIO, s.Type, "NER FIO overlapping an address must be filtered")
	}
}

func TestDetectOtherDocuments(t *testing.T) {
	d := New(StructuredRules())
	cases := []struct {
		text string
		typ  Type
	}{
		{"загранпаспорт 71 1234567", TypeForeignPassport},
		{"заграничный паспорт 71 1234567", TypeForeignPassport},
		{"военный билет 77 12 345678", TypeMilitaryID},
		{"свидетельство о рождении 77 12 345678", TypeBirthCertificate},
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
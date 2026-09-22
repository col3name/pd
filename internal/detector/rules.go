package detector

import "regexp"

// Rule binds a compiled regex to a PII type. Priority breaks ties when two
// rules match the same span with equal length: higher priority wins.
// ContextRe, when set, must match in the text preceding the span for the rule
// to apply. It disambiguates structurally identical patterns (e.g. passport
// vs driver license) by surrounding keywords.
// CaptureRe, when set, matches a keyword followed by the PII value captured in
// group 1. It is used for context-dependent types (birth place, citizenship,
// issuer, address) where the value is free text after a keyword.
// Keyword, when set, is a cheap case-insensitive substring pre-check: if it is
// absent from the text, the expensive CaptureRe is skipped entirely.
type Rule struct {
	Type      Type
	Re        *regexp.Regexp
	Priority  int
	ContextRe *regexp.Regexp
	CaptureRe *regexp.Regexp
	Keyword   string
}

// StructuredRules returns regex rules for structured PII types.
func StructuredRules() []Rule {
	return []Rule{
		{TypePassport, regexp.MustCompile(`(?i)\b\d{2}\s?\d{2}\s\d{6}\b`), 2,
			regexp.MustCompile(`(?i)(?:паспорт|серия\s+паспорта|паспорт\s+серия|серия\s+и\s+номер\s+паспорта)`), nil, "",
		},
		{TypeINN, regexp.MustCompile(`(?i)\b\d{10,12}\b`), 0, nil, nil, ""},
		{TypeCard, regexp.MustCompile(`(?i)\b\d{4}\s?\d{4}\s?\d{4}\s?\d{4}\b`), 0, nil, nil, ""},
		{TypeCVV, regexp.MustCompile(`(?i)\bcvv[:\s]*\d{3}\b`), 0, nil, nil, ""},
		{TypePIN, regexp.MustCompile(`(?i)(?:^|\s)(?:пин|pin)[:\s]*\d{4}\b`), 0, nil, nil, ""},
		{TypeEmail, regexp.MustCompile(`(?i)\b[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}\b`), 0, nil, nil, ""},
		{TypePhone, regexp.MustCompile(`(?i)(?:\+7|\b8)[\s\-]?\(?\d{3}\)?[\s\-]?\d{3}[\s\-]?\d{2}[\s\-]?\d{2}`), 0, nil, nil, ""},
		{TypeDeptCode, regexp.MustCompile(`(?i)\b\d{3}[-–]\d{3}\b`), 0, nil, nil, ""},
		{TypeDriverLicense, regexp.MustCompile(`(?i)\b\d{2}\s?\d{2}\s\d{6}\b`), 1,
			regexp.MustCompile(`(?i)(?:водительское\s+удостоверение|в/у|удостоверение)`), nil, "",
		},
		// Foreign passport: series 2 digits + number 7 digits.
		{TypeForeignPassport, regexp.MustCompile(`(?i)\b\d{2}\s?\d{7}\b`), 2,
			regexp.MustCompile(`(?i)(?:загранпаспорт|заграничный\s+паспорт)`), nil, "",
		},
		// Military ID: series 2+2 digits + number 6 digits.
		{TypeMilitaryID, regexp.MustCompile(`(?i)\b\d{2}\s?\d{2}\s\d{6}\b`), 2,
			regexp.MustCompile(`(?i)(?:военный\s+билет|военник)`), nil, "",
		},
		// Birth certificate: series 2+2 digits + number 6 digits.
		{TypeBirthCertificate, regexp.MustCompile(`(?i)\b\d{2}\s?\d{2}\s\d{6}\b`), 2,
			regexp.MustCompile(`(?i)(?:свидетельство\s+о\s+рождении)`), nil, "",
		},
		{TypeBirthDate, regexp.MustCompile(`(?i)\b\d{2}[./-]\d{2}[./-]\d{4}\b`), 0,
			regexp.MustCompile(`(?i)(?:дата\s+рождения|родился|родилась|родился\s+в|родилась\s+в|год\s+рождения|день\s+рождения)`), nil, ""},
		// Year-first date format: гггг.дд.мм or гггг-дд-мм.
		{TypeBirthDate, regexp.MustCompile(`(?i)\b\d{4}[./-]\d{2}[./-]\d{2}\b`), 0,
			regexp.MustCompile(`(?i)(?:дата\s+рождения|родился|родилась|родился\s+в|родилась\s+в|год\s+рождения|день\s+рождения)`), nil, ""},
		// Text date: "15 марта 1990 года" or "пятнадцатого марта 1990 года".
		{TypeBirthDate, regexp.MustCompile(`(?i)(?:^|\s)\d{1,2}\s+[а-яё]+\s+\d{4}\s+года(?:\s|[,.;]|$)`), 0,
			regexp.MustCompile(`(?i)(?:дата\s+рождения|родился|родилась|родился\s+в|родилась\s+в|год\s+рождения|день\s+рождения)`), nil, ""},
		{TypeBirthDate, regexp.MustCompile(`(?i)(?:^|\s)[а-яё]+\s+[а-яё]+\s+\d{4}\s+года(?:\s|[,.;]|$)`), 0,
			regexp.MustCompile(`(?i)(?:дата\s+рождения|родился|родилась|родился\s+в|родилась\s+в|год\s+рождения|день\s+рождения)`), nil, ""},
		// Context-dependent types: capture free text after a keyword.
		{TypeBirthPlace, nil, 0, nil,
			regexp.MustCompile(`(?i)(?:место\s+рождения|родил[а-я]+\s+в|родился\s+в|родилась\s+в)[:\s]+([^,;\n]{2,60})`), ""},
		{TypeCitizenship, nil, 0, nil,
			regexp.MustCompile(`(?i)(?:гражданство|гражданин|гражданка)[:\s]+([а-яё\s]{2,40})`), "граждан"},
		{TypeIssuer, nil, 0, nil,
			regexp.MustCompile(`(?i)(?:выдан|выдал|орган\s+выдавший|кем\s+выдан)[:\s]+([^,.;\n]{3,80})`), "выд"},
		{TypeAddress, nil, 0, nil,
			regexp.MustCompile(`(?i)(?:адрес|проживает\s+по\s+адресу|проживает|зарегистрирован|прописан|место\s+жительства)[:\s]+((?:г\.|ул\.|д\.|кв\.|проспект|переулок|шоссе|бульвар|набережная|область|край|республика|район|поселок|деревня|село)[^,;]*(?:\s*,\s*(?:г\.|ул\.|д\.|кв\.|проспект|переулок|шоссе|бульвар|набережная|область|край|республика|район|поселок|деревня|село)[^,;]*)*)`), "адрес"},
		{TypeCardholder, nil, 0, nil,
			regexp.MustCompile(`(?i)(?:держатель\s+карты|cardholder|имя\s+держателя)[:\s]+([а-яё]{2,30}\s+[а-яё]{2,30})`), "держател"},
	}
}
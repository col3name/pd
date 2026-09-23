package detector

import (
	"fmt"
	"regexp"
	"sort"
)

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
	// Confidence is the base confidence set on every span the rule produces.
	// Defaults to 1.0 (high confidence) when zero.
	Confidence float32
}

// StructuredRules returns regex rules for structured PII types.
func StructuredRules() []Rule {
	return []Rule{
		{TypePassport, regexp.MustCompile(`(?i)\b\d{2}\s?\d{2}\s\d{6}\b`), 2,
			regexp.MustCompile(`(?i)(?:паспорт|серия\s+паспорта|паспорт\s+серия|серия\s+и\s+номер\s+паспорта)`), nil, "", 0,
		},
		{TypeINN, regexp.MustCompile(`(?i)\b\d{10,12}\b`), 0, nil, nil, "", 0},
		{TypeCard, regexp.MustCompile(`(?i)\b\d{4}\s?\d{4}\s?\d{4}\s?\d{4}\b`), 0, nil, nil, "", 0},
		{TypeCVV, regexp.MustCompile(`(?i)\bcvv[:\s]*\d{3}\b`), 0, nil, nil, "", 0},
		{TypePIN, regexp.MustCompile(`(?i)(?:^|\s)(?:пин|pin)[:\s]*\d{4}\b`), 0, nil, nil, "", 0},
		{TypeEmail, regexp.MustCompile(`(?i)\b[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}\b`), 0, nil, nil, "", 0},
		{TypePhone, regexp.MustCompile(`(?i)(?:\+7|\b8)[\s\-]?\(?\d{3}\)?[\s\-]?\d{3}[\s\-]?\d{2}[\s\-]?\d{2}`), 0, nil, nil, "", 0},
		{TypeDeptCode, regexp.MustCompile(`(?i)\b\d{3}[-–]\d{3}\b`), 0, nil, nil, "", 0},
		{TypeDriverLicense, regexp.MustCompile(`(?i)\b\d{2}\s?\d{2}\s\d{6}\b`), 1,
			regexp.MustCompile(`(?i)(?:водительское\s+удостоверение|в/у|удостоверение)`), nil, "", 0,
		},
		// Foreign passport: series 2 digits + number 7 digits.
		{TypeForeignPassport, regexp.MustCompile(`(?i)\b\d{2}\s?\d{7}\b`), 2,
			regexp.MustCompile(`(?i)(?:загранпаспорт|заграничный\s+паспорт)`), nil, "", 0,
		},
		// Military ID: series 2+2 digits + number 6 digits.
		{TypeMilitaryID, regexp.MustCompile(`(?i)\b\d{2}\s?\d{2}\s\d{6}\b`), 2,
			regexp.MustCompile(`(?i)(?:военный\s+билет|военник)`), nil, "", 0,
		},
		// Birth certificate: series 2+2 digits + number 6 digits.
		{TypeBirthCertificate, regexp.MustCompile(`(?i)\b\d{2}\s?\d{2}\s\d{6}\b`), 2,
			regexp.MustCompile(`(?i)(?:свидетельство\s+о\s+рождении)`), nil, "", 0,
		},
		{TypeBirthDate, regexp.MustCompile(`(?i)\b\d{2}[./-]\d{2}[./-]\d{4}\b`), 0,
			regexp.MustCompile(`(?i)(?:дата\s+рождения|родился|родилась|родился\s+в|родилась\s+в|год\s+рождения|день\s+рождения)`), nil, "", 0},
		// Year-first date format: гггг.дд.мм or гггг-дд-мм.
		{TypeBirthDate, regexp.MustCompile(`(?i)\b\d{4}[./-]\d{2}[./-]\d{2}\b`), 0,
			regexp.MustCompile(`(?i)(?:дата\s+рождения|родился|родилась|родился\s+в|родилась\s+в|год\s+рождения|день\s+рождения)`), nil, "", 0},
		// Generic date: matches any date; the pipeline escalates it to
		// ДАТА_РОЖДЕНИЯ only when near other PII (see internal/pipeline).
		{TypeDate, regexp.MustCompile(`(?i)\b\d{2}[./-]\d{2}[./-]\d{4}\b`), 0, nil, nil, "", 0.7},
		{TypeDate, regexp.MustCompile(`(?i)\b\d{4}[./-]\d{2}[./-]\d{2}\b`), 0, nil, nil, "", 0.7},
		// Text date: "15 марта 1990 года" or "пятнадцатого марта 1990 года".
		{TypeBirthDate, regexp.MustCompile(`(?i)(?:^|\s)\d{1,2}\s+[а-яё]+\s+\d{4}\s+года(?:\s|[,.;]|$)`), 0,
			regexp.MustCompile(`(?i)(?:дата\s+рождения|родился|родилась|родился\s+в|родилась\s+в|год\s+рождения|день\s+рождения)`), nil, "", 0},
		{TypeBirthDate, regexp.MustCompile(`(?i)(?:^|\s)[а-яё]+\s+[а-яё]+\s+\d{4}\s+года(?:\s|[,.;]|$)`), 0,
			regexp.MustCompile(`(?i)(?:дата\s+рождения|родился|родилась|родился\s+в|родилась\s+в|год\s+рождения|день\s+рождения)`), nil, "", 0},
		// Context-dependent types: capture free text after a keyword.
		{TypeBirthPlace, nil, 0, nil,
			regexp.MustCompile(`(?i)(?:место\s+рождения|родил[а-я]+\s+в|родился\s+в|родилась\s+в)[:\s]+([^,;\n]{2,60})`), "", 0},
		{TypeCitizenship, nil, 0, nil,
			regexp.MustCompile(`(?i)(?:гражданство|гражданин|гражданка)[:\s]+([а-яё\s]{2,40})`), "граждан", 0},
		{TypeIssuer, nil, 0, nil,
			regexp.MustCompile(`(?i)(?:выдан|выдал|орган\s+выдавший|кем\s+выдан)[:\s]+([^,.;\n]{3,80})`), "выд", 0},
		{TypeAddress, nil, 0, nil,
			regexp.MustCompile(`(?i)(?:адрес|проживает\s+по\s+адресу|проживает|зарегистрирован|прописан|место\s+жительства)[:\s]+((?:г\.|ул\.|д\.|кв\.|проспект|переулок|шоссе|бульвар|набережная|область|край|республика|район|поселок|деревня|село)[^,;]*(?:\s*,\s*(?:г\.|ул\.|д\.|кв\.|проспект|переулок|шоссе|бульвар|набережная|область|край|республика|район|поселок|деревня|село)[^,;]*)*)`), "адрес", 0},
		{TypeCardholder, nil, 0, nil,
			regexp.MustCompile(`(?i)(?:держатель\s+карты|cardholder|имя\s+держателя)[:\s]+([а-яё]{2,30}\s+[а-яё]{2,30})`), "держател", 0},
	}
}

// RuleFromConfig compiles a user-supplied rule (from the config rules section).
// Detector must not import config (config imports detector), so parameters are
// passed as plain values.
func RuleFromConfig(typeName, regex, contextRe, captureRe string, priority int, confidence float32, keyword string) (Rule, error) {
	r := Rule{
		Type:       Type(typeName),
		Priority:   priority,
		Keyword:    keyword,
		Confidence: confidence,
	}
	var err error
	if regex != "" {
		if r.Re, err = regexp.Compile(regex); err != nil {
			return Rule{}, fmt.Errorf("rule %s: bad regex: %w", typeName, err)
		}
	} else {
		return Rule{}, fmt.Errorf("rule %s: regex is required", typeName)
	}
	if contextRe != "" {
		if r.ContextRe, err = regexp.Compile(contextRe); err != nil {
			return Rule{}, fmt.Errorf("rule %s: bad context: %w", typeName, err)
		}
	}
	if captureRe != "" {
		if r.CaptureRe, err = regexp.Compile(captureRe); err != nil {
			return Rule{}, fmt.Errorf("rule %s: bad capture: %w", typeName, err)
		}
	}
	return r, nil
}

// KnownTypes returns every built-in PII type, sorted, for UI pickers.
func KnownTypes() []Type {
	types := make([]Type, 0, len(TypeList))
	types = append(types, TypeList...)
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
	return types
}

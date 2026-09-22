package detector

import "regexp"

// Rule binds a compiled regex to a PII type. Priority breaks ties when two
// rules match the same span with equal length: higher priority wins.
// ContextRe, when set, must match in the text preceding the span for the rule
// to apply. It disambiguates structurally identical patterns (e.g. passport
// vs driver license) by surrounding keywords.
type Rule struct {
	Type      Type
	Re        *regexp.Regexp
	Priority  int
	ContextRe *regexp.Regexp
}

// StructuredRules returns regex rules for structured PII types.
func StructuredRules() []Rule {
	return []Rule{
		{TypePassport, regexp.MustCompile(`(?i)\b\d{2}\s?\d{2}\s\d{6}\b`), 2,
			regexp.MustCompile(`(?i)(?:паспорт|серия\s+паспорта|паспорт\s+серия|серия\s+и\s+номер\s+паспорта)`),
		},
		{TypeINN, regexp.MustCompile(`(?i)\b\d{10,12}\b`), 0, nil},
		{TypeCard, regexp.MustCompile(`(?i)\b\d{4}\s?\d{4}\s?\d{4}\s?\d{4}\b`), 0, nil},
		{TypeCVV, regexp.MustCompile(`(?i)\bcvv[:\s]*\d{3}\b`), 0, nil},
		{TypePIN, regexp.MustCompile(`(?i)(?:^|\s)пин[:\s]*\d{4}\b`), 0, nil},
		{TypeEmail, regexp.MustCompile(`(?i)\b[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}\b`), 0, nil},
		{TypePhone, regexp.MustCompile(`(?i)(?:\+7|\b8)[\s\-]?\(?\d{3}\)?[\s\-]?\d{3}[\s\-]?\d{2}[\s\-]?\d{2}`), 0, nil},
		{TypeDeptCode, regexp.MustCompile(`(?i)\b\d{3}[-–]\d{3}\b`), 0, nil},
		{TypeDriverLicense, regexp.MustCompile(`(?i)\b\d{2}\s?\d{2}\s\d{6}\b`), 1,
			regexp.MustCompile(`(?i)(?:водительское\s+удостоверение|в/у|удостоверение)`),
		},
		{TypeBirthDate, regexp.MustCompile(`(?i)\b\d{2}[./-]\d{2}[./-]\d{4}\b`), 0, nil},
	}
}
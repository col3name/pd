package detector

// Type identifies a category of personal data.
type Type string

// TypeList is the full set of built-in PII types.
var TypeList = []Type{
	TypeAddress, TypeBirthCertificate, TypeBirthDate, TypeBirthPlace,
	TypeCard, TypeCardholder, TypeCitizenship, TypeCVV, TypeDate,
	TypeDeptCode, TypeDriverLicense, TypeEmail, TypeFIO, TypeForeignPassport,
	TypeINN, TypeIssuer, TypeMilitaryID, TypePassport, TypePassportIssue,
	TypePhone, TypePIN,
}

const (
	TypeFIO              Type = "ФИО"
	TypeDate             Type = "ДАТА"
	TypeBirthDate        Type = "ДАТА_РОЖДЕНИЯ"
	TypeBirthPlace       Type = "МЕСТО_РОЖДЕНИЯ"
	TypePassport         Type = "ПАСПОРТ"
	TypeCitizenship      Type = "ГРАЖДАНСТВО"
	TypeIssuer           Type = "ОРГАН"
	TypeDeptCode         Type = "КОД_ПОДРАЗДЕЛЕНИЯ"
	TypePassportIssue    Type = "ДАТА_ВЫДАЧИ"
	TypeDriverLicense    Type = "ВУ"
	TypeForeignPassport  Type = "ЗАГРАНПАСПОРТ"
	TypeMilitaryID       Type = "ВОЕННЫЙ_БИЛЕТ"
	TypeBirthCertificate Type = "СВИДЕТЕЛЬСТВО_О_РОЖДЕНИИ"
	TypeAddress          Type = "АДРЕС"
	TypeEmail            Type = "EMAIL"
	TypePhone            Type = "ТЕЛЕФОН"
	TypeINN              Type = "ИНН"
	TypeCard             Type = "КАРТА"
	TypeCVV              Type = "CVV"
	TypePIN              Type = "ПИН"
	TypeCardholder       Type = "ДЕРЖАТЕЛЬ"
)

var placeholders = map[Type]string{
	TypeFIO:              "[ФИО]",
	TypeDate:             "[ДАТА]",
	TypeBirthDate:        "[ДАТА]",
	TypeBirthPlace:       "[МЕСТО_РОЖДЕНИЯ]",
	TypePassport:         "[ПАСПОРТ]",
	TypeCitizenship:      "[ГРАЖДАНСТВО]",
	TypeIssuer:           "[ОРГАН]",
	TypeDeptCode:         "[КОД_ПОДРАЗДЕЛЕНИЯ]",
	TypePassportIssue:    "[ДАТА_ВЫДАЧИ]",
	TypeDriverLicense:    "[ВУ]",
	TypeForeignPassport:  "[ЗАГРАНПАСПОРТ]",
	TypeMilitaryID:       "[ВОЕННЫЙ_БИЛЕТ]",
	TypeBirthCertificate: "[СВИДЕТЕЛЬСТВО_О_РОЖДЕНИИ]",
	TypeAddress:          "[АДРЕС]",
	TypeEmail:            "[EMAIL]",
	TypePhone:            "[ТЕЛЕФОН]",
	TypeINN:              "[ИНН]",
	TypeCard:             "[КАРТА]",
	TypeCVV:              "[CVV]",
	TypePIN:              "[ПИН]",
	TypeCardholder:       "[ДЕРЖАТЕЛЬ]",
}

// Placeholder returns the masking token for the type.
func (t Type) Placeholder() string {
	if p, ok := placeholders[t]; ok {
		return p
	}
	return "[" + string(t) + "]"
}

// Valid reports whether t can be masked. Unknown types (from overlay rules)
// are valid: they render as [TYPE] via the placeholder fallback.
func (t Type) Valid() bool { return t != "" }

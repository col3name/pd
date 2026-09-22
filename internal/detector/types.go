package detector

// Type identifies a category of personal data.
type Type string

const (
	TypeFIO           Type = "ФИО"
	TypeBirthDate     Type = "ДАТА_РОЖДЕНИЯ"
	TypeBirthPlace    Type = "МЕСТО_РОЖДЕНИЯ"
	TypePassport      Type = "ПАСПОРТ"
	TypeCitizenship   Type = "ГРАЖДАНСТВО"
	TypeIssuer        Type = "ОРГАН"
	TypeDeptCode      Type = "КОД_ПОДРАЗДЕЛЕНИЯ"
	TypePassportIssue Type = "ДАТА_ВЫДАЧИ"
	TypeDriverLicense Type = "ВУ"
	TypeAddress       Type = "АДРЕС"
	TypeEmail         Type = "EMAIL"
	TypePhone         Type = "ТЕЛЕФОН"
	TypeINN           Type = "ИНН"
	TypeCard          Type = "КАРТА"
	TypeCVV           Type = "CVV"
	TypePIN           Type = "ПИН"
	TypeCardholder    Type = "ДЕРЖАТЕЛЬ"
)

var placeholders = map[Type]string{
	TypeFIO:           "[ФИО]",
	TypeBirthDate:     "[ДАТА]",
	TypeBirthPlace:    "[МЕСТО_РОЖДЕНИЯ]",
	TypePassport:      "[ПАСПОРТ]",
	TypeCitizenship:   "[ГРАЖДАНСТВО]",
	TypeIssuer:        "[ОРГАН]",
	TypeDeptCode:      "[КОД_ПОДРАЗДЕЛЕНИЯ]",
	TypePassportIssue: "[ДАТА_ВЫДАЧИ]",
	TypeDriverLicense: "[ВУ]",
	TypeAddress:       "[АДРЕС]",
	TypeEmail:         "[EMAIL]",
	TypePhone:         "[ТЕЛЕФОН]",
	TypeINN:           "[ИНН]",
	TypeCard:          "[КАРТА]",
	TypeCVV:           "[CVV]",
	TypePIN:           "[ПИН]",
	TypeCardholder:    "[ДЕРЖАТЕЛЬ]",
}

// Placeholder returns the masking token for the type.
func (t Type) Placeholder() string {
	return placeholders[t]
}

// Valid reports whether t is a known PII type.
func (t Type) Valid() bool {
	_, ok := placeholders[t]
	return ok
}
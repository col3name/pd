package detector

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTypePlaceholder(t *testing.T) {
	cases := map[Type]string{
		TypeFIO:           "[ФИО]",
		TypePassport:      "[ПАСПОРТ]",
		TypePhone:         "[ТЕЛЕФОН]",
		TypeEmail:         "[EMAIL]",
		TypeBirthDate:     "[ДАТА]",
		TypeAddress:       "[АДРЕС]",
		TypeCard:          "[КАРТА]",
		TypeINN:           "[ИНН]",
		TypeDriverLicense: "[ВУ]",
		TypeCitizenship:   "[ГРАЖДАНСТВО]",
		TypeIssuer:        "[ОРГАН]",
		TypeDeptCode:      "[КОД_ПОДРАЗДЕЛЕНИЯ]",
		TypeCVV:           "[CVV]",
		TypePIN:           "[ПИН]",
		TypeCardholder:    "[ДЕРЖАТЕЛЬ]",
		TypeBirthPlace:    "[МЕСТО_РОЖДЕНИЯ]",
		TypePassportIssue: "[ДАТА_ВЫДАЧИ]",
	}
	for typ, want := range cases {
		require.Equal(t, want, typ.Placeholder(), "placeholder for %s", typ)
		require.True(t, typ.Valid(), "valid for %s", typ)
	}
}

func TestTypeUnknownPlaceholderFallback(t *testing.T) {
	got := Type("СНИЛС").Placeholder()
	require.Equal(t, "[СНИЛС]", got)
	require.True(t, Type("СНИЛС").Valid())
}

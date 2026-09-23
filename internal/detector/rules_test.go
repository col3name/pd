package detector

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStructuredRulesMatch(t *testing.T) {
	rules := StructuredRules()
	require.NotEmpty(t, rules)

	cases := []struct {
		text string
		typ  Type
	}{
		{"паспорт 4509 123456", TypePassport},
		{"ИНН 7707083893", TypeINN},
		{"карта 4276 1234 5678 9012", TypeCard},
		{"cvv 123", TypeCVV},
		{"пин 1234", TypePIN},
		{"email test@example.com", TypeEmail},
		{"телефон +7 912 345-67-89", TypePhone},
		{"код подразделения 770-001", TypeDeptCode},
		{"водительское удостоверение 77 12 345678", TypeDriverLicense},
		{"дата рождения 15.03.1990", TypeBirthDate},
	}
	for _, c := range cases {
		found := false
		for _, r := range rules {
			if r.Type == c.typ && r.Re.MatchString(c.text) {
				found = true
				break
			}
		}
		require.True(t, found, "no rule matched %q for %s", c.text, c.typ)
	}
}

func TestStructuredRulesCaseInsensitive(t *testing.T) {
	rules := StructuredRules()
	for _, r := range rules {
		if r.Type == TypeEmail {
			require.True(t, r.Re.MatchString("EMAIL TEST@EXAMPLE.COM"))
		}
	}
}

func TestRuleFromConfigValid(t *testing.T) {
	r, err := RuleFromConfig("СНИЛС", `\d{3}-\d{3}-\d{3}\s\d{2}`, "", "", 0, 0.99, "")
	require.NoError(t, err)
	require.Equal(t, Type("СНИЛС"), r.Type)
	require.Equal(t, float32(0.99), r.Confidence)

	d := New(StructuredRules(), WithExtraRules([]Rule{r}))
	spans := d.Detect("снилс 123-456-789 01")
	require.NotEmpty(t, spans)
}

func TestRuleFromConfigBadRegex(t *testing.T) {
	_, err := RuleFromConfig("X", "(", "", "", 0, 0, "")
	require.Error(t, err)
}

func TestKnownTypes(t *testing.T) {
	types := KnownTypes()
	require.Len(t, types, len(TypeList))
	for i := 1; i < len(types); i++ {
		require.Less(t, types[i-1], types[i], "KnownTypes must be sorted")
	}
}

package pipeline_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/context"
	"github.com/kind-earthquake/pii-module/internal/detector"
	"github.com/kind-earthquake/pii-module/internal/masker"
	"github.com/kind-earthquake/pii-module/internal/pipeline"
	"github.com/kind-earthquake/pii-module/internal/resolve"
	"github.com/kind-earthquake/pii-module/internal/whitelist"
)

func newPipeline(mode string) *pipeline.Pipeline {
	return pipeline.New(
		detector.New(detector.StructuredRules()),
		context.New(context.DefaultBoost, context.DefaultPenalty),
		whitelist.New(
			[]string{"Александр Пушкин", "Лев Толстой"},
			[]string{"отделение Альфа-Банка"},
			nil,
		),
		resolve.DefaultPriority,
		pipeline.Options{
			Mode:      mode,
			Gate:      0.95,
			Sensitive: []detector.Type{detector.TypePIN, detector.TypeCVV},
		},
	)
}

func TestPipelineRedactMasksClient(t *testing.T) {
	text := "Клиент Иванов Иван Иванович, паспорт 4509 123456, 12.03.1998, тел +7 999 123-45-67"
	res := newPipeline("redact").Process(text)
	require.Contains(t, res.Masked, "[ФИО]")
	require.Contains(t, res.Masked, "[ПАСПОРТ]")
	require.Contains(t, res.Masked, "[ДАТА]")
	require.Contains(t, res.Masked, "[ТЕЛЕФОН]")
}

func TestPipelineEscalatesDateToBirthDateNearFIO(t *testing.T) {
	// This is the scorer CRITICAL case: date next to FIO in a personal block.
	text := "указал: Иванов Иван Иванович, 12.03.1998, место рождения Москва"
	res := newPipeline("redact").Process(text)
	require.Contains(t, res.Masked, "[ДАТА]")
	// The date span type must have escalated to BIRTH_DATE.
	require.True(t, containsType(res.Spans, detector.TypeBirthDate))
}

func TestPipelineDoesNotMaskPlainDate(t *testing.T) {
	res := newPipeline("redact").Process("Банк открылся 12 марта 1998 года, а его регистрация 15.07.2001")
	require.Equal(t, "Банк открылся 12 марта 1998 года, а его регистрация 15.07.2001", res.Masked)
}

func TestPipelinePushkinNotMasked(t *testing.T) {
	res := newPipeline("redact").Process("Поэт Александр Пушкин написал роман")
	require.NotContains(t, res.Masked, "[ФИО]")
}

func TestPipelineBankBranchAddressNotMasked(t *testing.T) {
	res := newPipeline("redact").Process("Ближайшее отделение Альфа-Банка на ул. Тверская, 10")
	require.NotContains(t, res.Masked, "[АДРЕС]")
}

func TestPipelineClientAddressMasked(t *testing.T) {
	res := newPipeline("redact").Process("адрес клиента: г. Москва, ул. Ленина, д. 10")
	require.Contains(t, res.Masked, "[АДРЕС]")
}

func TestPipelineLonePINNotMasked(t *testing.T) {
	res := newPipeline("redact").Process("Введите пин 1234")
	require.NotContains(t, res.Masked, "[ПИН]")
}

func TestPipelineTokenModeRoundTrip(t *testing.T) {
	orig := "Клиент Иванов Иван Иванович, паспорт 4509 123456"
	res := newPipeline("token").Process(orig)
	require.Contains(t, res.Masked, "[PERSON_001]")
	require.Contains(t, res.Masked, "[PASSPORT_002]")
	require.NotEqual(t, orig, res.Masked)
	restored := masker.Detokenize(res.Masked, res.Tokens)
	require.Equal(t, orig, restored)
}

func containsType(spans []detector.Span, t detector.Type) bool {
	for _, s := range spans {
		if s.Type == t {
			return true
		}
	}
	return false
}
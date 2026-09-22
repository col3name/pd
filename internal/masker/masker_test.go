package masker

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/detector"
)

func TestMaskReplacesSpans(t *testing.T) {
	text := "паспорт 4509 123456 и email test@example.com"
	spans := []detector.Span{
		{Start: 15, End: 26, Type: detector.TypePassport},
		{Start: 36, End: 52, Type: detector.TypeEmail},
	}
	got := Mask(text, spans)
	require.Equal(t, "паспорт [ПАСПОРТ] и email [EMAIL]", got)
}

func TestMaskNoSpans(t *testing.T) {
	require.Equal(t, "просто текст", Mask("просто текст", nil))
}

func TestMaskPreservesNonPII(t *testing.T) {
	text := "Клиент Иванов, телефон +7 912 345-67-89"
	spans := []detector.Span{{Start: 42, End: 58, Type: detector.TypePhone}}
	got := Mask(text, spans)
	require.Equal(t, "Клиент Иванов, телефон [ТЕЛЕФОН]", got)
}
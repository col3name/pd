package masker_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/detector"
	"github.com/kind-earthquake/pii-module/internal/masker"
)

func TestTokenizeDeterministic(t *testing.T) {
	spans := []detector.Span{
		{Start: 8, End: 30, Type: detector.TypeFIO},
		{Start: 40, End: 56, Type: detector.TypePhone},
		{Start: 58, End: 66, Type: detector.TypePhone},
	}
	text := "Клиент Иванов Иван Иванович, тел. +7 999 123-45-67, дом +7 111 222-33-44"
	m1, tk1 := masker.Tokenize(text, spans)
	m2, tk2 := masker.Tokenize(text, spans)
	require.Equal(t, m1, m2, "tokenization must be deterministic")
	require.Equal(t, tk1, tk2, "token map must be deterministic")
	require.Len(t, tk1, 3)
}

func TestTokenizeExact(t *testing.T) {
	text := "Клиент Иванов Иван Иванович, +7 999 123-45-67"
	fioStart := strings.Index(text, "Иванов")
	phoneStart := strings.Index(text, "+7")
	spans := []detector.Span{
		{Start: fioStart, End: fioStart + len("Иванов Иван Иванович"), Type: detector.TypeFIO},
		{Start: phoneStart, End: phoneStart + len("+7 999 123-45-67"), Type: detector.TypePhone},
	}
	masked, tokens := masker.Tokenize(text, spans)
	require.Equal(t, "Клиент [PERSON_001], [PHONE_002]", masked)
	require.Len(t, tokens, 2)
	require.Equal(t, "Иванов Иван Иванович", tokens["[PERSON_001]"])
	require.Equal(t, "+7 999 123-45-67", tokens["[PHONE_002]"])
}

func TestDetokenizeRoundTrip(t *testing.T) {
	orig := "Клиент Иванов Иван Иванович, +7 999 123-45-67"
	fioStart := strings.Index(orig, "Иванов")
	phoneStart := strings.Index(orig, "+7")
	spans := []detector.Span{
		{Start: fioStart, End: fioStart + len("Иванов Иван Иванович"), Type: detector.TypeFIO},
		{Start: phoneStart, End: phoneStart + len("+7 999 123-45-67"), Type: detector.TypePhone},
	}
	masked, tokens := masker.Tokenize(orig, spans)
	restored := masker.Detokenize(masked, tokens)
	require.Equal(t, orig, restored)
}

func TestLabelUnknown(t *testing.T) {
	require.Equal(t, "PASSPORT", masker.Label(detector.TypePassport))
	require.Equal(t, "PERSON", masker.Label(detector.TypeFIO))
}
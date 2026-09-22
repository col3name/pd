package detector

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsSurname(t *testing.T) {
	require.True(t, isSurname("Иванов"))
	require.True(t, isSurname("Петров"))
	require.True(t, isSurname("Сидорова"))
	require.True(t, isSurname("Ковальчук"))
	require.True(t, isSurname("Шевченко"))
	require.False(t, isSurname("Москва"))
	require.False(t, isSurname("Иван"))
}

func TestScoreConfidence(t *testing.T) {
	// base + dict + context = 1.0
	require.Equal(t, float32(1.0), scoreConfidence(0.5, true, 1, 0))
	// base + dict, no context = 0.8
	require.Equal(t, float32(0.8), scoreConfidence(0.5, true, 0, 0))
	// base + dict - negative = 0.5
	require.Equal(t, float32(0.5), scoreConfidence(0.5, true, 0, 1))
	// context capped at +0.4
	require.Equal(t, float32(1.0), scoreConfidence(0.5, true, 3, 0))
	// clamped to [0,1]
	require.Equal(t, float32(0.0), scoreConfidence(0.1, false, 0, 3))
}

func TestHasContext(t *testing.T) {
	text := "Клиент Иванов Иван Иванович"
	require.True(t, hasContext(text, 7, 30, contextKeywords(TypeFIO)))
	require.False(t, hasContext("Александр Пушкин написал", 0, 20, contextKeywords(TypeFIO)))
}

func TestHasNegativeContext(t *testing.T) {
	text := "Банк находится по адресу Москва, ул. Тверская, 10"
	require.True(t, hasNegativeContext(text, 0, len(text), negativeContext(TypeAddress)))
}
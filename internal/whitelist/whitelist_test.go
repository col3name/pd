package whitelist_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/detector"
	"github.com/kind-earthquake/pii-module/internal/whitelist"
)

func TestBlocksKnownPerson(t *testing.T) {
	w := whitelist.New([]string{"Александр Пушкин", "Лев Толстой"}, nil, nil)
	s := detector.Span{Start: 0, End: 31, Type: detector.TypeFIO, Confidence: 0.9}
	require.True(t, w.Blocks("Александр Пушкин написал роман", s))
}

func TestCaseInsensitiveBlock(t *testing.T) {
	w := whitelist.New([]string{"Александр пушкин"}, nil, nil)
	s := detector.Span{Start: 0, End: 31, Type: detector.TypeFIO, Confidence: 0.9}
	require.True(t, w.Blocks("АЛЕКСАНДР ПУШКИН написал роман", s))
}

func TestDoesNotBlockUnknownPerson(t *testing.T) {
	w := whitelist.New([]string{"Лев Толстой"}, nil, nil)
	s := detector.Span{Start: 8, End: 30, Type: detector.TypeFIO, Confidence: 0.9}
	require.False(t, w.Blocks("Клиент Александр Петров", s))
}

func TestBlocksBankAddress(t *testing.T) {
	w := whitelist.New(nil, []string{"отделение Альфа-Банка"}, nil)
	s := detector.Span{Start: 16, End: 56, Type: detector.TypeAddress, Confidence: 0.85}
	require.True(t, w.Blocks("Справка: отделение Альфа-Банка на Тверской", s))
}
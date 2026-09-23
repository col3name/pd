package context_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/context"
	"github.com/kind-earthquake/pii-module/internal/detector"
)

func TestApplyBoostsFIO(t *testing.T) {
	r := context.New(context.DefaultBoost, context.DefaultPenalty)
	s := detector.Span{Start: 8, End: 30, Type: detector.TypeFIO, Confidence: 0.8}
	conf := r.Apply("Клиент: Иванов Иван Иванович", s)
	require.GreaterOrEqual(t, conf, float32(0.95), "клиент nearby must push FIO to mask threshold")
}

func TestApplyPenalizesBankAddress(t *testing.T) {
	r := context.New(context.DefaultBoost, context.DefaultPenalty)
	s := detector.Span{Start: 31, End: 55, Type: detector.TypeAddress, Confidence: 0.8}
	conf := r.Apply("Банк находится по адресу Москва, ул. Тверская, 10", s)
	require.Less(t, conf, float32(0.75), "bank keyword must push address below mask threshold")
}

func TestApplyClamps(t *testing.T) {
	r := context.New(context.DefaultBoost, context.DefaultPenalty)
	s := detector.Span{Start: 0, End: 8, Type: detector.TypeEmail, Confidence: 1.0}
	require.Equal(t, float32(1.0), r.Apply("a@b.ru", s))
}
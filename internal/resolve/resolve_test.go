package resolve_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/detector"
	"github.com/kind-earthquake/pii-module/internal/resolve"
)

func TestResolveCardOverINN(t *testing.T) {
	// Handcrafted overlapping spans: a 16-digit card region that also looks
	// like a 12-digit INN substring. Viewing "карта 4276123456789012", the
	// card rule covers [6:22] and a generic 12-digit rule could cover [6:18].
	// The resolver must keep the CARD span.
	spans := []detector.Span{
		{Start: 6, End: 22, Type: detector.TypeCard},
		{Start: 6, End: 18, Type: detector.TypeINN},
	}
	out := resolve.Resolve(spans, resolve.DefaultPriority)
	require.Len(t, out, 1)
	require.Equal(t, detector.TypeCard, out[0].Type)
	require.Equal(t, 6, out[0].Start)
	require.Equal(t, 22, out[0].End)
}

func TestResolveKeepsDisjointSpans(t *testing.T) {
	spans := []detector.Span{
		{Start: 0, End: 5, Type: detector.TypeEmail},
		{Start: 10, End: 20, Type: detector.TypePhone},
	}
	out := resolve.Resolve(spans, resolve.DefaultPriority)
	require.Len(t, out, 2)
	require.Equal(t, detector.TypeEmail, out[0].Type)
}

func TestPriorityConfigurable(t *testing.T) {
	spans := []detector.Span{
		{Start: 0, End: 16, Type: detector.TypeCard},
		{Start: 0, End: 12, Type: detector.TypeINN},
	}
	// Override: INN wins over CARD.
	out := resolve.Resolve(spans, map[detector.Type]int{detector.TypeINN: 100, detector.TypeCard: 50})
	require.Len(t, out, 1)
	require.Equal(t, detector.TypeINN, out[0].Type)
}

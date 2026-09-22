package detector

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveOverlaps(t *testing.T) {
	// Nested: passport (long) contains a date (short) — passport wins.
	spans := []Span{
		{Start: 10, End: 24, Type: TypePassport},
		{Start: 12, End: 20, Type: TypeBirthDate},
	}
	got := ResolveOverlaps(spans)
	require.Len(t, got, 1)
	require.Equal(t, TypePassport, got[0].Type)
	require.Equal(t, 10, got[0].Start)
	require.Equal(t, 24, got[0].End)
}

func TestResolveOverlapsDisjoint(t *testing.T) {
	spans := []Span{
		{Start: 0, End: 5, Type: TypeEmail},
		{Start: 10, End: 15, Type: TypePhone},
	}
	got := ResolveOverlaps(spans)
	require.Len(t, got, 2)
}

func TestResolveOverlapsSameLengthEarliest(t *testing.T) {
	spans := []Span{
		{Start: 5, End: 10, Type: TypePhone},
		{Start: 0, End: 5, Type: TypeEmail},
	}
	got := ResolveOverlaps(spans)
	require.Len(t, got, 2)
	require.Equal(t, TypeEmail, got[0].Type)
}

func TestSpanConfidenceField(t *testing.T) {
	s := Span{Start: 0, End: 5, Type: TypeEmail, Confidence: 0.98}
	require.Equal(t, float32(0.98), s.Confidence)
}

func TestResolveOverlapsSameLengthPriority(t *testing.T) {
	// Identical spans: passport and driver license both match "4509 123456".
	// Higher priority (passport) must win over earliest start.
	spans := []Span{
		{Start: 0, End: 11, Type: TypeDriverLicense, Priority: 1},
		{Start: 0, End: 11, Type: TypePassport, Priority: 2},
	}
	got := ResolveOverlaps(spans)
	require.Len(t, got, 1)
	require.Equal(t, TypePassport, got[0].Type)
}
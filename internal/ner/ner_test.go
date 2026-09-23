package ner

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/detector"
)

func TestMapLabel(t *testing.T) {
	typ, ok := mapLabel("B-PER")
	require.True(t, ok)
	require.Equal(t, detector.TypeFIO, typ)

	typ, ok = mapLabel("I-PER")
	require.True(t, ok)
	require.Equal(t, detector.TypeFIO, typ)

	typ, ok = mapLabel("B-LOC")
	require.True(t, ok)
	require.Equal(t, detector.TypeAddress, typ)

	_, ok = mapLabel("B-ORG")
	require.False(t, ok)

	_, ok = mapLabel("O")
	require.False(t, ok)
}

func TestSpansFromLabels(t *testing.T) {
	tk, err := NewTokenizer("testdata/vocab.txt")
	require.NoError(t, err)
	n := NewNER(tk, []string{"O", "B-PER", "I-PER", "B-LOC"})

	text := "Иван Иванов"
	tokens, err := tk.Encode(text)
	require.NoError(t, err)
	// tokens: [CLS] иван иванов [SEP]
	// label IDs: 0(O) 1(B-PER) 2(I-PER) 0(O)
	labelIDs := []int{0, 1, 2, 0}

	spans := n.SpansFromLabels(text, tokens, labelIDs)
	require.Len(t, spans, 1)
	require.Equal(t, detector.TypeFIO, spans[0].Type)
	require.Equal(t, 0, spans[0].Start)
	// "Иван Иванов" = 4×2 + 1 + 6×2 = 21 bytes.
	require.Equal(t, 21, spans[0].End)
}

func TestMapLabelConfigurable(t *testing.T) {
	// Default mapping: PER → ФИО.
	typ, ok := mapLabel("B-PER")
	require.True(t, ok)
	require.Equal(t, detector.TypeFIO, typ)

	// Custom mapping overrides the suffix map.
	custom := map[string]detector.Type{
		"PER": detector.TypeFIO,
		"LOC": detector.TypeAddress,
		"ORG": detector.TypeCardholder,
	}
	typ, ok = mapLabelWith("B-ORG", custom)
	require.True(t, ok)
	require.Equal(t, detector.TypeCardholder, typ)
	_, ok = mapLabelWith("B-ORG", nil)
	require.False(t, ok)
}
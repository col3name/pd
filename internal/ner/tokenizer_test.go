package ner

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTokenizerEncode(t *testing.T) {
	tk, err := NewTokenizer("testdata/vocab.txt")
	require.NoError(t, err)

	tokens, err := tk.Encode("Иван Иванов")
	require.NoError(t, err)
	// [CLS] + "иван" + "иванов" + [SEP]
	require.Len(t, tokens, 4)
	require.Equal(t, "[CLS]", tokens[0].Text)
	require.Equal(t, -1, tokens[0].Start)
	require.Equal(t, "иван", tokens[1].Text)
	require.Equal(t, 0, tokens[1].Start)
	require.Equal(t, "иванов", tokens[2].Text)
	// "Иван" = 4 Cyrillic chars × 2 bytes = 8 bytes, + 1 space = 9.
	require.Equal(t, 9, tokens[2].Start)
	require.Equal(t, "[SEP]", tokens[3].Text)
}

func TestTokenizerUnknownWord(t *testing.T) {
	tk, err := NewTokenizer("testdata/vocab.txt")
	require.NoError(t, err)

	// "петров" is not in vocab; should fall back to [UNK].
	tokens, err := tk.Encode("петров")
	require.NoError(t, err)
	require.Len(t, tokens, 3) // [CLS] + [UNK] + [SEP]
	require.Equal(t, "[UNK]", tokens[1].Text)
}
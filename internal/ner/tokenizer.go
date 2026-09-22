package ner

import (
	"bufio"
	"os"
	"strings"
)

// Token is a single WordPiece token with byte offsets into the original text.
// Special tokens ([CLS], [SEP], [UNK], [PAD]) have Start == -1.
type Token struct {
	ID    int
	Text  string
	Start int
	End   int
}

// Tokenizer performs WordPiece tokenization for a BERT-style model.
type Tokenizer struct {
	vocab map[string]int
}

// NewTokenizer loads a vocab.txt file (one token per line) into a Tokenizer.
func NewTokenizer(vocabPath string) (*Tokenizer, error) {
	f, err := os.Open(vocabPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	vocab := make(map[string]int)
	scanner := bufio.NewScanner(f)
	id := 0
	for scanner.Scan() {
		vocab[scanner.Text()] = id
		id++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return &Tokenizer{vocab: vocab}, nil
}

// Encode tokenizes text into WordPiece tokens, tracking byte offsets.
// It prepends [CLS] and appends [SEP].
func (t *Tokenizer) Encode(text string) ([]Token, error) {
	tokens := []Token{{ID: t.id("[CLS]"), Text: "[CLS]", Start: -1, End: -1}}
	for _, word := range splitWords(text) {
		tokens = append(tokens, t.tokenizeWord(word)...)
	}
	tokens = append(tokens, Token{ID: t.id("[SEP]"), Text: "[SEP]", Start: -1, End: -1})
	return tokens, nil
}

// splitWords splits text into words with byte offsets, preserving whitespace
// boundaries. Words are lowercased for matching.
func splitWords(text string) []word {
	var words []word
	start := -1
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == ' ' || c == '\t' || c == '\n' || c == ',' || c == '.' ||
			c == ':' || c == ';' || c == '(' || c == ')' || c == '-' {
			if start >= 0 {
				words = append(words, word{text: strings.ToLower(text[start:i]), start: start, end: i})
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		words = append(words, word{text: strings.ToLower(text[start:]), start: start, end: len(text)})
	}
	return words
}

type word struct {
	text  string
	start int
	end   int
}

// tokenizeWord applies greedy longest-match WordPiece to a single word.
func (t *Tokenizer) tokenizeWord(w word) []Token {
	if _, ok := t.vocab[w.text]; ok {
		return []Token{{ID: t.vocab[w.text], Text: w.text, Start: w.start, End: w.end}}
	}
	// Greedy longest-match with ## continuation.
	var tokens []Token
	remaining := w.text
	offset := w.start
	for remaining != "" {
		matched := false
		for end := len(remaining); end > 0; end-- {
			piece := remaining[:end]
			if offset > w.start {
				piece = "##" + piece
			}
			if id, ok := t.vocab[piece]; ok {
				tokens = append(tokens, Token{ID: id, Text: piece, Start: offset, End: offset + end})
				remaining = remaining[end:]
				offset += end
				matched = true
				break
			}
		}
		if !matched {
			// Unknown: emit [UNK] for the whole word.
			return []Token{{ID: t.id("[UNK]"), Text: "[UNK]", Start: w.start, End: w.end}}
		}
	}
	return tokens
}

func (t *Tokenizer) id(s string) int {
	if id, ok := t.vocab[s]; ok {
		return id
	}
	return -1
}
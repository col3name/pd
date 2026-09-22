package ner

import (
	"fmt"

	ort "github.com/yalue/onnxruntime_go"

	"github.com/kind-earthquake/pii-module/internal/detector"
)

// Model wraps the ONNX runtime and tokenizer for NER inference.
type Model struct {
	*NER
	session   *ort.AdvancedSession
	tokenizer *Tokenizer
	labels    []string
	maxLen    int
	input     *ort.Tensor[int64]
	mask      *ort.Tensor[int64]
	output    *ort.Tensor[float32]
}

// NewModel initializes the ONNX environment, loads the model and tokenizer.
// modelPath is the .onnx file; vocabPath is the vocab.txt file.
func NewModel(modelPath, vocabPath string, labels []string, maxLen int) (*Model, error) {
	tk, err := NewTokenizer(vocabPath)
	if err != nil {
		return nil, fmt.Errorf("load tokenizer: %w", err)
	}
	if err := ort.InitializeEnvironment(); err != nil {
		return nil, fmt.Errorf("init onnxruntime: %w", err)
	}
	shape := ort.NewShape(1, int64(maxLen))
	input, err := ort.NewTensor[int64](shape, make([]int64, maxLen))
	if err != nil {
		return nil, fmt.Errorf("create input tensor: %w", err)
	}
	mask, err := ort.NewTensor[int64](shape, make([]int64, maxLen))
	if err != nil {
		return nil, fmt.Errorf("create mask tensor: %w", err)
	}
	// Output shape: [1, maxLen, numLabels]. We use a flat buffer sized
	// maxLen * len(labels).
	output, err := ort.NewTensor[float32](ort.NewShape(1, int64(maxLen), int64(len(labels))), make([]float32, maxLen*len(labels)))
	if err != nil {
		return nil, fmt.Errorf("create output tensor: %w", err)
	}
	session, err := ort.NewAdvancedSession(modelPath,
		[]string{"input_ids", "attention_mask"},
		[]string{"logits"},
		[]ort.Value{input, mask},
		[]ort.Value{output},
		nil)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &Model{NER: NewNER(tk, labels), session: session, tokenizer: tk, labels: labels, maxLen: maxLen,
		input: input, mask: mask, output: output}, nil
}

// Detect tokenizes text, runs the model, and returns PII spans.
func (m *Model) Detect(text string) ([]detector.Span, error) {
	tokens, err := m.tokenizer.Encode(text)
	if err != nil {
		return nil, err
	}
	if len(tokens) > m.maxLen {
		tokens = tokens[:m.maxLen]
	}
	ids := m.input.GetData()
	mask := m.mask.GetData()
	for i := range ids {
		ids[i] = 0
		mask[i] = 0
	}
	for i, tok := range tokens {
		ids[i] = int64(tok.ID)
		mask[i] = 1
	}
	if err := m.session.Run(); err != nil {
		return nil, fmt.Errorf("run model: %w", err)
	}
	// Argmax over labels for each token.
	labelIDs := make([]int, len(tokens))
	data := m.output.GetData()
	for i := 0; i < len(tokens); i++ {
		best := 0
		bestVal := data[i*len(m.labels)]
		for j := 1; j < len(m.labels); j++ {
			v := data[i*len(m.labels)+j]
			if v > bestVal {
				bestVal = v
				best = j
			}
		}
		labelIDs[i] = best
	}
	return m.SpansFromLabels(text, tokens, labelIDs), nil
}

// Close releases the ONNX session and environment.
func (m *Model) Close() error {
	if m.session != nil {
		m.session.Destroy()
	}
	m.input.Destroy()
	m.mask.Destroy()
	m.output.Destroy()
	ort.DestroyEnvironment()
	return nil
}
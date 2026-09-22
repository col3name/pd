package ner

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestModelDetect is skipped unless ONNXRUNTIME_SHARED_LIBRARY_PATH and a model
// file are provided, since the model and runtime are not committed to the repo.
func TestModelDetect(t *testing.T) {
	libPath := os.Getenv("ONNXRUNTIME_SHARED_LIBRARY_PATH")
	modelPath := os.Getenv("NER_MODEL_PATH")
	if libPath == "" || modelPath == "" {
		t.Skip("set ONNXRUNTIME_SHARED_LIBRARY_PATH and NER_MODEL_PATH to run")
	}
	// This test requires a real rubert-tiny NER ONNX model. It is a smoke test
	// that the model loads and runs without error.
	m, err := NewModel(modelPath, "testdata/vocab.txt", []string{"O", "B-PER", "I-PER", "B-LOC"}, 128)
	require.NoError(t, err)
	defer m.Close()

	spans, err := m.Detect("Иван Иванов")
	require.NoError(t, err)
	// We don't assert specific spans (model-dependent); just that it ran.
	_ = spans
}
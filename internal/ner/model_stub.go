//go:build !cgo

package ner

import (
	"errors"

	"github.com/kind-earthquake/pii-module/internal/detector"
)

// errNoNERCGO is returned when CGO_ENABLED=0 disables the ONNX runtime.
var errNoNERCGO = errors.New("ner: not available, built with CGO_ENABLED=0")

// Model is a stub used when building with CGO_ENABLED=0 (e.g. the Docker
// image), where the ONNX runtime shared library is unavailable. NewModel
// always errors so main.go's graceful fallback ("continuing without NER")
// kicks in; all deterministic regex/context detection still works.
type Model struct{}

// NewModel is not supported in cgo-disabled builds.
func NewModel(modelPath, vocabPath string, labels []string, maxLen int) (*Model, error) {
	return nil, errNoNERCGO
}

// Detect implements detector.NERDetector (never reached; NewModel errors).
func (m *Model) Detect(text string) []detector.Span { return nil }

// Close implements the model teardown hook (never reached).
func (m *Model) Close() error { return nil }

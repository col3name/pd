#!/usr/bin/env bash
# Downloads the rubert-tiny NER ONNX model and vocab for the smart path.
# Usage: ./scripts/setup_ner.sh
set -euo pipefail

MODEL_DIR="models/ner"
mkdir -p "$MODEL_DIR"

echo "Downloading rubert-tiny NER ONNX model..."
# Replace with the actual model URL. The model is a rubert-tiny NER exported
# to ONNX. Place the .onnx file and vocab.txt in $MODEL_DIR.
# Example (placeholder — update to the real source):
# curl -L -o "$MODEL_DIR/model.onnx" "https://example.com/rubert-tiny-ner.onnx"
# curl -L -o "$MODEL_DIR/vocab.txt" "https://example.com/vocab.txt"

echo "Model files should be placed in $MODEL_DIR:"
echo "  $MODEL_DIR/model.onnx"
echo "  $MODEL_DIR/vocab.txt"
echo ""
echo "Also required: the onnxruntime shared library. Set"
echo "ONNXRUNTIME_SHARED_LIBRARY_PATH to its path before running the server."
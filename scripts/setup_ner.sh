#!/usr/bin/env bash
# Downloads the rubert-tiny NER ONNX model, vocab, and the ONNX Runtime
# shared library for the smart path.
#
# Usage:
#   ./scripts/setup_ner.sh                 # download model + vocab + onnxruntime
#   ./scripts/setup_ner.sh --skip-runtime  # only model + vocab (runtime already present)
#
# After running, build the image with NER enabled:
#   docker build --build-arg WITH_NER=1 -t pii-module-v2 .
#   docker compose up -d --build
set -euo pipefail

MODEL_DIR="models/ner"
mkdir -p "$MODEL_DIR"

# ── 1. Model + vocab ─────────────────────────────────────────────────────
# Source: HuggingFace rubert-tiny NER (ONNX export). Override with env vars.
MODEL_URL="${NER_MODEL_URL:-https://huggingface.co/onnx-community/ner-rubert-tiny-news-ONNX/resolve/main/onnx/model.onnx}"
VOCAB_URL="${NER_VOCAB_URL:-https://huggingface.co/onnx-community/ner-rubert-tiny-news-ONNX/resolve/main/vocab.txt}"

echo "Downloading rubert-tiny NER ONNX model..."
curl -L --fail -o "$MODEL_DIR/model.onnx" "$MODEL_URL"
echo "Downloading vocab.txt..."
curl -L --fail -o "$MODEL_DIR/vocab.txt" "$VOCAB_URL"

echo "Model files:"
echo "  $MODEL_DIR/model.onnx"
echo "  $MODEL_DIR/vocab.txt"

# ── 2. ONNX Runtime shared library ──────────────────────────────────────
if [ "${1:-}" = "--skip-runtime" ]; then
  echo "Skipping ONNX Runtime download (--skip-runtime)."
else
  echo "Downloading ONNX Runtime shared library..."
  # ONNX Runtime CPU release. Override with ONNXRUNTIME_URL.
  ORT_URL="${ONNXRUNTIME_URL:-https://github.com/microsoft/onnxruntime/releases/download/v1.17.1/onnxruntime-linux-x64-1.17.1.tgz}"
  ORT_TGZ="$MODEL_DIR/onnxruntime.tgz"
  curl -L --fail -o "$ORT_TGZ" "$ORT_URL"
  tar -xzf "$ORT_TGZ" -C "$MODEL_DIR"
  rm -f "$ORT_TGZ"
  echo "ONNX Runtime extracted to $MODEL_DIR/onnxruntime-linux-x64-1.17.1/"
fi

echo ""
echo "Done. Next steps:"
echo "  1. docker build --build-arg WITH_NER=1 -t pii-module-v2 ."
echo "  2. docker compose up -d --build"
echo ""
echo "The container mounts ./models into /srv/models (see docker-compose.yml)."
echo "Config: ml.enabled=true, ml.model_path=/srv/models/ner/model.onnx,"
echo "        ml.vocab_path=/srv/models/ner/vocab.txt"
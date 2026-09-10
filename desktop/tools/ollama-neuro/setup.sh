#!/usr/bin/env bash
# Install deps + pull heredos/rwkv7:2.9b for local Neuro API testing.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

MODEL="${NEURO_MOCK_MODEL:-heredos/rwkv7:2.9b}"
OLLAMA_URL="${OLLAMA_HOST:-http://127.0.0.1:11434}"

echo "=== neuro-desktop · ollama-neuro setup ==="
echo "Model: $MODEL"
echo "Ollama: $OLLAMA_URL"
echo

if ! command -v ollama >/dev/null 2>&1; then
  echo "Ollama CLI not found."
  echo "Install from https://ollama.com (Linux/macOS/Windows), then re-run this script."
  exit 1
fi

if ! curl -sf "$OLLAMA_URL/api/tags" >/dev/null; then
  echo "Ollama API not reachable at $OLLAMA_URL"
  echo "Start it with: ollama serve"
  exit 1
fi

echo "[1/3] Python venv + dependencies"
python3 -m venv .venv
# shellcheck disable=SC1091
source .venv/bin/activate
pip install --upgrade pip
pip install -r requirements.txt

echo "[2/3] Pulling $MODEL (RWKV-7 · BlinkDL lineage via heredos — fast architecture, still slow cold-start on CPU laptops)"
ollama pull "$MODEL"

echo "[3/3] Smoke-check chat endpoint (may take several minutes on CPU while the model loads)"
python - <<PY
import json, urllib.request
url = "${OLLAMA_URL}/api/chat"
body = json.dumps({
  "model": "${MODEL}",
  "messages": [{"role": "user", "content": "Reply with exactly: ok"}],
  "stream": False,
  "keep_alive": "30m",
  "options": {"num_predict": 8, "num_ctx": 1024},
}).encode()
req = urllib.request.Request(url, data=body, headers={"Content-Type": "application/json"})
with urllib.request.urlopen(req, timeout=600) as resp:
    data = json.load(resp)
print("model reply:", (data.get("message") or {}).get("content", "")[:80])
print("setup ok — model should stay warm for ~30m")
PY

echo
echo "Done. On slow CPUs (e.g. Latitude E7490), prefer:"
echo "  ./run.sh --mode ollama --warm --keep-alive -1"
echo "For instant IPC tests without waiting on the LLM:"
echo "  ./run.sh --mode manual"
echo "Then open http://127.0.0.1:1337/ and connect neuro-integration to ws://127.0.0.1:8000"

#!/usr/bin/env bash
# Cross-platform build entrypoint for Linux / macOS / Git Bash / WSL.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

OS_UNAME="$(uname -s | tr '[:upper:]' '[:lower:]')"
BIN_EXT=""
case "$OS_UNAME" in
  mingw*|msys*|cygwin*) BIN_EXT=".exe" ;;
esac

echo "=== neuro-desktop build-all ($OS_UNAME) ==="

echo "[1/4] Go integration"
mkdir -p apps/neuro-integration/dist
(
  cd apps/neuro-integration
  go build -o "dist/neuro-integration${BIN_EXT}" .
)

echo "[2/4] Rust orchestrator"
(
  cd apps/neuro-desktop
  cargo build --release
)

echo "[3/4] Frontend"
(
  cd frontend
  if [[ -f package-lock.json ]]; then
    npm ci
  else
    npm install
  fi
  npm run build
)

echo "[4/4] Process handler (optional)"
if command -v cmake >/dev/null 2>&1; then
  cmake -S apps/process-handler -B apps/process-handler/build -DBUILD_TESTS=OFF
  cmake --build apps/process-handler/build --config Release --parallel
else
  echo "  cmake not found — skipping process-handler"
fi

echo
echo "Build complete."
echo "  Bundle + run (dev):  ./scripts/bundle/dev.sh"
echo "  Rust binary:         apps/neuro-desktop/target/release/neuro-desktop${BIN_EXT}"
echo "  Go binary:           apps/neuro-integration/dist/neuro-integration${BIN_EXT}"

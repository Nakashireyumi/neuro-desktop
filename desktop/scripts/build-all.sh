#!/usr/bin/env bash
# Build everything the shipped product needs on Linux / macOS / Git Bash / WSL.
#
# The shipped product is three programs and a web page:
#   * the Go server (apps/neuro-integration) — talks to Neuro, enforces the
#     permission policy, hosts the relay and the dashboard API;
#   * the Go dashboard program (apps/neuro-dashboard) — serves the dashboard and
#     forwards its API to the server (optional; for split setups);
#   * the Python agent (backend/python/controller) — the only process that
#     touches the machine Neuro controls (runs on the PC being controlled;
#     scripts/build-client.sh packages it as the neuro-client program);
#   * the TypeScript dashboard (frontend), which the server serves at /ui/.
#
#   ./scripts/build-all.sh            # build + test
#   ./scripts/build-all.sh --no-test  # build only
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

RUN_TESTS=1
for arg in "$@"; do
  case "$arg" in
    --no-test|--skip-tests) RUN_TESTS=0 ;;
    -h|--help) sed -n '2,12p' "$0"; exit 0 ;;
    *) echo "unknown argument: $arg" >&2; exit 2 ;;
  esac
done

OS_UNAME="$(uname -s | tr '[:upper:]' '[:lower:]')"
BIN_EXT=""
case "$OS_UNAME" in
  mingw*|msys*|cygwin*) BIN_EXT=".exe" ;;
esac

echo "=== neuro-desktop build ($OS_UNAME) ==="

echo "[1/5] Server (apps/neuro-integration)"
(
  cd apps/neuro-integration
  if [[ "$RUN_TESTS" == "1" ]]; then
    go vet ./...
    go test -count=1 ./...
  fi
  mkdir -p dist
  go build -o "dist/neuro-integration${BIN_EXT}" .
)
echo "      -> apps/neuro-integration/dist/neuro-integration${BIN_EXT}"

echo "[2/5] Dashboard program (apps/neuro-dashboard)"
(
  cd apps/neuro-dashboard
  if [[ "$RUN_TESTS" == "1" ]]; then
    go vet ./...
    go test -count=1 ./...
  fi
  mkdir -p dist
  go build -o "dist/neuro-dashboard${BIN_EXT}" .
)
echo "      -> apps/neuro-dashboard/dist/neuro-dashboard${BIN_EXT}"

echo "[3/5] Dashboard (frontend)"
(
  cd frontend
  if [[ -f package-lock.json ]]; then
    npm ci
  else
    npm install
  fi
  if [[ "$RUN_TESTS" == "1" ]]; then npm test; fi
  npm run build
)
echo "      -> frontend/dist"

echo "[4/5] Agent (backend/python/controller)"
(
  cd backend/python
  python3 -m compileall -q controller >/dev/null
  if [[ "$RUN_TESTS" == "1" ]]; then
    NEURO_HEADLESS=1 python3 -m unittest discover -s tests -t . >/dev/null
  fi
)
if [[ "$RUN_TESTS" == "1" ]]; then
  echo "      -> backend/python/controller (syntax checked, tests run)"
else
  echo "      -> backend/python/controller (syntax checked)"
fi

echo "[+] Go client, slice 1 (apps/neuro-client-go). Not shipped yet: the Python client is"
echo "    the one in the bundle. This checks the port that is under way."
(
  cd apps/neuro-client-go
  if [[ "$RUN_TESTS" == "1" ]]; then
    go vet ./...
    go test -count=1 ./...
  fi
  mkdir -p dist
  go build -o "dist/neuro-client-go${BIN_EXT}" .
)
echo "      -> apps/neuro-client-go/dist/neuro-client-go${BIN_EXT}"

echo
echo "[5/5] Native manager (GTK 4)"
if [[ "$RUN_TESTS" == "1" ]]; then
  PYTHONPATH=apps/neuro-manager python3 -m unittest discover -s apps/neuro-manager/tests
fi
python3 scripts/build-manager.py --out dist/manager-source

echo "Build complete."
echo "  Run from source:      ./scripts/bundle/dev.sh"
echo "  Release bundle:       ./scripts/bundle/prod.sh"
echo "  Server binary:        apps/neuro-integration/dist/neuro-integration${BIN_EXT}"
echo "  Dashboard program:    apps/neuro-dashboard/dist/neuro-dashboard${BIN_EXT}"
echo "  neuro-client (PC):    scripts/build-client.sh   (PyInstaller; build on the PC's OS)"
echo "  Agent (on the PC):    python3 -m controller.agent --bridge <server>:9876   (cwd: backend/python)"

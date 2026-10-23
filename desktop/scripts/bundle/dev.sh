#!/usr/bin/env bash
# Development bundle: build the server and the dashboard program, stage the agent
# and config, then run the whole thing from one directory — no Rust, no sudo.
# The agent runs from source here; neuro-client (PyInstaller) is for release bundles.
#
#   ./scripts/bundle/dev.sh
#   NEURO_BUNDLE_NO_LAUNCH=1 ./scripts/bundle/dev.sh   # stage only
#
# Staged layout (desktop/dist/dev):
#   neuro-integration      the server: Neuro API client + dashboard API + hub
#   neuro-dashboard        the dashboard program (optional; split setups)
#   agent/controller/      the agent that executes commands on this machine
#   frontend/              the dashboard client, served at /ui/
#   catalog/, config/, permissions.json
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

echo "=== Development Bundle ==="

# Never run as root: it creates root-owned files that break later non-sudo builds
# and strips X11/Wayland auth so the agent cannot open the display.
if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
  echo "ERROR: do not run this script with sudo." >&2
  echo "  sudo leaves root-owned files under dist/ and frontend/dist," >&2
  echo "  and removes the display authorisation the agent needs." >&2
  exit 1
fi

OS_UNAME="$(uname -s | tr '[:upper:]' '[:lower:]')"
BIN_EXT=""
case "$OS_UNAME" in
  mingw*|msys*|cygwin*) BIN_EXT=".exe" ;;
esac

DIST="dist/dev"
SERVER="neuro-integration$BIN_EXT"

echo "Detected OS: $OS_UNAME"

# A leftover from an old sudo run cannot be cleaned up here; say so instead of
# failing with a confusing "permission denied" halfway through.
if [[ -e "$DIST" && ! -w "$DIST" ]]; then
  echo "ERROR: $DIST is not writable (root-owned from a prior sudo run)." >&2
  echo "  Fix ownership, then re-run as your user:" >&2
  echo "    sudo chown -R \"\$USER\" dist frontend/dist" >&2
  exit 1
fi

echo "Updating $DIST (keeping tokens, permissions and installed extensions)..."
mkdir -p "$DIST"

echo "Building the server (../headless/apps/neuro-integration)..."
(
  cd ../headless/apps/neuro-integration
  mkdir -p dist
  # -tags neurodev: only development bundles accept NEURO_EXTENSIONS_ALLOW_UNSIGNED.
  go build -tags neurodev -o "dist/$SERVER" .
)
cp "../headless/apps/neuro-integration/dist/$SERVER" "$DIST/$SERVER"

echo "Building the dashboard program (../headless/apps/neuro-dashboard)..."
(
  cd ../headless/apps/neuro-dashboard
  mkdir -p dist
  go build -o "dist/neuro-dashboard$BIN_EXT" .
)
cp "../headless/apps/neuro-dashboard/dist/neuro-dashboard$BIN_EXT" "$DIST/"

echo "Building the dashboard client (frontend)..."
(
  cd frontend
  if [[ -f package-lock.json ]]; then npm ci; else npm install; fi
  npm run build
)
rm -rf "$DIST/frontend"
mkdir -p "$DIST/frontend"
cp -r frontend/dist/. "$DIST/frontend/"

echo "Staging the agent (python/controller)..."
python3 scripts/build-manager.py --out "$DIST/manager"
rm -rf "$DIST/agent"
mkdir -p "$DIST/agent"
cp -r python/controller "$DIST/agent/controller"
rm -rf "$DIST/agent/controller/__pycache__"
cp python/requirements.txt "$DIST/agent/requirements.txt"
cp python/requirements-windows.txt "$DIST/agent/requirements-windows.txt" 2>/dev/null || true

echo "Staging config, catalog and docs..."
mkdir -p "$DIST/integration-docs"
mkdir -p "$DIST/config"
cp -Rn config/. "$DIST/config/"
mkdir -p "$DIST/catalog"
for item in catalog/*; do
  name="$(basename "$item")"
  case "$name" in extensions-state.json|extension-permissions.json|integrations.json|builtin-services.json) continue ;; esac
  if [[ -d "$item" ]]; then
    mkdir -p "$DIST/catalog/$name"
    cp -Rn "$item/." "$DIST/catalog/$name/"
  else
    cp "$item" "$DIST/catalog/"
  fi
done
MARKETPLACE_INDEX="${NEURO_MARKETPLACE_SOURCE:-$ROOT/catalog/marketplace.json}"
if [[ -f "$MARKETPLACE_INDEX" ]]; then cp "$MARKETPLACE_INDEX" "$DIST/catalog/marketplace.json"; fi
if [[ ! -f "$DIST/permissions.json" ]]; then
  cp ../headless/apps/neuro-integration/permissions.example.json "$DIST/permissions.json"
fi
cp "../headless/apps/neuro-integration/integration-docs/Action Script Documentation.md" \
   "$DIST/integration-docs/" 2>/dev/null || true

sed "s|@SERVER@|$SERVER|g" scripts/bundle/templates/start.sh > "$DIST/start.sh"
chmod +x "$DIST/start.sh"

echo
echo "=== Dev bundle complete ==="
echo "Location: $DIST"
echo
echo "1. Start the server and local client together:"
echo "     cd $DIST && ./start.sh --ws-url ws://localhost:8000"
echo "   setup prints the dashboard token once: copy it. It is the sign-in token."
echo
echo "2. For a remote client, use NEURO_NO_AGENT=1 ./start.sh on the server, then:"
echo "     cd $DIST/agent && python3 -m controller.agent --bridge 127.0.0.1:9876"
echo
echo "   Split machines: run step 1 with --executor-listen 0.0.0.0:9876 and set"
echo "   NEURO_EXECUTOR_TOKEN on both sides, then point the agent at the server's IP."
echo "   The executor link is plain TCP for now: on an untrusted network, tunnel it over SSH."
echo "   The relay host and the server on this machine share ./relay-token, so nothing is copied."
echo "   A relay on another machine needs the same token value: copy the file's value there."
echo
echo "3. The native manager opens automatically (GTK 4 + PyGObject required for source builds)."
echo "   Standalone manager: from the repository root, python3 desktop/scripts/build-manager.py --standalone"
echo "   Optional browser dashboard: http://127.0.0.1:8300/ui/"
echo "   Split: ./neuro-dashboard --server http://<server>:8300 --listen 127.0.0.1:8310"
echo "   serves the same UI from another program and forwards only the API."
echo

if [[ "${NEURO_BUNDLE_NO_LAUNCH:-0}" == "1" ]]; then
  echo "Skipping launch (NEURO_BUNDLE_NO_LAUNCH=1)"
  exit 0
fi

echo "Launching the server and local client (Ctrl-C to stop)..."
if [[ -z "${NEURO_AGENT_PYTHON:-}" && -x "$ROOT/python/.venv/bin/python" ]]; then
  export NEURO_AGENT_PYTHON="$ROOT/python/.venv/bin/python"
fi
cd "$DIST"
chmod +x "$SERVER"
exec ./start.sh

#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

echo "=== Building Neuro Desktop Bundle ==="

# --------------------------------------------------
# OS DETECTION
# --------------------------------------------------
OS_UNAME="$(uname -s | tr '[:upper:]' '[:lower:]')"

IS_WINDOWS=false
IS_WSL=false

if [[ "$OS_UNAME" == mingw* || "$OS_UNAME" == msys* || "$OS_UNAME" == cygwin* ]]; then
  IS_WINDOWS=true
elif grep -qi microsoft /proc/version 2>/dev/null; then
  IS_WSL=true
fi

if $IS_WINDOWS; then
  BIN_EXT=".exe"
else
  BIN_EXT=""
fi

echo "Detected OS: $OS_UNAME"
$IS_WINDOWS && echo "→ Windows mode"
$IS_WSL && echo "→ WSL mode"

# --------------------------------------------------
# PATHS
# --------------------------------------------------
DIST="dist/neuro-desktop"
PY_DIST="$DIST/agent"

SERVER="neuro-integration$BIN_EXT"

# --------------------------------------------------
# CLEAN
# --------------------------------------------------
# Rebuilding must not erase credentials, the operator's policy, extensions, or
# other bundles under dist/. Replace generated assets only.
mkdir -p "$DIST/"
rm -rf "$DIST/frontend" "$DIST/agent" "$DIST/integration-docs"

# Releases include the native runtime. Source staging is explicit for CI/dev
# machines without GTK; it never silently substitutes a browser application.
if [[ "${NEURO_MANAGER_SOURCE_ONLY:-0}" == "1" ]]; then
  python3 scripts/build-manager.py --out "$DIST/manager"
else
  python3 scripts/build-manager.py --standalone --out "$DIST/manager"
fi

# --------------------------------------------------
# ABOUT THE SHIPPED LAYOUT (three programs; see docs/ARCHITECTURE.md)
# --------------------------------------------------
#   $SERVER           the server: Neuro API client, dashboard API, executor hub,
#                     vision. Runs on the machine with the power to spare.
#   neuro-dashboard   the dashboard program: serves frontend/ and forwards the
#                     API to the server. Can run on another machine.
#   neuro-client/     the program for the PC Neuro controls: the agent as one
#                     executable (only if it could be built here; see below).
#   agent/            the Python agent. The fallback when neuro-client/ is absent.
#   frontend/         the dashboard client (the server also serves it at /ui/)
#   config/, catalog/ policy example, game profiles, docs
# The Rust executor and the C++ supervisor are not shipped (see
# docs/ARCHITECTURE.md).

# --------------------------------------------------
# BUILD GO PROGRAMS
# --------------------------------------------------
echo "[1/3] Building the server and the dashboard program (Go)..."

mkdir -p ../headless/apps/neuro-integration/dist
pushd ../headless/apps/neuro-integration > /dev/null
# No -tags neurodev: a release build has no unsigned-extension escape hatch.
go build -o "dist/$SERVER" .
popd > /dev/null

cp "../headless/apps/neuro-integration/dist/$SERVER" "$DIST/"

DASHBOARD="neuro-dashboard$BIN_EXT"
mkdir -p ../headless/apps/neuro-dashboard/dist
pushd ../headless/apps/neuro-dashboard > /dev/null
go build -o "dist/$DASHBOARD" .
popd > /dev/null
cp "../headless/apps/neuro-dashboard/dist/$DASHBOARD" "$DIST/"

mkdir -p "$DIST/integration-docs"
cp \
  "../headless/apps/neuro-integration/integration-docs/Action Script Documentation.md" \
  "$DIST/integration-docs/Action Script Documentation.md"

echo "      ✓ server built"

# --------------------------------------------------
# BUILD FRONTEND
# --------------------------------------------------
echo "[2/3] Building the dashboard client (frontend)..."
pushd frontend > /dev/null
if [[ -f package-lock.json ]]; then npm ci; else npm install; fi
npm run build
popd > /dev/null

mkdir -p "$DIST/frontend"
cp -r frontend/dist/* "$DIST/frontend/"
echo "      ✓ dashboard client built"

# --------------------------------------------------
# COPY CONFIG / CATALOG / DOCS
# --------------------------------------------------
echo "Copying configuration, catalog and docs..."
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
cp ../headless/apps/neuro-integration/integration-docs/action-schema.run_script.json "$DIST/integration-docs/" 2>/dev/null || true
cp docs/*.md "$DIST/integration-docs/" 2>/dev/null || true
echo "  ✓ config, catalog and docs copied"

# --------------------------------------------------
# PYTHON VENV DETECTION
# --------------------------------------------------
echo "[3/3] Bundling the Python agent..."
mkdir -p "$PY_DIST"

# The agent is plain Python; only its third-party drivers (pyautogui & friends)
# need installing. A vendored runtime is a convenience, not a requirement — a
# build machine without Python headers or without network must still produce a
# usable bundle, so a failure here degrades to "install the requirements on the
# target machine" instead of aborting the release.
bundle_python_runtime() {
  local venv_base="python/.venv"
  local req_file="python/requirements.txt"

  python3 -m venv "$venv_base" || return 1
  # shellcheck disable=SC1091
  source "$venv_base/bin/activate" || return 1
  if [[ -f "$req_file" ]]; then
    echo "Installing/updating dependencies from $req_file..."
    pip install --quiet --upgrade pip || true
    pip install --quiet -r "$req_file" || return 1
  else
    echo "Warning: $req_file not found; shipping without a vendored runtime."
  fi
  deactivate

  if [[ -d "$venv_base/Lib" ]]; then
    PY_LIB_SRC="$venv_base/Lib"                 # Windows layout
  elif [[ -d "$venv_base/lib" ]]; then
    local site
    site=$(find "$venv_base/lib" -maxdepth 1 -type d -name "python*" | head -n 1)
    [[ -n "$site" ]] || return 1
    PY_LIB_SRC="$site/site-packages"            # Unix layout
  else
    return 1
  fi
  return 0
}

VENDORED_RUNTIME=1
if [[ "${NEURO_BUNDLE_SKIP_VENV:-0}" == "1" ]]; then
  echo "  ! NEURO_BUNDLE_SKIP_VENV=1 — shipping the agent source only"
  VENDORED_RUNTIME=0
elif ! bundle_python_runtime; then
  VENDORED_RUNTIME=0
  echo "  ! Could not build a vendored Python runtime (missing Python headers or no network)."
  echo "    The bundle still runs with the system python3; the agent needs:"
  echo "      python3 -m pip install -r agent/requirements.txt"
fi

if [[ "$VENDORED_RUNTIME" == "1" ]]; then
  cp -r "$PY_LIB_SRC" "$PY_DIST/Lib"
fi

cp -r python/controller "$PY_DIST/controller"
# Byte-compiled caches are not reproducible and only bloat the archive.
find "$PY_DIST/controller" -name __pycache__ -type d -prune -exec rm -rf {} + 2>/dev/null || true
cp python/requirements.txt "$PY_DIST/requirements.txt"
cp python/requirements-windows.txt "$PY_DIST/requirements-windows.txt" 2>/dev/null || true

echo "      ✓ agent bundled"

# neuro-client: the agent packaged as one executable, for the PC Neuro controls,
# so that machine needs no Python install. PyInstaller builds it on this machine
# (it cannot cross-compile), and on Linux the build needs the Python headers.
# When it fails, the bundle still works: the launcher falls back to agent/.
if [[ "${NEURO_BUNDLE_SKIP_CLIENT:-0}" == "1" ]]; then
  echo "  ! NEURO_BUNDLE_SKIP_CLIENT=1: no neuro-client in this bundle"
elif NEURO_CLIENT_OUT="$PWD/$DIST/neuro-client" scripts/build-client.sh; then
  echo "      ✓ neuro-client built"
else
  echo "  ! neuro-client was not built (see the message above). The bundle uses agent/."
fi

# --------------------------------------------------
# METADATA + LAUNCHERS
# --------------------------------------------------
# Templates rather than heredocs: they are checked in, so `bash -n` and the
# Windows launcher can be reviewed like any other code.
TEMPLATES="scripts/bundle/templates"
sed "s|@SERVER@|$SERVER|g" "$TEMPLATES/README.txt" > "$DIST/README.txt"
sed "s|@SERVER@|$SERVER|g" "$TEMPLATES/start.sh"  > "$DIST/start.sh"
sed "s|@SERVER@|$SERVER|g" "$TEMPLATES/start.bat" > "$DIST/start.bat"
chmod +x "$DIST/start.sh"

chmod +x "$DIST/$SERVER" || true

# --------------------------------------------------
# DONE
# --------------------------------------------------
echo
echo "=== Bundle complete ==="
echo "Location: $DIST"
echo
echo "Files included:"
find "$DIST" -type f | sed "s|^$DIST/|  - |"
echo
echo "To test:"
$IS_WINDOWS && echo "  cd $DIST && start.bat"
! $IS_WINDOWS && echo "  cd $DIST && ./start.sh"

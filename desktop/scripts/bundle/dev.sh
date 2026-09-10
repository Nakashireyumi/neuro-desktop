#!/usr/bin/env bash
set -euo pipefail

echo "=== Development Bundle ==="

# Never run as root — it creates root-owned files that break later non-sudo builds
# and strips X11/Wayland auth so the executor cannot open the display.
if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
  echo "ERROR: do not run this script with sudo."
  echo "  Past sudo runs leave root-owned files under apps/neuro-desktop/target/,"
  echo "  frontend/dist/, and dist/ — which breaks non-sudo builds."
  echo "  Fix ownership, then re-run as your user:"
  echo "    sudo chown -R \"\$USER:\$USER\" apps/neuro-desktop/target backend/python/.venv frontend/dist dist"
  exit 1
fi

# Detect leftover root-owned dirs early (common after a sudo experiment)
for sticky in frontend/dist dist apps/neuro-desktop/target/release/frontend apps/neuro-desktop/target/release/config; do
  if [[ -e "$sticky" ]] && [[ ! -w "$sticky" ]]; then
    echo "ERROR: $sticky is not writable (often root-owned from a prior sudo run)."
    echo "  Fix with:"
    echo "    sudo chown -R \"\$USER:\$USER\" frontend/dist dist apps/neuro-desktop/target"
    exit 1
  fi
done

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

BIN_EXT=""
$IS_WINDOWS && BIN_EXT=".exe"

RUST_BIN="neuro-desktop$BIN_EXT"
GO_BIN="neuro-integration$BIN_EXT"

echo "Detected OS: $OS_UNAME"
$IS_WINDOWS && echo "→ Windows mode"
$IS_WSL && echo "→ WSL mode"

# --------------------------------------------------
# PATHS
# --------------------------------------------------
DIST="apps/neuro-desktop/target/release"
PY_DIST="$DIST/python"

# --------------------------------------------------
# BUILD RUST (so $DIST exists)
# --------------------------------------------------
echo "Building Rust executor..."
(
  cd apps/neuro-desktop
  cargo build --release
)

# --------------------------------------------------
# CLEAN DEV ASSETS (tolerate prior root-owned leftovers)
# --------------------------------------------------
safe_rm() {
  local path="$1"
  if [[ ! -e "$path" ]]; then
    return 0
  fi
  if rm -rf "$path" 2>/dev/null; then
    return 0
  fi
  echo "ERROR: cannot remove $path (permission denied)."
  echo "  Likely owned by root from a previous sudo bundle. Fix with:"
  echo "    sudo chown -R \"\$USER:\$USER\" apps/neuro-desktop/target"
  exit 1
}

safe_rm "$PY_DIST"
safe_rm "$DIST/frontend"
safe_rm "$DIST/config"

# --------------------------------------------------
# BUILD GO BRIDGE
# --------------------------------------------------
echo "Building Neuro integration (bridge)..."

mkdir -p apps/neuro-integration/dist
pushd apps/neuro-integration > /dev/null
go build -o "dist/$GO_BIN" .
popd > /dev/null

cp "apps/neuro-integration/dist/$GO_BIN" "$DIST/$GO_BIN"

mkdir -p "$DIST/integration-docs"
cp \
  "apps/neuro-integration/integration-docs/Action Script Documentation.md" \
  "$DIST/integration-docs/Action Script Documentation.md"

# Default permissions example beside binaries
if [[ -f config/permissions.example.json ]]; then
  cp config/permissions.example.json "$DIST/permissions.json"
fi

echo "  ✓ Neuro Integration binary copied to $DIST"

# --------------------------------------------------
# BUILD FRONTEND
# --------------------------------------------------
echo "Building frontend..."
pushd frontend > /dev/null
npm install
npm run build
popd > /dev/null

mkdir -p "$DIST/frontend"
cp -r frontend/dist/* "$DIST/frontend/"

# --------------------------------------------------
# COPY CONFIG
# --------------------------------------------------
echo "Copying configuration files..."
cp -r config "$DIST/config"
echo "  ✓ Config files copied"

# --------------------------------------------------
# PYTHON VENV
# --------------------------------------------------
echo "Bundling Python files and libraries..."

mkdir -p "$PY_DIST"

VENV_BASE="backend/python/.venv"
REQ_FILE="backend/python/requirements.txt"
REQ_WIN="backend/python/requirements-windows.txt"
python3 -m venv "$VENV_BASE"
# shellcheck disable=SC1091
source "$VENV_BASE/bin/activate"

if [ -f "$REQ_FILE" ]; then
    echo "Installing/updating dependencies from $REQ_FILE..."
    pip install --upgrade pip
    if $IS_WINDOWS && [ -f "$REQ_WIN" ]; then
      pip install -r "$REQ_WIN"
    else
      pip install -r "$REQ_FILE"
    fi
else
    echo "Warning: $REQ_FILE not found. Skipping installation."
fi

if [[ -d "$VENV_BASE/Lib" ]]; then
  PY_LIB_SRC="$VENV_BASE/Lib"
elif [[ -d "$VENV_BASE/lib" ]]; then
  PY_SITE=$(find "$VENV_BASE/lib" -maxdepth 1 -type d -name "python*" | head -n 1)
  if [[ -z "$PY_SITE" ]]; then
    echo "❌ Could not find python site-packages"
    exit 1
  fi
  PY_LIB_SRC="$PY_SITE/site-packages"
else
  echo "❌ Python venv not found"
  exit 1
fi

cp -r "$PY_LIB_SRC" "$PY_DIST/Lib"
cp -r backend/python/controller "$PY_DIST/controller"

echo
echo "=== Dev bundle complete ==="
echo "Run from: $DIST"
echo
echo "Co-located (default):"
echo "  ./$RUST_BIN"
echo
echo "Split machines:"
echo "  # On Neuro / operator PC (bridge):"
echo "  ./$GO_BIN --ws-url ws://localhost:8000 --executor-listen 0.0.0.0:9876"
echo "  # On the PC Neuro should control (executor):"
echo "  ./$RUST_BIN --executor --server <bridge-ip>:9876"
echo

# --------------------------------------------------
# LAUNCH (optional — skip with NEURO_BUNDLE_NO_LAUNCH=1)
# --------------------------------------------------
if [[ "${NEURO_BUNDLE_NO_LAUNCH:-0}" == "1" ]]; then
  echo "Skipping launch (NEURO_BUNDLE_NO_LAUNCH=1)"
  exit 0
fi

pushd "$DIST" > /dev/null
chmod +x "$RUST_BIN" 2>/dev/null || true
chmod +x "$GO_BIN" 2>/dev/null || true
./"$RUST_BIN"
popd > /dev/null

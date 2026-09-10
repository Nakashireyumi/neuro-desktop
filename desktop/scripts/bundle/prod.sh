#!/usr/bin/env bash
set -euo pipefail

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
PY_DIST="$DIST/python"

RUST_BIN="neuro-desktop$BIN_EXT"
GO_BIN="neuro-integration$BIN_EXT"

# --------------------------------------------------
# CLEAN
# --------------------------------------------------
rm -rf dist
mkdir -p "$DIST/"

# --------------------------------------------------
# BUILD RUST
# --------------------------------------------------
echo "[1/4] Building Rust application..."
pushd apps/neuro-desktop > /dev/null
cargo build --release
popd > /dev/null

cp "apps/neuro-desktop/target/release/$RUST_BIN" "$DIST/"
echo "      ✓ Rust binary built"

# --------------------------------------------------
# BUILD GO INTEGRATION
# --------------------------------------------------
echo "[2/4] Building Neuro integration..."

mkdir -p apps/neuro-integration/dist
pushd apps/neuro-integration > /dev/null
go build -o "dist/$GO_BIN" .
popd > /dev/null

cp "apps/neuro-integration/dist/$GO_BIN" "$DIST/"

mkdir -p "$DIST/integration-docs"
cp \
  "apps/neuro-integration/integration-docs/Action Script Documentation.md" \
  "$DIST/integration-docs/Action Script Documentation.md"

echo "      ✓ Neuro Integration binary built"

# --------------------------------------------------
# BUILD FRONTEND
# --------------------------------------------------
echo "[3/4] Building frontend..."
pushd frontend > /dev/null
npm install # install the npm dependencies before building.
npm run build
popd > /dev/null

mkdir -p "$DIST/frontend"
cp -r frontend/dist/* "$DIST/frontend/"
echo "      ✓ Frontend built"

# --------------------------------------------------
# COPY CONFIG
# --------------------------------------------------
echo "Copying configuration files..."
cp -r config "$DIST/config"
echo "  ✓ Config files copied"

# --------------------------------------------------
# PYTHON VENV DETECTION
# --------------------------------------------------
echo "[4/4] Bundling Python runtime..."
mkdir -p "$PY_DIST"

VENV_BASE="backend/python/.venv"
REQ_FILE="backend/python/requirements.txt"
python -m venv "$VENV_BASE"
source "$VENV_BASE/bin/activate"

if [ -f "$REQ_FILE" ]; then
    echo "Installing/updating dependencies from $REQ_FILE..."
    pip install --upgrade pip
    pip install -r "$REQ_FILE"
else
    echo "Warning: $REQ_FILE not found. Skipping installation."
fi

if [[ -d "$VENV_BASE/Lib" ]]; then
  # Windows venv
  PY_LIB_SRC="$VENV_BASE/Lib"
elif [[ -d "$VENV_BASE/lib" ]]; then
  # Unix venv — find pythonX.Y
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

echo "      ✓ Python runtime bundled"

# --------------------------------------------------
# METADATA
# --------------------------------------------------
cat > "$DIST/README.txt" << EOF
Neuro Desktop Control System
=============================

This is a self-contained bundle of Neuro Desktop.

Contents:
  - $RUST_BIN              Main application (Rust)
  - $GO_BIN                Neuro API connector (Go)
  - python/                Python runtime and drivers
  - frontend/              Web UI assets

To run:
  - Windows: start.bat
  - Unix: ./start.sh

Environment Variables:
  - NEURO_SDK_WS_URL (default ws://localhost:8000)
  - NEURO_IPC_FILE   (default ./neuro_ipc.json)

The Go integration binary is launched automatically.
EOF

# --------------------------------------------------
# LAUNCHERS
# --------------------------------------------------
if $IS_WINDOWS; then
  cat > "$DIST/start.bat" << EOF
@echo off
echo Starting Neuro Desktop...
$RUST_BIN
pause
EOF
else
  cat > "$DIST/start.sh" << EOF
#!/usr/bin/env bash
echo "Starting Neuro Desktop..."
./$RUST_BIN
read -p "Press Enter to exit..."
EOF
  chmod +x "$DIST/start.sh"
fi

chmod +x "$DIST/$RUST_BIN" || true
chmod +x "$DIST/$GO_BIN" || true

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

#!/usr/bin/env bash
# Builds neuro-client: the program that runs on the PC Neuro controls. It is the
# controller agent packaged as one executable, with its own Python inside, so the
# controlled PC needs no Python install and runs nothing else (no dashboard, no
# vision, no policy).
#
#   ./scripts/build-client.sh                  # -> desktop/dist/neuro-client/neuro-client
#   NEURO_CLIENT_OUT=/some/dir ./scripts/build-client.sh
#
# PyInstaller does not cross-compile: build on the OS you ship for.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"      # the desktop/ folder
cd "$ROOT"

PYINSTALLER_VERSION="6.22.3"
OUT="${NEURO_CLIENT_OUT:-$ROOT/dist/neuro-client}"
WORK="$ROOT/dist/.client-build"                              # under dist/, which git ignores
VENV="$WORK/venv"

if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
  echo "ERROR: do not run this as root; it leaves root-owned files under dist/." >&2
  exit 1
fi

mkdir -p "$WORK"
if [[ ! -x "$VENV/bin/python" ]]; then
  python3 -m venv "$VENV"
fi
"$VENV/bin/python" -m pip install --quiet --upgrade pip
if ! "$VENV/bin/python" -m pip install --quiet -r backend/python/requirements.txt "pyinstaller==$PYINSTALLER_VERSION"; then
  echo "ERROR: installing the build dependencies failed." >&2
  echo "On Linux, pynput depends on evdev, which has no prebuilt wheel and needs the" >&2
  echo "Python development headers (Python.h). Install them, then re-run this script:" >&2
  echo "  Debian/Ubuntu: sudo apt install python3-dev" >&2
  exit 1
fi

rm -rf "$WORK/pyi" "$OUT"
mkdir -p "$WORK/pyi" "$OUT"
# Input and screenshots use PyAutoGUI, MSS and Pillow. We do not use PyAutoGUI's
# optional image matching; collecting ambient OpenCV/NumPy can also pull in
# hundreds of megabytes of video, Qt and VTK libraries from a build machine.
"$VENV/bin/pyinstaller" --noconfirm --onefile --clean \
  --name neuro-client \
  --paths backend/python \
  --collect-submodules pynput \
  --exclude-module cv2 --exclude-module numpy \
  --distpath "$OUT" \
  --workpath "$WORK/pyi/build" \
  --specpath "$WORK/pyi" \
  apps/neuro-client/neuro_client.py

echo "built: $OUT/neuro-client"
"$OUT/neuro-client" --help >/dev/null && echo "smoke: --help runs"

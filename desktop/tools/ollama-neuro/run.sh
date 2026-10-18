#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

if [[ ! -d .venv ]]; then
  echo "Missing .venv — run ./setup.sh first"
  exit 1
fi

# shellcheck disable=SC1091
source .venv/bin/activate

exec python server.py "$@"

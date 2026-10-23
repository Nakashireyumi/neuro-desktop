#!/usr/bin/env bash
# Create the workspace Python environment and install frontend dependencies.
# Safe to re-run; it does not write secrets or start any server.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

if [ ! -x .venv/bin/python ]; then
    python3 -m venv --system-site-packages .venv
fi

.venv/bin/python -m pip install --upgrade pip
.venv/bin/pip install -r desktop/python/requirements.txt

npm ci --prefix desktop/frontend

cat <<'EOF'
Devcontainer ready.
  Python controller: PYTHONPATH=desktop/python .venv/bin/python -m controller.agent --help
  Go:                go test ./... (from headless/apps/neuro-integration)
  Frontend:          npm --prefix desktop/frontend run build
  GUI tests:         xvfb-run -a env NEURO_MANAGER_GUI_TESTS=1 PYTHONPATH=desktop/apps/neuro-manager .venv/bin/python -m unittest discover -s desktop/apps/neuro-manager/tests
EOF

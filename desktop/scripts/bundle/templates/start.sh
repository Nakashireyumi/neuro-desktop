#!/usr/bin/env bash
# Neuro Desktop launcher (bundled). Installed as start.sh.
#
# Starts the native manager, server and local agent. The server also serves a
# browser dashboard at http://127.0.0.1:8300/ui/ and
# talks to Neuro, and starts the agent on this PC unless this is a split-machine
# setup. The agent is neuro-client/ when the bundle has it, else the Python agent
# in agent/ (needs python3 with agent/requirements.txt installed).
#
# Split machine (see README.txt):
#   server:  NEURO_NO_AGENT=1 ./start.sh --executor-listen 0.0.0.0:9876
#   the PC Neuro controls: copy the neuro-client folder there and run
#     ./neuro-client --bridge <server-ip>:9876 --token <executor token>
set -euo pipefail

cd "$(dirname "$0")"

export NEURO_UI_DIR="$PWD/frontend"
export NEURO_IPC_FILE="$PWD/neuro_ipc.json"
export NEURO_PERMISSIONS_FILE="$PWD/permissions.json"
export NEURO_CATALOG_FILE="$PWD/catalog/index.json"
export NEURO_GAME_PROFILES_DIR="$PWD/catalog/games"
export NEURO_EXTENSIONS_STATE_FILE="$PWD/catalog/extensions-state.json"
export PYTHONPATH="$PWD/agent:$PWD/agent/Lib${PYTHONPATH:+:$PYTHONPATH}"

SERVER_BIN="$PWD/@SERVER@"
if [[ ! -x "$SERVER_BIN" ]]; then
  echo "Cannot find the server binary at $SERVER_BIN" >&2
  exit 1
fi

# First-run step. It is idempotent: it prints the dashboard token only the first
# time, and a failed check stops the launch here with the reason printed.
"$SERVER_BIN" setup

echo "Dashboard: http://127.0.0.1:8300/ui/ (sign in with the dashboard token)"

CHILD_PIDS=()
cleanup() {
  for pid in "${CHILD_PIDS[@]}"; do kill "$pid" 2>/dev/null || true; done
}
trap cleanup EXIT
if [[ "${NEURO_NO_MANAGER:-0}" != "1" && -x "$PWD/manager/neuro-manager" ]]; then
  "$PWD/manager/neuro-manager" --server "${NEURO_MANAGER_SERVER:-http://127.0.0.1:8300}" &
  CHILD_PIDS+=("$!")
fi

# The agent on this PC. neuro-client needs no Python install, so it is preferred.
AGENT_BIN="$PWD/neuro-client/neuro-client"
AGENT_CMD=()
if [[ -x "$AGENT_BIN" ]]; then
  AGENT_CMD=("$AGENT_BIN")
elif [[ -n "${NEURO_AGENT_PYTHON:-}" ]]; then
  AGENT_CMD=("$NEURO_AGENT_PYTHON" -m controller.agent)
elif command -v python3 >/dev/null 2>&1; then
  AGENT_CMD=(python3 -m controller.agent)
fi

if [[ "${NEURO_NO_AGENT:-0}" != "1" && ${#AGENT_CMD[@]} -gt 0 ]]; then
  "${AGENT_CMD[@]}" --bridge "${NEURO_AGENT_BRIDGE:-127.0.0.1:9876}" &
  CHILD_PIDS+=("$!")
  # The agent must not outlive the server, or the next start finds the port busy.
else
  echo "Agent not started here. On the PC Neuro should control:"
  echo "  copy neuro-client/ there, then run: ./neuro-client --bridge <server-ip>:9876 --token <executor token>"
  echo "  (no neuro-client in this bundle: cd agent && python3 -m controller.agent --bridge <server-ip>:9876)"
fi

# Not exec: the EXIT trap above must still run when the server stops.
status=0
"$SERVER_BIN" "$@" || status=$?
exit "$status"

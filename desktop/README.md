# Neuro Desktop — desktop control for Neuro-sama

Neuro Desktop lets Neuro (or Evil) use a real computer: move the mouse, press
keys, run scripts and shell commands, play games that have no integration of
their own, and read the desktop back as context. Vedal/you get a dashboard to
watch it, grant permissions, and pause or kill anything at any time.

## The three programs

```
┌─────────────────────────────┐
│  Neuro API (ws://…)         │
└──────────┬──────────────────┘
           │ neuro-sdk (Go)
┌──────────▼───────────────────────────────────────────────────────────┐
│  SERVER — desktop/apps/neuro-integration (Go)                        │
│  • talks to Neuro (actions, context, results)                        │
│  • serves the dashboard API and the dashboard page at /ui/           │
│  • owns the permission policy, audit log, kill switch, game catalog  │
│  • vision client; forwards machine commands to the client            │
└──────────┬───────────────────────────────────────────────────────────┘
           │ executor protocol (JSON lines) or file IPC
┌──────────▼───────────────────────────────────────────────────────────┐
│  CLIENT (neuro-client) — desktop/backend/python/controller (Python)  │
│  • runs on the PC Neuro controls (usually the same machine)          │
│  • mouse/keyboard via pyautogui, screen capture, windows, shell      │
│  • built as one program with PyInstaller: no Python needed there     │
│  • refuses input on a headless machine and says why                  │
└──────────────────────────────────────────────────────────────────────┘

Dashboard page (your browser) ──HTTP /ui/──► the server, or the DASHBOARD PROGRAM
(`neuro-dashboard`, optional, on your own PC), which forwards only /api and /health.
```

The server executes *decisions*; the client executes *actions*. The dashboard
is a client of the server. See `docs/ARCHITECTURE.md` for why the project is split
into these programs (and why the client is not rewritten in Go yet), and
`docs/EXECUTOR_PROTOCOL.md` for the wire protocol between the server and the client.

## Quick start

```bash
# 1. Server (on the PC that runs Neuro). Set up once: this prints the dashboard token.
cd desktop/apps/neuro-integration
go build -o neuro-integration .
./neuro-integration setup
NEURO_SDK_WS_URL=ws://127.0.0.1:8000 ./neuro-integration

# 2. Client (on the PC Neuro should control — same machine in dev)
cd desktop/backend/python
python3 -m controller.agent --bridge 127.0.0.1:9876
#    or the built program: ./scripts/build-client.sh, then dist/neuro-client/neuro-client --bridge 127.0.0.1:9876

# 3. Dashboard: sign in with the token from step 1
xdg-open http://127.0.0.1:8300/ui/      # macOS: open, Windows: start
#    or on your own PC (the server's admin port reached over SSH):
#    cd apps/neuro-dashboard && go build -o neuro-dashboard .
#    ./neuro-dashboard --server http://127.0.0.1:8300 --listen 127.0.0.1:8310
```

`setup` creates the relay token (`relay-token`, shared by the relay host and the
server) and the dashboard token (`dashboard-token`, which keeps only its hash). It
is safe to run again, and `setup --check` changes nothing. The bundle's `start.sh`
and `start.bat` run it on every start.

Everything can also be built and staged in one step:

```bash
./scripts/build-all.sh            # server + dashboard program + dashboard page + client syntax check
./scripts/bundle/dev.sh           # stages dist/dev and launches the server
NEURO_BUNDLE_SKIP_VENV=1 ./scripts/bundle/prod.sh   # release bundle in dist/neuro-desktop
./scripts/build-client.sh         # neuro-client: the client as one program (PyInstaller)
```

The bundle's `start.sh` / `start.bat` launch the server and the local client
together (`neuro-client/` when the bundle has it, else the Python agent);
`NEURO_NO_AGENT=1` skips the client for a split-machine setup.

On Linux/macOS, the development bundle also provides `dist/dev/start.sh` and
starts the local client automatically. Rebuilding either shell bundle preserves
tokens, `permissions.json`, installed extensions, and custom game profiles.
The development script uses `backend/python/.venv/bin/python` when it exists;
`NEURO_AGENT_PYTHON=/path/to/python` selects another client interpreter.
For a desktop client from source, install its drivers once:

```bash
python3 -m venv backend/python/.venv
backend/python/.venv/bin/python -m pip install -r backend/python/requirements.txt
./scripts/bundle/dev.sh
```

With `NEURO_HEADLESS=1`, the client needs only Python's standard library.
On supported Wayland compositors (including Hyprland), install `grim` for native
screenshots; the client uses MSS on X11, Windows, and macOS. Missing GUI drivers
and capture failures are reported in telemetry without stopping the client.
The dashboard exposes Pause / Resume and Release all input on every tab. Its
Activity tab reads `NEURO_AUDIT_LOG`; the Neuro API connection has its own runtime
card, so an available dashboard does not imply Neuro is connected.

## Layout

```
desktop/
├── apps/
│   ├── neuro-integration/    SERVER (Go): Neuro client, dashboard API, executor hub, policy,
│   │                         relay host (`relay` subcommand), MCP bridge, signed catalog
│   ├── neuro-dashboard/      DASHBOARD PROGRAM (Go, stdlib only): serves the page, forwards /api and /health
│   ├── neuro-client/         entry point for the PyInstaller build of the CLIENT
│   └── nd-vision-server/     optional vision service (Python, stdlib HTTP; Pillow optional)
├── backend/python/
│   ├── controller/           CLIENT / AGENT + drivers (agent.py, actions.py, controls/, shell.py)
│   └── tests/                unittest suite for the agent and the safety rules
├── frontend/                 DASHBOARD PAGE (React + TypeScript + Vite)
├── catalog/                  game profiles, catalog index, live extension state
├── config/                   example policy (no config file: settings are env vars)
├── scripts/                  build-all.*, build-go.ps1, build-client.*, bundle/{dev,prod}.*, templates/
├── tools/
│   ├── ci/repo_checks.py     dependency-free repository checks (what CI runs first)
│   ├── fake-executor/        protocol simulator for the server (no input is ever generated)
│   └── ollama-neuro/         local small-model brain for testing the action surface
└── docs/ (repo root docs/)   ARCHITECTURE, EXECUTOR_PROTOCOL, LLM_GUIDE, SAFETY, CAPABILITIES
```

## Configuration

| Variable | What it does |
| --- | --- |
| `NEURO_SDK_WS_URL` / `-ws-url` | Neuro API websocket (`ws://localhost:8000` by default) |
| `NEURO_ADMIN_LISTEN` | dashboard address (`127.0.0.1:8300`) |
| `NEURO_DASHBOARD_LISTEN` / `--listen` | **dashboard program only**: its address (`127.0.0.1:8310`). A non-loopback address prints a warning |
| `NEURO_DASHBOARD_SERVER` / `--server` | **dashboard program only**: the server it forwards `/api` and `/health` to (`http://127.0.0.1:8300`). Plain HTTP to another machine prints a warning |
| `NEURO_UI_DIR` / `--ui-dir` | **dashboard program only**: the built page folder (it contains `index.html`); found next to the program when empty |
| `NEURO_ADMIN_TOKEN` | optional override of the dashboard token; at least 16 characters, and it must equal the token `setup` printed. Every `/api` route needs the token, loopback included |
| `NEURO_DASHBOARD_TOKEN_FILE` | where the dashboard token's SHA-256 hash is kept (`./dashboard-token`, mode 0600). `setup` writes it |
| `NEURO_EXECUTOR_LISTEN` / `-executor-listen` | hub address clients (agents) connect to (`127.0.0.1:9876`) |
| `NEURO_EXECUTOR_TOKEN` | shared secret every agent must present. Required before the hub listens beyond loopback. It is sent in plain TCP (see `docs/SAFETY.md`, section 6) |
| `NEURO_IPC_FILE` | file-IPC fallback path for a co-located agent |
| `NEURO_PERMISSIONS_FILE` | policy file (see `config/permissions.example.json`) |
| `NEURO_HEADLESS` | `1`/`true` on a machine with no display session |
| `NEURO_SHELL_ALLOWLIST` | programs the shell action may run (`ls,echo,python3`) |
| `NEURO_DENY_ACTIONS` | actions that never run (`type_text`) |
| `NEURO_AUDIT_LOG` | JSON-lines audit file |
| `NEURO_RELAY_ENABLED` / `NEURO_RELAY_URL` | connect the server to a relay as an integration (`neuro-integration relay`, or the upstream relay) |
| `NEURO_RELAY_TOKEN` | optional. The server's relay token comes from the relay token file; if this is set it must equal that file, or the link stays off. For the upstream relay, point `NEURO_RELAY_TOKEN_FILE` at a file holding its `intermediary.auth_token` |
| `NEURO_RELAY_TOKEN_FILE` | the relay token shared by the relay host and the server (`./relay-token`, mode 0600). `setup` creates it |
| `NEURO_RELAY_LISTEN` / `NEURO_RELAY_AUTH_TOKEN` | the relay host's socket, and an optional token override for the host (it must equal the file) |
| `NEURO_RELAY_HEALTH_LISTEN` / `NEURO_RELAY_NEURO_URL` / `NEURO_RELAY_NEURO_OS_TOKEN` | the relay host's health endpoint, the optional Neuro link for `direct_to_neuro`, and the enhanced watcher token |
| `NEURO_VISION_URL` / `NEURO_VISION_TOKEN` | the vision service `game_observe` calls (`NEURO_VISION_SERVER_URL` is still read as an alias) |
| `NEURO_CATALOG_FILE` / `NEURO_PUBLISHERS_FILE` | the signed extension index and the publisher keys the server trusts |
| `NEURO_EXTENSION_DIR` / `NEURO_EXTENSION_INSTALL_MODE` | where installed extensions live, and `metadata_only` (default) or `git_clone` |
| `NEURO_EXTENSIONS_ALLOW_UNSIGNED` | **dangerous, development builds only** (`go build -tags neurodev`, which `dev.sh` uses): install extensions whose signature does not verify. A release build ignores it and logs once that it is ignored. Leave it unset |
| `NEURO_GAME_PROFILES_DIR`, `NEURO_CATALOG_FILE` | game profiles and catalog index |
| `NEURO_RELAY_*` | optional Neuro Relay participation (see `docs/RELAY.md`) |
| `NEURO_VISION_URL` | vision server used by `game_observe` |

## Actions

`GET /api/actions` lists every action with its `kind`, its JSON `schema`, and a
plain-language `params` hint (for example `direction` with the allowed values,
which params are required). The same schemas are registered with Neuro.
`docs/LLM_GUIDE.md` explains how to prompt a small model so it uses them
correctly; `docs/CAPABILITIES.md` is the full feature list.

Roughly: mouse/keyboard primitives, script and shell execution, desktop shell
intents (`open_windows_menu`, `show_desktop`, …), catalog and extension
management, telemetry (`get_status`, `get_desktop_context`), and the high-level
game interface (`game_list_profiles`, `game_start_session`, `game_move`, …).

## Safety

Every action goes through the same pipeline: kill switch and pause state, deny
list, permission policy (per-scope budgets and explicit consent for `shell` and
`system`), rate limit, the shell firewall — all logged to the audit file, and all
flippable live from the dashboard. `docs/SAFETY.md` lists each guard, where it
lives, and how to verify it.

## Headless / command-line-only machines

Set `NEURO_HEADLESS=1` (or just have no `DISPLAY`/`WAYLAND_DISPLAY`). Input
actions then fail fast with an explanation, `get_status` reports `headless:
true`, and the shell capability becomes the useful surface — still bounded by
`NEURO_SHELL_ALLOWLIST`, `NEURO_SHELL_DENY_PATTERNS` and the 20-second default
timeout.

## Testing

```bash
# server (Go)
cd desktop/apps/neuro-integration && go test -race ./...    # -race needs cgo (CI runs it on Linux)
cd desktop/apps/neuro-integration && go test -race -tags neurodev ./...   # the dev-only switch's tests

# dashboard program (Go, stdlib only): the proxy rules, the sign-in pass-through, 502 when the server is down
cd desktop/apps/neuro-dashboard && go vet ./... && go test -count=1 ./...

# agent + safety rules (Python, no third-party deps needed)
cd desktop/backend/python && python3 -m unittest discover -s tests -t .

# the client as one program (Linux/macOS; Windows: scripts/build-client.ps1). Needs Python headers on Linux
./scripts/build-client.sh && ./dist/neuro-client/neuro-client --help

# everything CI runs first: JSON/JSONC, policy parity, catalog, docs, file sizes,
# stale references, the ollama brain tests
python3 desktop/tools/ci/repo_checks.py

# dashboard client
cd desktop/frontend && npm run build
```

`desktop/tools/fake-executor/fake_executor.py` speaks the agent protocol and
never generates input, so the server can be exercised (and the dashboard
explored) on a machine with no display and without touching anything.

## Troubleshooting

| Symptom | Cause / fix |
| --- | --- |
| `/api/status` shows `executor.connected: false` | No agent connected. Start `python3 -m controller.agent --bridge 127.0.0.1:9876`, or use `NEURO_IPC_FILE` file IPC. |
| `executor.replaced_connections` climbing | Two agents are running against one hub; the newer one wins. Kill the extra process. |
| Actions fail with "no display session" | The agent is on a machine without a GUI session. Use `shell_command`, or run the agent on the desktop PC. |
| `relay rejected the registration` | The token must match on both sides. The server reads the relay token file that `setup` writes. The relay host reads the same file (or `NEURO_RELAY_AUTH_TOKEN`). For the upstream relay, it is `intermediary.auth_token`. The relay's log says `invalid auth token`. |
| `relay link disabled: NEURO_RELAY_TOKEN differs from the relay token file` | Unset `NEURO_RELAY_TOKEN`, or copy the file's value into it. Run `setup --check` to see every mismatch. |
| `no relay token: run neuro-integration setup` | No relay token file exists yet. Run `neuro-integration setup`. |
| Shell action says the command is not allow-listed | Add the program to `NEURO_SHELL_ALLOWLIST`; the output names the pattern that blocked it. |
| Dashboard answers `503`: "the dashboard has no token yet" | Run `neuro-integration setup`, then sign in with the token it prints. |
| Dashboard answers `503` with "does not match the dashboard token" | `NEURO_ADMIN_TOKEN` disagrees with the stored token. Unset it, or set it to the printed token. |
| Dashboard answers `401` | The token is wrong or missing. Send it in the `X-ND-Token` header or as a Bearer token. A `?token=` query parameter is not read. |
| Dashboard program answers `502` | It is running, but the server at `--server` did not answer. Start the server, or fix the address (see `docs/TROUBLESHOOTING.md`) |
| `scripts/build-client.sh` fails on Linux with `Python.h` or `evdev` | `pynput` needs `evdev`, which builds from source. Install the Python development headers (`sudo apt install python3-dev`) and run it again |

## Removed

Several components were deleted when the server and the agent were consolidated into Go and Python: the Rust executor, the C++ process handler, the Python relay shim, the `native/` placeholders, and the root integration scripts that could not run. Nothing in the shipped path uses them, and `tools/ci/repo_checks.py` fails if a reference to one of them returns. The list and the reasons are in `CHANGELOG.md`.

The manager is a separate native GTK 4 application with its own startup splash.
Build it with `python3 scripts/build-manager.py --standalone`, or use the development
bundle with GTK and PyGObject installed. The server also provides an optional browser
dashboard. See [manager setup and resource use](apps/neuro-manager/README.md).

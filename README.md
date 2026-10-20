# Neuro Desktop

> **An AI-powered desktop control system that gives Neuro-sama the ability to control a computer through natural language commands.**

[![License: GPL-3.0](https://img.shields.io/badge/License-GPL--3.0-blue.svg)](LICENSE)
[![CI](https://github.com/cassitly/neuro-desktop/actions/workflows/ci.yml/badge.svg)](https://github.com/cassitly/neuro-desktop/actions/workflows/ci.yml)
[![Version](https://img.shields.io/badge/version-0.2.0--dev-blue.svg)]()

## Table of Contents

- [Overview](#overview)
- [Features](#features)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Installation](#installation)
- [Usage](#usage)
- [Development](#development)
- [Documentation](#documentation)
- [Contributing](#contributing)
- [License](#license)

## Overview

Neuro Desktop is a multi-language integration system that enables [Neuro-sama](https://twitch.tv/vedal987) to interact with desktop environments through a sophisticated action scripting language. The system bridges AI decision-making with real-world computer control through a carefully designed architecture that prioritizes safety, reliability, and human-like interaction patterns.

### What Makes Neuro Desktop Special?

- **Separate native manager, server and controlled-PC client**: the GTK 4 manager uses native widgets and a startup splash; the Go server handles the Neuro API, permissions, audit, relay and vision; the Python client executes actions. An optional browser dashboard supports remote use. See [architecture](docs/ARCHITECTURE.md) and [native manager setup](desktop/apps/neuro-manager/README.md).
- **Human-Like Mouse Movement**: Advanced algorithmic pathfinding that mimics natural human mouse movements with Bézier curves and Perlin noise
- **Powerful Script Language**: Simple yet expressive action scripting for complex automation tasks
- **Automatic Recovery**: Built-in crash detection and automatic process restart capabilities
- **Cross-Platform**: Windows, Linux, and macOS — high-level intents resolve to OS-native shortcuts
- **Operator Controls**: Vedal can gate capabilities via scoped permission policies

## Features

See **[docs/CAPABILITIES.md](docs/CAPABILITIES.md)** for an honest “what works today” list
(bridge/executor, actions, permissions, platforms, and what’s still stubbed).

### Core Capabilities

- Mouse / keyboard control with human-like pathfinding
- Action script language for multi-step workflows
- Bridge ↔ executor over TCP (same PC or remote controlled machine)
- Scoped permission policies for Vedal / operators
- Cross-platform intents (Windows-solid; Linux/macOS best-effort)
- Local Ollama+RWKV7 Neuro API mock for integration testing

## Architecture

Neuro Desktop has a native manager, server, controlled-PC client and optional web
dashboard. Per the
[Neuro SDK](https://github.com/VedalAI/neuro-sdk), the server is a WebSocket
**client** of Neuro's API server. Internally:

- **Native manager** — `neuro-manager` (GTK 4) runs independently, shows its own
  startup splash and uses the server's authenticated management API. It can run
  on the same PC or connect from your administration desktop.
- **Server** — `neuro-integration` (Go) talks to Neuro, owns the permission
  policy, audit log, game catalog, relay link and vision client, serves the dashboard,
  and listens for clients on TCP `:9876` (or uses file IPC on the same machine).
- **Dashboard program** — `neuro-dashboard` (Go, optional) serves the dashboard page on
  the PC you sit at and forwards only its API to the server. It holds no secret and adds
  no token of its own.
- **Client** — `neuro-client`, the Python agent packaged as one program, runs on the PC
  Neuro controls and is the only process that touches that machine. It carries out what
  the server sends, and nothing else. From source it is
  `python3 -m controller.agent --bridge host:9876`.
- **Dashboard page** — the TypeScript frontend, served by the server at `/ui/` and by the
  dashboard program.

The split exists so that the PC Neuro controls does not need to be powerful.

```
┌──────────────────┐     Neuro WS      ┌─────────────────────────────┐
│  Neuro API       │◄─────────────────►│  SERVER (Go)                │
│  (Vedal)         │                   │  dashboard API :8300/ui     │
└──────────────────┘                   │  executor hub :9876         │
                                       └──────┬───────────────┬──────┘
                       dashboard API (token)  │               │ JSON-lines
┌──────────────────┐                  ┌───────▼───────┐ ┌─────▼─────────────────┐
│  DASHBOARD PAGE  │◄── forwards ─────┤ DASHBOARD     │ │  CLIENT (neuro-client)│
│  in your browser │                  │ PROGRAM (opt.)│ │  on the PC Neuro      │
└──────────────────┘                  └───────────────┘ │  controls: input,     │
                                                        │  screen, shell        │
                                                        └───────────────────────┘
```

`docs/ARCHITECTURE.md` explains the consolidation and the split (Rust and C++ are no longer on
the shipping path), `docs/EXECUTOR_PROTOCOL.md` is the server↔client wire format,
and `docs/RELAY.md` covers coexisting with other integrations.

### Split-machine quick start

```bash
# PC that runs Neuro (server). Set up once: setup prints the dashboard token ONCE.
./neuro-integration setup

# The hub refuses to listen beyond loopback without a token. Use a long random value.
export NEURO_EXECUTOR_TOKEN="$(openssl rand -hex 24)"
./neuro-integration --ws-url ws://localhost:8000 --executor-listen 0.0.0.0:9876

# PC Neuro should control (client) — graphical session, NO sudo.
# Give it the same NEURO_EXECUTOR_TOKEN value. Built with desktop/scripts/build-client.sh, it is
# one program that needs no Python on this PC (copy the neuro-client folder over):
./neuro-client --bridge <server-lan-ip>:9876 --token "$NEURO_EXECUTOR_TOKEN"
# From source instead (Python and the requirements needed):
# cd desktop/python && python3 -m controller.agent --bridge <server-lan-ip>:9876 --token "$NEURO_EXECUTOR_TOKEN"
```

The executor token and the agent's commands cross the network in plain TCP for now.
Use this only on a network you trust. Or keep the hub on loopback and tunnel it:
run `ssh -L 9876:127.0.0.1:9876 user@<server>` on the agent PC and point the agent at
`127.0.0.1:9876`. Encrypting this link is an open item in
[docs/PRODUCTION_TODO.md](docs/PRODUCTION_TODO.md).

The same machine is the default: run the agent with no arguments (loopback hub)
or point `NEURO_IPC_FILE` at a shared path for file IPC.

### Operator manager and dashboard

The bundle opens the native GTK manager. Its **Marketplace** tab connects SDK
adapters and offers reviewed source listings; **Extensions** manages the signed
runtime catalog. Follow the [manager setup](desktop/apps/neuro-manager/README.md)
to approve an integration, create a scoped token, or give Neuro/Evil a restricted
manager sign-in.

The bridge serves the dashboard itself — no separate server, no `file://` page:

```
http://127.0.0.1:8300/ui/
```

- **Extensions** — install / enable / disable / uninstall from the catalog
  (`desktop/catalog/index.json`), with the install mode (metadata, git clone, MCP).
- **Marketplace** — reviewed, commit-pinned game/application source and connected
  SDK adapters. Source downloads still need the upstream installation steps.
- **Games** — profiles found on this machine, the game detected right now, the live
  session (mode, actions issued), "show me what Neuro sees", and a
  **release all input** panic button.
- **Permissions** — scopes (input, game, filesystem, process, network, system,
  vision), per-scope actions-per-minute limits, and explicit allow/deny lists.
  `Save` applies the policy to the running bridge immediately.
- **Status** — version, executor connection, relay peers, reserved actions, paths.
- **Operator controls** — pause/resume and release held input from every tab.
  The Activity tab shows recent action decisions when `NEURO_AUDIT_LOG` is set.
  Neuro API connectivity is reported separately from the server and client.

Every dashboard request needs the dashboard token, reads included. Run
`neuro-integration setup` once. It prints the token and keeps only its hash. The
sign-in page asks for the token, and the browser keeps it for that browser session
only. The token is never written into a page or a URL. There is no exception for
loopback: a script on this machine sends the token too, in the `X-ND-Token` header:

```bash
curl -s -H "X-ND-Token: <dashboard token>" http://127.0.0.1:8300/api/status
```

Only `/health` answers without the token. A server that has not been set up answers
`503` and says to run `setup`.

Try it without a display, a Windows box, or pyautogui — the repo ships a protocol
simulator that never touches your real mouse or keyboard:

```bash
# Terminal 1: pretend to be the executor (reports "Minecraft" as the active window)
python3 desktop/tools/fake-executor/fake_executor.py --addr 127.0.0.1:9876

# Terminal 2: the bridge + dashboard (set up once; sign in with the printed token)
cd headless/apps/neuro-integration
go run . setup
NEURO_UI_DIR=../../desktop/frontend/dist go run .
```

### Playing games

Neuro Desktop can play a game two ways, and picks the right one per game:

1. **Alongside a dedicated integration** (Minecraft, osu!, ...). The game keeps its
   own integration; Neuro Desktop does not register the actions that integration
   owns (`NEURO_RESERVED_ACTIONS`, relay peers) and refuses to send input while the
   session is delegated (`control.mode: external`).
2. **On its own**, for anything without an integration: Neuro Desktop supplies the
   high-level interface (`game_list_profiles`, `game_detect`, `game_start_session`,
   `game_move`, `game_look`, `game_action`, `game_press`, `game_observe`, ...), using
   the profile's keybinds and masked keys.

Profiles live in `desktop/catalog/games/*.json` (see
[the profile README](desktop/catalog/games/README.md)); ship one profile per game:

```json
{
  "id": "minecraft",
  "name": "Minecraft",
  "match": { "window_titles": ["Minecraft"], "processes": ["javaw"] },
  "control": { "mode": "auto", "external_integration": "minecraft",
               "mouse_look": { "enabled": true }, "move_hold_seconds": 0.6 },
  "keys": { "forward": "w", "jump": "space" }
}
```

`mode: auto` hands the game to the dedicated integration when it is connected
(through the relay) and drives it from Neuro Desktop otherwise. Override it per
session from the dashboard or with the `mode` parameter of `game_start_session`
(`auto`, `nd`, `external`, `hybrid`). `generic-keyboard-mouse.json` is the fallback
profile for games nobody wrote a profile for.

### Headless machines (no display, command line only)

Neuro Desktop runs on a server, container, SSH session or CI runner with no
graphical session. Nothing has to be installed for it: mouse and keyboard
libraries are loaded only when a display actually exists, and `NEURO_HEADLESS`
forces the mode either way.

```bash
# A CLI-only box: no X/Wayland, no pyautogui/pynput/mss needed
export NEURO_HEADLESS=1                    # optional: auto-detected when DISPLAY is unset
export NEURO_SHELL_ALLOWLIST="ls,cat,python3,git"   # programs Neuro may run (empty = none)
export NEURO_SHELL_TIMEOUT=20              # seconds per command (max 120)
export NEURO_SHELL_CWD=/srv/work

cd headless/apps/neuro-integration && go run . setup && go run .   # setup prints the dashboard token once
```

What changes on a headless machine:

- **`shell_command` is the capability that matters.** Neuro runs one command line
  per call and gets the exit code plus truncated stdout/stderr back, which is what
  a weak model needs to decide the next step: `shell_command {"command": "ls -la"}`.
- Mouse, keyboard, screenshot and game actions answer with a clear message
  ("no display session ... use the shell_command action") instead of crashing the
  controller at import time, which is what used to happen.
- `get_status` still reports platform, processes (from `/proc`, `psutil` optional)
  and screen size; there is simply no cursor or window to report.
- The dashboard, permissions, relay and game *registry* all work exactly as on a
  desktop, so Neuro can be pointed at a headless build during development.

The shell is fenced three times: the `shell` scope (off in every shipped example
policy, and not enabled by `default_allow`), the allowlist/denylist firewall, and
the same checks again inside the Python executor. Destructive patterns (`rm -rf /`,
`mkfs`, `shutdown`, `curl ... | sh`, `sudo`, `diskpart`, ...) are refused even when
the program is allowlisted; `NEURO_SHELL_DENYLIST` adds the operator's own
patterns.

### Seeing what it did (audit log) and stopping it

Two operator safety systems sit in front of every action:

```bash
export NEURO_PAUSED=1                              # start paused
export NEURO_KILL_SWITCH_FILE=/run/nd/STOP         # actions refuse while this file exists
export NEURO_AUDIT_LOG=/var/log/neuro-desktop.jsonl # one JSON line per decision
export NEURO_DENY_ACTIONS="type_text,key_press"     # a hard deny list the dashboard cannot undo
```

- `POST /api/control/pause` and `POST /api/control/resume` toggle the brake from
  the dashboard; `GET /api/control` shows the current state, and input release /
  status / session-end actions are always allowed so nothing stays stuck down.
- The kill-switch file is checked once per second; creating it stops action
  execution immediately and removing it resumes. Actions answer with the reason
  and the file name, so a model knows to wait rather than retry.
- `GET /api/audit?limit=100` returns the tail of the audit log
  (`{"time","event","action","decision","reason"}`); `NEURO_DENY_ACTIONS` is
  merged into the policy at load time and survives dashboard edits.

### Component Breakdown

| Component | Language | Role |
|-----------|----------|------|
| **neuro-integration** | Go | **Server**: Neuro API, permissions, audit, game interface, dashboard API, executor hub, vision client |
| **neuro-dashboard** (`headless/apps/neuro-dashboard`) | Go | **Dashboard program** (optional): serves the dashboard page and forwards `/api` and `/health` to the server. Stdlib only |
| **neuro-client** (`headless/apps/neuro-client`, built by `desktop/scripts/build-client.sh`) | Python, PyInstaller | **Client** for the PC Neuro controls: the agent as one program. Input, script parsing, shell, telemetry; the only part that touches that machine |
| **neuro-client-go** (`headless/apps/neuro-client-go`, not shipped yet) | Go | The Go port of the client, in progress: the executor link, status, shell and lifecycle commands. Input and screen still run only in the Python client |
| **controller/agent.py** | Python | The agent that `neuro-client` runs (also runnable from source as `python3 -m controller.agent`) |
| **controller/\*** | Python | Drivers the agent uses: `actions`, `desktop`, `shell`, `platform_intents`, `controls/` |
| **frontend** | TypeScript | **Dashboard page**, served by the server at `/ui/` and by the dashboard program |
| **relay host** (`neuro-integration relay`) | Go | Neuro Relay intermediary: lets other integrations and Neuro-OS watchers share the connection |
| **nd-vision-server** | Python | Optional vision service the server calls through `NEURO_VISION_URL` (stdlib HTTP, Pillow optional) |
| **MCP servers** | any | Optional extension servers. The server starts one only after Vedal enables it on the Extensions page |

## Quick Start

### Prerequisites

```bash
# Go 1.22+ — the server (required)
# Download from https://go.dev/dl/

# Python 3.10+ — the client, from source (PyInstaller builds neuro-client)
python3 --version

# Node.js 22.12+ — to build and test the dashboard client
node --version

```

### Installation

**Option 1: Pre-built bundle** (recommended)

1. Download the latest release archive
2. Extract it anywhere
3. Run `start.bat` (Windows) or `./start.sh` (Linux/macOS): that runs the one-time
   setup, then starts the server, the local client and the dashboard together. The
   first start prints the dashboard token once. Copy it; you type it into the
   dashboard's sign-in page. A release bundle also holds `neuro-dashboard` (run it on
   your own PC when the dashboard should not run on the server) and, when it could be
   built, the `neuro-client/` folder for the PC Neuro controls. See `README.txt` in the
   bundle, and `docs/DEPLOYMENT.md`

**Option 2: Build from Source**

```bash
# Clone repository
git clone https://github.com/cassitly/neuro-desktop.git
cd neuro-desktop

# Run automated build
desktop/scripts/build-all.sh   # Linux/macOS
.\desktop\scripts\build-all.ps1  # Windows
```

### First Run

```bash
# 1. Server + dashboard (PC that runs Neuro). Set up once, then start.
cd headless/apps/neuro-integration && go build -o neuro-integration .
./neuro-integration setup          # creates the relay token and PRINTS the dashboard token once
./neuro-integration                # starts the server and the dashboard

# 2. Client (PC Neuro should control; the same machine in a dev setup; new terminal at the repository root)
cd desktop/python && python3 -m controller.agent --bridge 127.0.0.1:9876
#    or, once built with desktop/scripts/build-client.sh: desktop/dist/neuro-client/neuro-client --bridge 127.0.0.1:9876

# 3. Dashboard: http://127.0.0.1:8300/ui/ — sign in with the token from step 1
#    or, on your own PC: go build -o neuro-dashboard . (in headless/apps/neuro-dashboard), then
#    ./neuro-dashboard --server http://127.0.0.1:8300 --listen 127.0.0.1:8310 and open
#    http://127.0.0.1:8310/ui/ (reach the server's admin port over SSH)
```

`setup` is safe to run again: it keeps the tokens that exist, and `setup --check`
only checks them. Lost the dashboard token? `./neuro-integration setup --rotate-dashboard`
makes a new one. A running server keeps the old token until it restarts.

Or stage the whole thing at once: `desktop/scripts/bundle/dev.sh` (Linux/macOS) /
`.\desktop\scripts\bundle\dev.ps1` (Windows). The server prints every connection attempt,
so you can see immediately whether Neuro, the client and the dashboard are up.

## Usage

### Basic Example

Once running, Neuro can execute commands like:

```javascript
// Move mouse to center of screen
{
  "action": "move_mouse_to",
  "params": { "x": 960, "y": 540 }
}

// Type text
{
  "action": "type_text",
  "params": { "text": "Hello from Neuro!" }
}

// Execute complex script
{
  "action": "run_script",
  "params": {
    "script": `
      TYPE "notepad"
      ENTER
      WAIT 1
      TYPE "Hello World!"
    `
  }
}
```

### Action Script Language

The script language supports powerful multi-command sequences:

```text
# Open application
SHORTCUT win
TYPE "notepad"
ENTER
WAIT 1

# Type content
TYPE "Dear User,"
ENTER
TYPE "This is Neuro!"
ENTER

# Save file
SHORTCUT ctrl s
WAIT 0.5
TYPE "neuro_message.txt"
ENTER
```

See [Action Script Documentation](docs/action_script/LANGUAGE_REFERENCE.md) for complete reference.

### Configuration

The server reads its settings from environment variables and command-line flags. There is no
configuration file to edit. The full list is in [desktop/README.md](desktop/README.md#configuration).

```bash
# Linux / macOS
export NEURO_SDK_WS_URL=ws://localhost:8000     # the Neuro API (the default)
export NEURO_ADMIN_LISTEN=127.0.0.1:8300        # the dashboard (the default)
export NEURO_EXECUTOR_TOKEN="$(openssl rand -hex 24)"   # the secret every agent presents
# Optional: NEURO_ADMIN_TOKEN must equal the dashboard token that setup printed.
```

```powershell
# Windows (PowerShell)
$env:NEURO_SDK_WS_URL = "ws://localhost:8000"
$env:NEURO_EXECUTOR_TOKEN = "a-long-random-value"
```

## Development

### Local Neuro API tester (Ollama + RWKV7)

In-repo stand-in for [Randy](https://github.com/VedalAI/neuro-sdk/tree/main/Randy) that
can also pick actions with [heredos/rwkv7:2.9b](https://ollama.com/heredos/rwkv7):

```bash
cd desktop/tools/ollama-neuro
./setup.sh
./run.sh --mode ollama --warm    # ws://127.0.0.1:8000 + http://127.0.0.1:1337/
# Instant IPC debugging without waiting on the LLM:
./run.sh --mode manual
```

On CPU laptops (e.g. Dell Latitude E7490), cold model load can take minutes —
use `--warm` / `--keep-alive -1`, or stay on `manual`/`random` while wiring IPC.
Details: [`desktop/tools/ollama-neuro/README.md`](desktop/tools/ollama-neuro/README.md).

### Fake executor (no display needed)

`desktop/tools/fake-executor/fake_executor.py` speaks the executor protocol against
the bridge and never generates input, so the dashboard, the permission checks and
the game interface can be exercised on any machine (including CI and macOS):

```bash
python3 desktop/tools/fake-executor/fake_executor.py --addr 127.0.0.1:9876
```

Run it before the bridge to see the executor as *connected* and a game as detected.

### Docker Modular Tests

Run modular tests in Docker:

```bash
docker compose -f docker-compose.tests.yml run --rm go-integration-tests
docker compose -f docker-compose.tests.yml run --rm python-parser-tests
```

### Coexistence with other integrations (Neuro Relay)

[Neuro Relay](https://github.com/Nakashireyumi/neuro-relay) multiplexes several
integrations behind one Neuro connection. Neuro Desktop participates as a relay
*integration* — it does not spawn the relay, which is a Python service with its own
YAML config:

```bash
export NEURO_RELAY_ENABLED=true
export NEURO_RELAY_URL="ws://127.0.0.1:8765"                 # relay intermediary socket
export NEURO_RELAY_TOKEN_FILE="$HOME/upstream-relay-token"   # a file holding the upstream intermediary.auth_token
export NEURO_RELAY_NAME="Neuro Desktop"
export NEURO_RESERVED_ACTIONS="minecraft_place_block,osu_click"
```

Keep the upstream token in its own file (mode 0600). Do not use the upstream sample,
`super-secret-token`, which is refused. You can also run `neuro-integration setup`
with `NEURO_RELAY_TOKEN_FILE` pointing at a file that does not exist yet. It creates
the file with a new token, and you copy that value into the upstream
`intermediary.auth_token`.

There are two ways to coexist, and they are complementary:

1. **Share one Neuro connection (recommended for games).** Point the bridge's own
   Neuro API client at the relay's Nakurity Backend instead of at Neuro
   (`NEURO_SDK_WS_URL=ws://127.0.0.1:8001`, the `nakurity-backend` port in the
   relay's `authentication.yaml`). The bridge then looks like any other Neuro SDK
   client to the relay: it sends `startup` (`game: "Neuro Desktop"`) and
   `actions/register`, and the relay multiplexes everything to the real backend.
   This is the path that makes "run alongside an existing game integration" work
   with no extra configuration.
2. **Look in on / be driven by the relay (operator visibility).** With
   `NEURO_RELAY_ENABLED=true` the bridge registers on the relay's intermediary
   socket (`ws://127.0.0.1:8765`) as `{"type":"integration","name":...,
   "auth_token":...}`, publishes its action schemas, and accepts `cmd` envelopes
   from Neuro-OS watchers — each one still subject to the same permission policy,
   pause flag and kill switch as Neuro's own calls. Note that the relay's
   intermediary keeps registrations for watchers; it does not forward them to
   Neuro, which is exactly why mode 1 exists.

### Neuro Relay (Go host)

Neuro Relay's intermediary (the socket integrations and Neuro-OS watchers connect to) is built into the server as a subcommand. It replaces the earlier Python shim, and the server no longer starts any relay process itself.

```bash
cd headless/apps/neuro-integration
go run . setup                       # once: writes ./relay-token (mode 0600), shared with the server
go run . relay --listen 127.0.0.1:8765 --health 127.0.0.1:8766   # integrations and watchers connect here (ws)
```

- The host reads its token from the relay token file (default `./relay-token`, or `NEURO_RELAY_TOKEN_FILE`). If the file does not exist, the host creates it. The server reads the same file, so nothing is copied by hand. The sample token from the upstream project is refused, and so is any token shorter than 16 characters.
- `NEURO_RELAY_AUTH_TOKEN` overrides the file for the host. Leave it unset, or set it to the file's value; `setup --check` reports a difference.
- `GET /health` on the health address reports the connected integrations, the watchers, and whether the optional Neuro link is up. It shows names and counts only.
- The bridge connects as a relay client with `NEURO_RELAY_ENABLED=true` and `NEURO_RELAY_URL=ws://127.0.0.1:8765`. It takes its token from the same file, so `NEURO_RELAY_TOKEN` is not needed. If you set it, it must equal the file, or the relay link stays off and the dashboard says why. The dashboard's Extensions page shows the live relay state.
- Browsers are refused (any request with an `Origin` header), binary frames are refused, and each connection has a frame-rate limit.
- A watcher with `NEURO_RELAY_NEURO_OS_TOKEN` (a second, enhanced token) can send `direct_to_neuro` messages, but only when the host has `NEURO_RELAY_NEURO_URL` set to a Neuro API server.

The upstream Python relay speaks the same socket, so the bridge can connect to it too. The bridge does not start it for you. Full details and the protocol are in [docs/RELAY.md](docs/RELAY.md).

### Project Structure

```
neuro-desktop/
├── headless/apps/
│   ├── neuro-integration/     # Go server: Neuro API, permissions, audit, dashboard API,
│   │                          #   relay host, MCP bridge, signed catalog, game interface
│   │   └── third_party/neuro-integration-sdk/   # the Go SDK port (replaced in go.mod)
│   ├── neuro-dashboard/       # Go dashboard program: serves the page, forwards /api and /health
│   ├── neuro-client/          # entry point for the PyInstaller build of the client
│   ├── neuro-client-go/       # Go port of the client, slice 1 (not shipped yet)
│   └── nd-vision-server/      # optional Python vision service (NEURO_VISION_URL)
├── desktop/
│   ├── apps/neuro-manager/    # native GTK 4 manager
│   ├── python/                # Python client/agent (controller/) and its tests
│   ├── frontend/              # TypeScript dashboard, served by the server at /ui/
│   ├── catalog/               # signed extension index, publisher keys, game profiles
│   ├── config/                # example permission policy and integration config
│   ├── scripts/               # build and bundle scripts (dev, prod), build-client (PyInstaller)
│   └── tools/                 # CI checks, fake executor, Ollama test client
└── docs/                      # architecture, safety, capabilities, relay, deployment
```

### Development Workflow

Paths below are relative to the repository root.

```bash
# 1. Build the server, the dashboard program and the dashboard page (Windows: .\desktop\scripts\build-all.ps1)
make -C desktop all

# 2. Run in development mode: a -tags neurodev build, set up, then started
desktop/scripts/bundle/dev.sh            # Windows: .\desktop\scripts\bundle\dev.ps1

# 3. Run tests. CI runs these too, plus the repository checks.
(cd headless/apps/neuro-integration && go test -race -count=1 ./... && go test -race -count=1 -tags neurodev ./...)   # the second run covers the dev-only unsigned-extension switch
python3 -m unittest discover desktop/python/tests -t desktop/python   # client (agent) tests
(cd headless/apps/neuro-dashboard && go test -count=1 ./...)                     # dashboard program tests
python3 desktop/tools/ci/repo_checks.py    # repository checks

# 4. Build the release bundle (no -tags neurodev). It also tries to build neuro-client
desktop/scripts/bundle/prod.sh           # Windows: .\desktop\scripts\bundle\prod.ps1
desktop/scripts/build-client.sh          # neuro-client alone (Windows: .\desktop\scripts\build-client.ps1)
```

### Adding New Actions

1. **Define it** in `headless/apps/neuro-integration/action-registry.go`: a `CommandType` constant, then an `actionSpec` in `HLActionSpecs` or `LLActionSpecs` with the name, a description that ends with a one-line example, and the parameter schema.
2. **Build the command** in `buildIPCCommand` (same file). Reject bad input there with a message that names the parameter and shows a valid example. That text is what a small model reads on its next turn.
3. **Give it a scope** in `headless/apps/neuro-integration/permissions.go` (`actionScope`). Actions without a scope follow `default_allow`. Only actions that cannot start anything belong in `alwaysAllowed`.
4. **Connect the executor** if the command runs on the machine: add it to the allowlist in `executor_commands.go`, and handle it in the Python agent (`desktop/python/controller/agent.py`).
5. **Test it**: add the call a small model would make to `headless/apps/neuro-integration/testdata/weak_model_cases.json` with the expected verdict and reply text, then run `go test ./...`.

### Testing with Randy

Randy is a mock Neuro API server for testing:

```bash
# Terminal 1: Start Randy
cd Randy
npm install
npm start

# Terminal 2: Run Neuro Desktop
cd headless/apps/neuro-integration
go run .
```

Randy will send random actions to test your integration.

## Documentation

- [Native Manager Setup](desktop/apps/neuro-manager/README.md) — registration, approvals and scoped tokens
- [Development and Release Workflow](docs/WORKFLOW.md) — repositories, catalog updates, checks and releases
- [Delegated Manager Sessions](docs/DELEGATED_MANAGER.md) — let Neuro/Evil manage approved integrations
- [Extension Permissions](docs/EXTENSION_PERMISSIONS.md) — management, runtime capabilities and settings
- [Vision Service Controls](docs/VISION.md) — built-in service, per-twin settings and image privacy
- [Integration SDK](https://github.com/cassitly/neuro-desktop-sdk) and [Vencord Adapter](https://github.com/cassitly/neuro-desktop-vencord) — separate source and setup guides
- ✅ [Current Capabilities](docs/CAPABILITIES.md) — what works today (honest)
- 🐣 [Driving it with a small/weak model](docs/LLM_GUIDE.md) — prompting tactics and parameter shapes
- 🛡️ [Safety systems and firewalls](docs/SAFETY.md) — every guard, where it lives, how to verify it
- 📖 [Action Script Language Reference](docs/action_script/LANGUAGE_REFERENCE.md)
- 🏗️ [Architecture Deep Dive](docs/ARCHITECTURE.md)
- 🔧 [API Specification](headless/apps/neuro-integration/integration-docs/Action Script Documentation.md)
- 🚀 [Deployment Guide](docs/DEPLOYMENT.md)
- 🧪 [Ollama Neuro Tester](desktop/tools/ollama-neuro/README.md)
- 🤝 [Contributing Guidelines](CONTRIBUTING.md)
- 🧭 [Project Vision](VISION.md)
- 🗺️ [Production TODO](docs/PRODUCTION_TODO.md)
- 📝 [Changelog](CHANGELOG.md)
- 🤝 [Code of Conduct](CODE_OF_CONDUCT.md)
- 🔐 [Security Policy](SECURITY.md)

## Troubleshooting

### Common Issues

**`EACCES` / Permission denied on `frontend/dist`**

Leftover from a `sudo` bundle. Fix ownership, never rebuild as root:

```bash
cd desktop
sudo chown -R "$USER:$USER" frontend/dist dist python/.venv
./scripts/bundle/dev.sh
```

**The dashboard says the server has no token (`503`), or the token is refused (`401`)**
```bash
cd headless/apps/neuro-integration
./neuro-integration setup --check   # says what is missing or does not match; changes nothing
./neuro-integration setup           # only if no token exists yet
```
The sign-in page takes the token that `setup` printed. If it was lost, `setup --rotate-dashboard`
makes a new one, and the server must be restarted to use it.

**The server binary is missing or out of date**
```bash
# Rebuild the server
cd headless/apps/neuro-integration
go build -o neuro-integration .     # Windows: go build -o neuro-integration.exe .
```

**"Failed to initialize Python controller"**
```bash
# Reinstall Python dependencies
cd desktop/python
python -m venv .venv
.venv\Scripts\activate
pip install -r requirements.txt
```

**"WebSocket connection failed"**
```bash
# Check if Randy or Neuro API is running
curl ws://localhost:8000
# Or start Randy
cd Randy && npm start
```

See [Troubleshooting Guide](docs/TROUBLESHOOTING.md) for more solutions.

## Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

### Areas We Need Help

- 🐛 Bug reports and fixes
- 📝 Documentation improvements
- ✨ New action types
- 🧪 Test coverage
- 🌐 Cross-platform testing
- 🎨 UI/UX improvements

## Roadmap

- [ ] **v0.1.0**: Core functionality (current)
- [ ] **v0.2.0**: Enhanced safety features
- [ ] **v0.3.0**: Vision system integration
- [ ] **v0.4.0**: Advanced macro system
- [ ] **v1.0.0**: Production-ready release

## License

This project retains the upstream GPL-3.0 license; see [LICENSE](LICENSE).
Bundled third-party components retain their own licenses.

## Acknowledgments

- **Nakashireyumi** - Original Neuro Desktop / Neuro-OS development
- **Neuro-sama** - The AI that makes this all worthwhile
- **Vedal** - Creator of Neuro-sama
- The community for testing and feedback

## Support

- 💬 [Discord](https://discord.gg/neuro)
- 🐛 [Issue Tracker](https://github.com/cassitly/neuro-desktop/issues)
- 📧 Email: support@neuro-desktop.dev

---

**Made with ❤️ by the Neuro Desktop Team**

*"Giving Neuro the keys to the desktop, one action at a time."*


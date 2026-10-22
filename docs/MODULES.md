# Neurcro module guide

This is the map for operators and contributors who need to understand more than
one program. Each section covers the same eight questions: purpose, setup,
configuration, permissions, protocols, tests, interconnection and platform
limits. Protocol detail stays in the dedicated guides linked below, so this page
does not become a second, drifting specification.

Neurcro is the public name. Executables, repository paths, package IDs,
`NEURO_*` variables and wire action names deliberately keep their `neuro-*`
forms for compatibility. In particular, `run_script` keeps its existing name and
wire format.

## Status at a glance

| Module | Where | Status |
| --- | --- | --- |
| Server | `headless/apps/neuro-integration` | Shipped; the central policy and protocol process |
| Python client / agent | `desktop/python/controller` | Shipped; packaged as `neuro-client` |
| Native manager | `desktop/apps/neuro-manager` | Shipped; GTK 4, built on Linux, Windows and macOS CI |
| Dashboard page | `desktop/frontend` | Shipped; served by the server or dashboard program |
| Dashboard program | `headless/apps/neuro-dashboard` | Shipped; optional remote-dashboard front end |
| Vision service | `headless/apps/nd-vision-server` | Optional; built-in controls, separately managed process |
| Relay host | server binary, `relay` subcommand | Shipped; replaces the earlier Python shim |
| Go client, slice 1 | `headless/apps/neuro-client-go` | Not shipped; executor link, status, shell and lifecycle only |
| SDK | separate `neuro-desktop-sdk` repository | Published as source; not a PyPI release |
| Vencord adapter | separate `neuro-desktop-vencord` repository | Published source plugin; not a private Discord backend |
| Catalog and marketplace | `desktop/catalog`; `marketplace/` snapshot | Signed runtime entries plus reviewed source metadata |

## Layout and ownership

```text
desktop/          graphical and operator-facing code
  apps/           native manager
  frontend/       browser dashboard
  python/         client/agent and its drivers
  catalog/        signed runtime catalog and game profiles
  config/         example permission policy
  scripts/        build, bundle and packaging entry points
  tools/          CI checks, fake executor, local model tester
headless/apps/    programs that do not need a GUI
  neuro-integration/  server, relay host and MCP bridge
  neuro-dashboard/    dashboard proxy program
  neuro-client/       PyInstaller entry point
  neuro-client-go/    partial Go client
  nd-vision-server/   optional vision service
docs/             operator and protocol documentation
marketplace/      reviewed source-index snapshot and tests
ecosystem/        separate Git repositories during development (ignored here)
```

The shared catalog, configuration and build scripts stay under `desktop/` in this
pass. They are operator/build assets rather than another runtime program, and
moving them to a new top-level directory would add path churn without changing a
module boundary. No symlinks are used, so a Windows checkout behaves the same as
a Linux or macOS one.

## Runtime interconnection

```text
Neuro API (WebSocket)
        │  startup, actions/register, action, action/result, context
        ▼
┌──────────────────── server: neuro-integration ────────────────────┐
│ policy · audit · pause/kill · rate limits · game profiles         │
│ executor hub ◀──── newline JSON, v2 ──── Python client / Go slice │
│ admin HTTP  ◀──── token ──── manager, dashboard page/program      │
│ vision HTTP ──── bearer token ──── nd-vision-server               │
│ relay client ─── fixed token ──── relay host ── integrations      │
│ MCP bridge ───── stdio child processes                            │
└───────────────────────────────────────────────────────────────────┘
```

| From | To | Transport | Authentication |
| --- | --- | --- | --- |
| Server | Neuro API | WebSocket | `NEURO_WS_TOKEN` / connection options |
| Agent | Server hub | TCP, newline JSON | `NEURO_EXECUTOR_TOKEN` |
| Server | Co-located agent | Files under `NEURO_IPC_DIR` | Session and request echoes, file modes |
| Manager / dashboard | Server | HTTP `/api/`, `/health` | Dashboard token; fresh operator token for approvals |
| Dashboard program | Server | HTTP proxy | Forwards the caller's token unchanged |
| Server | Vision service | HTTP | `NEURO_VISION_TOKEN` |
| Server | Relay host | WebSocket | Relay token file |
| Relay integrations | Relay host | WebSocket | Relay token |
| Server | MCP servers | stdio | Process boundary; per-item grants |
| SDK adapter | Server | HTTP integration routes | Scoped integration token |
| Viewer / overlay | Server | HTTP viewer route | Scoped viewer token |

The server never reads a path named by an agent, and the vision service only
accepts byte payloads from the server. Screenshots travel as bounded PNG bytes.
See [EXECUTOR_PROTOCOL.md](EXECUTOR_PROTOCOL.md) and [SAFETY.md](SAFETY.md).

## Server (`neuro-integration`)

**Purpose.** Owns the Neuro API connection, action registry, permission policy,
rate limits, audit, pause/kill state, game sessions, executor hub, admin API,
catalog verification, MCP bridge, relay client and vision client.

**Setup.**

```bash
cd headless/apps/neuro-integration
go build -o neuro-integration .
./neuro-integration setup          # once; prints the dashboard token
./neuro-integration                # starts the server and serves /ui/
./neuro-integration setup --check  # verify without changing files
```

**Configuration.** The full table is in [desktop/README.md](../desktop/README.md).
The high-value groups are `NEURO_SDK_WS_URL`, `NEURO_ADMIN_LISTEN`,
`NEURO_EXECUTOR_LISTEN`/`NEURO_EXECUTOR_TOKEN`, `NEURO_PERMISSIONS_FILE`,
`NEURO_AUDIT_LOG`, `NEURO_PAUSED`, `NEURO_KILL_SWITCH_FILE`,
`NEURO_SHELL_ALLOWLIST`, `NEURO_VISION_URL` and the `NEURO_RELAY_*` family.
Flags override the matching environment variables where documented.

**Permissions.** Every action passes the stop switch, deny list, scoped policy,
rate limit and action-specific checks. Global scopes are `input`, `vision`,
`game`, `process`, `network`, `filesystem`, `extensions`, `shell` and `system`.
The example policy keeps shell and system off. `desktop_guide`,
`reset_controls` and `request_permission` remain reachable so a model can recover
without a hidden escape route. Extension installation is separately signed and
per-item grants are independent of global scopes. See [SAFETY.md](SAFETY.md) and
[EXTENSION_PERMISSIONS.md](EXTENSION_PERMISSIONS.md).

**Protocols.** Neuro's official WebSocket API; the executor protocol v2; the
token-protected admin HTTP API; the relay WebSocket protocol; stdio MCP; and the
vision `/describe` contract. Action schemas are registered with Neuro and exposed
at `GET /api/actions`.

**Tests.**

```bash
cd headless/apps/neuro-integration
go vet ./...
go test ./...
go test -race ./...
go test -race -tags neurodev ./...
```

**Interconnection.** Start the server before the manager, dashboard, relay host
or clients. The server does not supervise or start those processes. An agent can
connect out to the hub, or a co-located agent can use file IPC.

**Platform.** Go 1.22+. CI builds and tests on Linux, Windows and macOS; the race
suite runs where cgo and the race detector are available. Live Neuro is not
required for protocol tests, but live model behaviour is not covered by fixtures.

**Troubleshooting.** A dashboard `502` means the dashboard program cannot reach
the server. `invalid or missing executor token` means the agent and server
disagree. An action that is accepted but never completes usually has no connected
agent; check `GET /health`, the executor hub address, and the audit log.

## Python client / agent (`neuro-client`)

**Purpose.** The only process that touches the controlled machine: pointer and
keyboard input, screen capture, windows, process telemetry, shell, scripts and
input queues. It reconnects on its own and refuses GUI work honestly when no
interactive desktop is available.

**Setup.**

```bash
cd desktop/python
python3 -m controller.agent --bridge 127.0.0.1:9876
# or package it:
desktop/scripts/build-client.sh
desktop/dist/neuro-client/neuro-client --bridge 127.0.0.1:9876
```

Headless operation needs only the Python standard library. A graphical session
needs the packages in `desktop/python/requirements.txt`; Wayland capture also
needs `grim`.

**Configuration.** `NEURO_AGENT_BRIDGE`, `NEURO_EXECUTOR_TOKEN`, `NEURO_IPC_FILE`,
`NEURO_HEADLESS`, `NEURO_SHELL_ALLOWLIST`, `NEURO_SHELL_DENYLIST`,
`NEURO_SHELL_CWD`, `NEURO_SHELL_TIMEOUT`, `NEURO_SHELL_MAX_OUTPUT`,
`NEURO_ALLOW_XWAYLAND_INPUT` and the capture/context limits documented in
[CAPABILITIES.md](CAPABILITIES.md).

**Permissions.** The server decides what may run; the agent rechecks its own
headless state, platform support, shell firewall and allowlist. `type_text`
accepts printable ASCII only and refuses other text atomically. Wayland
full-desktop input is refused unless the operator explicitly accepts the
XWayland-only limitation.

**Protocols.** Executor protocol v2 over newline-delimited JSON or file IPC.
Commands are listed in [EXECUTOR_PROTOCOL.md](EXECUTOR_PROTOCOL.md); the
`run_script` language is documented in
[action_script/LANGUAGE_REFERENCE.md](action_script/LANGUAGE_REFERENCE.md).

**Tests.**

```bash
python3 -m unittest discover -s desktop/python/tests -t desktop/python -v
```

CI runs the suite without a display. Focused tests cover script preflight,
queues, shell rules, capture geometry, portability probes and input release.

**Interconnection.** The agent always dials out; it needs no inbound port. The
server must be running with a matching executor token. File IPC is for a
co-located process only.

**Platform.** Windows supports an interactive input desktop; Linux supports X11
and native Wayland capture through `grim`; macOS needs Accessibility and Screen
Recording approval. Windows/macOS input and capture have not been verified on
real devices here even though their builds and protocol tests pass.

**Troubleshooting.** `no display` means the process lacks a graphical session.
On Wayland, input failures are expected unless XWayland-only input was explicitly
accepted. A stuck key should be cleared with `reset_controls`; a shell refusal
should name the missing scope, allowlist entry or blocked pattern.

## Native manager (`neuro-manager`)

**Purpose.** GTK 4 operator application: sign-in, status, permissions, activity,
extensions, marketplace, games and vision-service controls. It is a client of the
server, not a supervisor.

**Setup.**

```bash
cd desktop
python3 scripts/build-manager.py
./dist/neuro-manager/neuro-manager --server http://127.0.0.1:8300
```

See the [manager README](../desktop/apps/neuro-manager/README.md) for standalone
builds, macOS bundles and platform dependencies.

**Configuration.** `NEURO_MANAGER_SERVER`, `NEURO_MANAGER_PYTHON`,
`NEURO_MANAGER_OUT`, `NEURO_MANAGER_SOURCE_ONLY`, `NEURO_NO_MANAGER` and
`NEURO_MANAGER_GUI_TESTS`. The server origin and operator token are entered at
sign-in; the token stays in memory.

**Permissions.** The operator session can change policy and mint scoped
credentials; it must re-enter the operator token for sensitive writes. A delegated
Neuro/Evil session is read-only outside its explicit grants. Ambient proxies are
ignored so headers cannot be forwarded elsewhere. See
[DELEGATED_MANAGER.md](DELEGATED_MANAGER.md).

**Protocols.** The token-protected admin HTTP API only. The manager does not
speak the executor, relay or Neuro API protocols directly.

**Tests.**

```bash
PYTHONPATH=apps/neuro-manager python3 -m unittest discover -s apps/neuro-manager/tests
NEURO_MANAGER_GUI_TESTS=1 PYTHONPATH=apps/neuro-manager \
  python3 -m unittest discover -s apps/neuro-manager/tests
```

**Interconnection.** Requires a running server for every page except the sign-in
and startup shell. It polls only the visible page and refreshes on a bounded
schedule.

**Platform.** GTK 4 and PyGObject on Linux, Windows (MSYS2 UCRT64) and macOS
(Homebrew). CI builds and exercises the native widgets on all three systems.
Local Linux use has been exercised visually; packaging/signing beyond CI is not
verified.

**Troubleshooting.** If the window opens but the server card is offline, check the
origin, firewall and `NEURO_ADMIN_LISTEN`. If GTK cannot be imported, use a
GTK-enabled interpreter or the standalone build. macOS bundle failures usually
mean PyInstaller was not run on that macOS version/architecture.

## Dashboard page and dashboard program

**Purpose.** The page is the browser operator UI. The optional Go program serves
that page on a separate host and proxies only `/api/` and `/health` to the
server, so a remote operator does not expose the server port directly.

**Setup.**

```bash
cd desktop/frontend
npm install
npm run build
# server serves ../frontend/dist automatically, or:
cd ../headless/apps/neuro-dashboard
go build -o neuro-dashboard .
./neuro-dashboard --server http://127.0.0.1:8300 --listen 127.0.0.1:8310
```

**Configuration.** `NEURO_UI_DIR`, `NEURO_DASHBOARD_LISTEN`,
`NEURO_DASHBOARD_SERVER`, `NEURO_ADMIN_URL` and `NEURO_ADMIN_TOKEN` (the last is
the server-side override, not a dashboard-program secret). A non-loopback listen
address prints a warning.

**Permissions.** Every `/api` route except `/health` needs the dashboard token,
including loopback. The token is read from the `X-ND-Token` header or a Bearer
header, never a query parameter. Sensitive writes need a freshly entered operator
token. The dashboard program adds no authority and stores no secret.

**Protocols.** HTTP plus JSON; the server also serves WebSocket-backed viewer and
event traffic where documented. The program refuses non-`/api` and non-`/health`
paths.

**Tests.**

```bash
cd desktop/frontend && npm test && npm run build
cd headless/apps/neuro-dashboard && go vet ./... && go test ./...
```

**Interconnection.** The page can be served directly by the server at `/ui/` or
by the dashboard program. The program is a forwarder, not a cache or policy
engine. The viewer overlay uses a separate read token in the URL fragment.

**Platform.** Browser UI is platform-neutral. CI builds the page and tests the Go
program on three systems. Browser flows were exercised on Linux against fixtures;
real browser/macOS/Windows combinations are not certified.

**Troubleshooting.** `502` from the program means the server is down or the
`--server` origin is wrong. A blank page usually means the UI directory is
missing or not built. Sign-in failures with a correct token usually indicate a
rotated token hash; rerun `setup --rotate-dashboard` deliberately.

## Vision service (`nd-vision-server`)

**Purpose.** Optional screen description for `game_observe` and approved
observations. It is a separate HTTP process; disabling Neurcro's image use does
not stop it.

**Setup.**

```bash
cd headless/apps/nd-vision-server
NEURO_VISION_BACKEND=stats python3 -m nd_vision --host 127.0.0.1 --port 8610
# server:
NEURO_VISION_URL=http://127.0.0.1:8610 NEURO_VISION_TOKEN=... ./neuro-integration
```

**Configuration.** `NEURO_VISION_BACKEND`, `NEURO_VISION_MODEL`,
`NEURO_VISION_OLLAMA_URL`, `NEURO_VISION_OPENAI_URL`, `NEURO_VISION_OPENAI_KEY`,
`NEURO_VISION_WORKERS`, `NEURO_VISION_WORKER_TOKEN`, connection/inference limits
and the 40-second deadline. See the
[vision README](../headless/apps/nd-vision-server/README.md).

**Permissions.** The server checks the `vision` scope, per-persona settings and
current approval before capture, inference and delivery. The service requires a
bearer token off loopback. Cluster workers are a fixed operator list; no URL from
a model response is followed. See [VISION.md](VISION.md).

**Protocols.** `GET /health` and `POST /describe` with bounded JSON, base64 image
bytes and an optional deadline header. Errors are explicit `4xx`/`5xx` JSON
responses. Redirects are refused and upstream reads are capped.

**Tests.**

```bash
cd headless/apps/nd-vision-server
python3 -m unittest discover -s tests -t .
```

**Interconnection.** The server sends image bytes to one configured service. The
service may forward to one worker and then one failover worker, never a
speculative fanout. `game_observe` waits for the result as a context message.

**Platform.** Pure Python on all three systems; `stats` needs Pillow, `ocr` needs
tesseract, and model backends need their local/remote endpoint. Real semantic
model inference was not available here; OCR and cluster failover were exercised
with real local processes.

**Troubleshooting.** `401` means the token differs. `503` means connection or
analysis slots are full. `504` means the deadline expired. A `502` names the
unreachable backend or worker. If summaries are missing, check whether vision was
disabled globally or for that persona.

## Relay host and relay client

**Purpose.** Share one Neuro connection among several integrations and watchers,
or let Neurcro coexist with an existing game integration. The host is built into
the server binary; the server can also connect to it as an integration.

**Setup.**

```bash
cd headless/apps/neuro-integration
./neuro-integration setup                 # creates the relay token file
./neuro-integration relay --listen 127.0.0.1:8765 --health 127.0.0.1:8766
# in the server environment:
NEURO_RELAY_ENABLED=true NEURO_RELAY_URL=ws://127.0.0.1:8765 ./neuro-integration
```

**Configuration.** `NEURO_RELAY_LISTEN`, `NEURO_RELAY_HEALTH_LISTEN`,
`NEURO_RELAY_TOKEN_FILE`, `NEURO_RELAY_TOKEN`, `NEURO_RELAY_NAME`,
`NEURO_RELAY_NEURO_URL` and `NEURO_RELAY_NEURO_OS_TOKEN`. The token file is the
shared source of truth; an override must match it.

**Permissions.** The host refuses browser origins, binary frames, weak tokens and
excessive frame rates. Relay commands pass the same stop/policy/rate gates as
Neuro's actions. `direct_to_neuro` requires the enhanced watcher token.

**Protocols.** One JSON registration frame, then JSON events/actions over
WebSocket. See [RELAY.md](RELAY.md). The upstream limitation remains: actions
registered on an intermediary do not automatically reach Neuro.

**Tests.**

```bash
cd headless/apps/neuro-integration
go test -run 'Relay' ./...
go test -run 'RelayHost' ./...
```

CI also runs a live host/client smoke test.

**Interconnection.** The host is a separate process from the server even though
the same binary contains it. The server does not start the host. Integrations may
connect to the host; the server connects as one more integration when enabled.

**Platform.** Go on all three systems. Real third-party relay integrations were
not available; protocol, reconnect and smoke tests use the repository's own
client and fixtures.

**Troubleshooting.** `invalid auth token` means the host and client disagree on
the file. A configured link that stays `disabled` or `unreachable` is reported on
the Extensions page with a reason. A duplicate name replaces the older connection.

## Go client, slice 1 (`neuro-client-go`)

**Purpose.** Incremental port of the Python client: executor link, status,
shell, heartbeat and lifecycle. It is not in the bundle and does not implement
input, capture, windows or `run_script`.

**Setup and tests.**

```bash
cd headless/apps/neuro-client-go
go vet ./... && go test -count=1 ./...
go build -o neuro-client-go .
```

**Configuration.** `--bridge`, `--token`, `--once` and the same shell/headless
variables as the Python client where implemented.

**Permissions and protocols.** Uses executor protocol v2 and the same shell
firewall. Unsupported commands are refused explicitly rather than silently
dropped. A parity test fails if the Python agent adds a command this client
neither handles nor refuses.

**Interconnection.** Drop-in only for the subset above; do not present it as a
full client. File IPC and GUI commands remain in the Python client.

**Platform.** Builds without cgo on Linux, macOS and Windows; CI runs its tests
on all three. Windows shell behavior is not verified locally.

**Troubleshooting.** A refused input/script command is expected in this slice. A
connection failure follows the same token and hub checks as the Python client.

## SDK and Vencord adapter

The separate [SDK](https://github.com/cassitly/neuro-desktop-sdk) adds a
versioned manifest, scoped configuration, readiness heartbeats and an optional
curated viewer feed to an existing official Neuro API integration. It does not
replace the official action protocol or carry a private backend.

The separate [Vencord adapter](https://github.com/cassitly/neuro-desktop-vencord)
is a local Discord visibility plugin: it publishes a bounded selected-channel
projection and never reads messages, credentials or member lists. It needs a
scoped integration token and explicit `discord.viewer` approval; its viewer token
is independent.

Both are source repositories with their own tests and CI. The application pins
Vencord's reviewed upstream commit and checks the SDK wheel against the real
server. Installation does not imply a PyPI release, a live Discord session or
Vedal's private backend. See [MARKETPLACE.md](MARKETPLACE.md) for the registration
contract and licensing boundaries.

## Catalog, marketplace and game profiles

`desktop/catalog/index.json` authorizes signed runtime entries.
`desktop/catalog/marketplace.json` is a reviewed source snapshot; a listing does
not gain execution privileges from appearing there. Game profiles in
`desktop/catalog/games` define control modes (`nd`, `external`, `hybrid`, `auto`),
keybinds and launch metadata.

The catalog lives on its own branch upstream. Do not treat the eight marketplace
source listings as eight functioning bundled game integrations: game/mod setup,
start/stop ownership, rollback and per-platform acceptance remain
integration-specific work. See [MARKETPLACE.md](MARKETPLACE.md) and
[WORKFLOW.md](WORKFLOW.md).

## Test tools

```bash
# Repository policy, JSON, docs and path checks
python3 desktop/tools/ci/repo_checks.py

# Protocol simulator: no real input is generated
python3 desktop/tools/fake-executor/fake_executor.py --addr 127.0.0.1:9876

# Local small-model tester (needs a local Ollama model)
python3 desktop/tools/ollama-neuro/ollama_brain.py --help

# Full desktop build/bundle
make -C desktop test
desktop/scripts/build-all.sh --no-test
desktop/scripts/bundle/dev.sh
```

`fake-executor` is a fixture, not a desktop backend. `ollama-neuro` is a local
tester, not evidence of live Neuro behavior.

## Platform support

| Capability | Linux | Windows | macOS |
| --- | --- | --- | --- |
| Server, dashboard program, relay host | Built/tested | Built/tested | Built/tested |
| Python client, shell/headless | Tested | Protocol/build tested; input unverified | Protocol/build tested; input unverified |
| GUI input | X11 yes; Wayland refused by default | Interactive desktop required | Accessibility approval required |
| Screen capture | X11 MSS; Wayland `grim` | MSS, unverified on device | MSS after Screen Recording approval |
| Native manager | Built and used locally | Built/tested in CI | Built/tested in CI |
| Standalone client package | Built in CI | Build not run | Build not run |
| Signing/notarization | Not provided | Not provided | Not provided |

A cross-build, CI package check or fixture does not certify real input, capture,
permissions or installation on that platform. Live Neuro/Evil, private
integrations and real game/mod installers remain acceptance work.

## Cross-module troubleshooting

| Symptom | Likely module | Check |
| --- | --- | --- |
| Dashboard sign-in fails | Server or page | Token hash, `setup --rotate-dashboard`, header path |
| Dashboard page blank | Frontend or dashboard program | `npm run build`, `NEURO_UI_DIR`, `index.html` |
| Dashboard `502` | Dashboard program | `--server`, server process, firewalls |
| Actions accepted but never finish | Agent/executor | Hub address, token, `/health`, audit log |
| GUI action refused | Agent | Headless state, Wayland policy, OS permission, intent support |
| Shell refused | Server or agent | Scope, allowlist, deny list, firewall pattern |
| Vision missing or stale | Vision or server | URL/token, 503/504, persona/global approval |
| Relay stays offline | Relay host/server | Token file match, URL, health endpoint, name |
| Extension cannot run | Catalog/permissions | Signature, publisher, runtime grant, fresh approval |
| Manager cannot import GTK | Manager | GTK-enabled interpreter or standalone build |

For deeper diagnostics, use [TROUBLESHOOTING.md](TROUBLESHOOTING.md),
[SAFETY.md](SAFETY.md) and the module-specific READMEs.

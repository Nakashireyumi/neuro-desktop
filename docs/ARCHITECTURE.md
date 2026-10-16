# Architecture

Neuro Desktop lets Neuro and Evil use applications and operating systems, including
applications without a dedicated integration. The native manager, server and
controlled-machine client run as separate processes:

* the **server** (`neuro-integration`) receives Neuro's commands, applies Vedal's policy,
  and does the heavy work (the dashboard API, the vision client, the relay);
* the **native manager** (`neuro-manager`) provides GTK 4 desktop controls and a
  startup splash, with no embedded browser;
* the optional **dashboard program** (`neuro-dashboard`) serves the browser dashboard and forwards
  its API to the server. It is optional: the server serves the page too;
* the **client** (`neuro-client`) runs on the PC Neuro controls. It carries out the
  commands the server sends, and nothing else.

The split exists so that the PC Neuro controls does not need to be powerful. The heavy
work runs on the server, and that PC only needs the client.

```
                 Neuro API (websocket, Neuro-sama's backend)
                                  │
                                  ▼
 ┌─────────────────────── SERVER (Go, neuro-integration) ───────────────────────┐
 │ Neuro client (vendored SDK port)   policy · scopes · rate limits · audit     │
 │ stop switch: pause / kill file     executor hub ◀── clients (TCP, token)    │
 │ dashboard API + UI at /ui/         file IPC fallback (co-located client)    │
 │ relay client ──▶ Neuro Relay       relay host (`relay` subcommand, optional) │
 │ MCP bridge ──▶ enabled MCP servers (child processes)                   │
 │ catalog (signed) · game profiles · extension state · vision client           │
 └──────────────────────────────────────────────────────────────────────────────┘
      ▲ dashboard API (token)                  │ commands        ▲ results, telemetry
      │                                        ▼                 │
 ┌──── DASHBOARD PROGRAM (Go, neuro-dashboard) ────┐   ┌──── CLIENT (neuro-client) ────────┐
 │ serves the dashboard page; forwards /api and    │   │ on the PC Neuro controls: input,  │
 │ /health only. No secret, no policy, adds no     │   │ screen, shell, scripts. No UI,    │
 │ token of its own. Optional.                     │   │ vision or policy. Python agent,   │
 └─────────────────────────────────────────────────┘   │ packaged with PyInstaller.        │
                                                       └───────────────────────────────────┘

 NATIVE MANAGER (GTK 4) / OPTIONAL DASHBOARD (TypeScript + Vite):
 Extensions · Games · Permissions · Status (Settings)
```

## Components

| Component | Where | Language | Role |
| --- | --- | --- | --- |
| Native manager | `desktop/apps/neuro-manager` | Python + GTK 4 | Desktop controls, native splash, bounded background API requests; standalone builds include GTK |
| Server | `desktop/apps/neuro-integration` | Go | Everything Neuro can reach, and every gate between Neuro and the machine |
| Client (`neuro-client`) | `desktop/backend/python/controller`, entry `desktop/apps/neuro-client`, packaged by `desktop/scripts/build-client.sh` | Python (stdlib-first), PyInstaller | Executes commands on the PC Neuro controls; reconnects on its own. Also run from source as `python3 -m controller.agent` |
| Dashboard program | `desktop/apps/neuro-dashboard` | Go (stdlib only) | Serves the dashboard page and forwards `/api` and `/health` to the server. Optional |
| Dashboard page | `desktop/frontend` | TypeScript | Vedal's controls. The server serves it at `/ui/`; the dashboard program serves it too |
| Relay host | the server binary, `relay` subcommand | Go | Neuro Relay socket for integrations and watchers |
| Vision | `desktop/apps/nd-vision-server` | Python (stdlib HTTP) | Optional screen description for `game_observe` |
| MCP servers | catalog-installed | any | Optional tools, started only after Vedal enables them |
| Test tools | `desktop/tools/` | Python | `fake-executor` (protocol simulator), `ollama-neuro` (small-model tester) |

The Go SDK port is vendored under `desktop/apps/neuro-integration/third_party/`
and pinned in `go.mod`.

## How a command flows

1. Neuro sends an action over the Neuro API websocket.
2. The server checks, in order: the **stop switch** (paused, or the kill-switch
   file exists), the **permission policy** (is the action's scope allowed, or
   requestable?), and the **per-scope rate limit**. A refusal is returned to
   Neuro with a message that says what to do next. The refusal is audited.
3. The command goes to an **agent** connected to the executor hub. If no agent is
   connected and one runs on the same machine, the server falls back to file IPC
   (`NEURO_IPC_FILE`).
4. The agent runs it and reports back. The server answers Neuro's `action/result`
   early, then sends the output as context when it arrives, for long commands.

Watcher commands from the relay take the same path, with the same gates
(`denyRelayCommand` in `relay.go`).

## Where state lives

| State | Where | Notes |
| --- | --- | --- |
| Policy | `NEURO_PERMISSIONS_FILE` (`permissions.json`) | the example is `desktop/config/permissions.example.json` |
| Permission requests and approvals | in memory, shown on the Permissions page | Neuro asks; Vedal approves or denies. A restart clears both |
| Audit | `NEURO_AUDIT_LOG` (JSON lines) | refusals, accepted actions, relay commands, policy changes |
| Extension state | `NEURO_EXTENSIONS_STATE_FILE` (runtime, not tracked by Git) | what is installed and enabled |
| Catalog | `desktop/catalog/index.json`, `publishers.json` | signed; the bridge verifies every item |

## Decisions

* **Three programs, split at the process boundary.** The server and the dashboard
  program are Go; the client is the Python agent packaged with PyInstaller as
  `neuro-client`. The split puts the power on the server, not on the PC Neuro
  controls. The alternative, a full rewrite of the client into Go now, was considered
  and not done. The input and capture code is platform-specific, and it cannot be run
  on Windows or macOS from the development environment. Rewriting it now would replace
  code that is in use with code that nobody has run. A Go client is the documented next
  step (`docs/PRODUCTION_TODO.md`). This decision can be revisited.
* **Screenshots travel as bytes, never as paths.** The server may run on another PC.
  The old design had the server open the path that the agent named, which was an
  arbitrary-file-read risk. The status reply now carries `screenshot_png_b64`, and the
  server checks the bytes before use. See `docs/EXECUTOR_PROTOCOL.md`.
* **Two languages on the machine: Go and Python**, plus the TypeScript dashboard.
  Rust and C++ were removed from the shipped path, and `tools/ci/repo_checks.py`
  fails if they come back. The history is in `CHANGELOG.md`.
* **One setup step, one shared secret file.** `neuro-integration setup` creates the
  relay token, which the relay host and the server both read. The dashboard never pushes
  tokens to another process. The dashboard token is printed once and kept only as a hash.
  This answers upstream issue Nakashireyumi/neuro-desktop#16, which asked how the UI and a
  relay that the bundle did not ship keep their tokens in step.
* **Development builds are separate.** The unsigned-extension switch
  (`NEURO_EXTENSIONS_ALLOW_UNSIGNED`) compiles in only with `-tags neurodev`, which the
  development bundle uses. A release build has no way to turn it on, and it logs once
  that the variable is ignored.
* **The server does not start other programs.** It does not start a relay, and it
  does not supervise the agent. Operators run each process (or the bundle's
  `start.sh` / `start.bat`) and point the server at the others.
* **Native manager, optional browser dashboard.** The GTK manager runs independently
  of the server. It polls only the visible page, backs off when unfocused or offline,
  and never polls vision. The server and client can run headlessly with
  `NEURO_NO_MANAGER=1`. See `desktop/apps/neuro-manager/README.md`.
* **Headless first.** On a machine with no display (`NEURO_HEADLESS=1`), the agent
  runs its command-line parts and the server still serves the dashboard and the
  policy. CI runs the agent with no display as a regression test.
* **The default is closed.** Without a policy file the server uses a built-in
  default, and an action is denied unless its scope is on. Input, process,
  network, vision, and game are on; filesystem, system, shell, and extensions are
  off. `desktop_guide`, `reset_controls`, and `request_permission` are always
  allowed. Shell and system access need explicit consent.

## Security boundaries

* Client connections require the executor token (`NEURO_EXECUTOR_TOKEN`). The link is
  plain TCP until the pinned-TLS work in `docs/PRODUCTION_TODO.md` lands.
* The dashboard program adds no token and holds no secret. It forwards the caller's
  `X-ND-Token` (or `Authorization: Bearer`) to the server unchanged, and the server
  decides. It forwards only `/api/` and `/health`.
* The server reads no file that a client names (see screenshots, above).
* Every dashboard API call needs the dashboard token that `setup` makes. There is no
  exception for loopback. The server keeps only the token's SHA-256 hash.
* The relay refuses browsers, binary frames, and weak tokens. Its token comes from the
  relay token file that `setup` writes, which the server reads too. See `docs/RELAY.md`.
* Extensions must have a valid signature from a publisher in `publishers.json`.
  See `docs/SAFETY.md`.

## Testing

| Layer | Command | Where it runs |
| --- | --- | --- |
| Server | `go test -race ./...` | CI on Linux, Windows and macOS (race detector on Linux) |
| Server, development build | `go test -race -tags neurodev ./...` | CI on Linux |
| Relay, MCP, vision, catalog, eval | see `.github/workflows/ci.yml` | their own CI jobs |
| Agent (client) | `python3 -m unittest discover backend/python/tests` | CI, with no display |
| Dashboard program | `go vet` and `go test ./...` in `desktop/apps/neuro-dashboard` | CI, all three systems |
| Client build | `desktop/scripts/build-client.sh`, then `neuro-client --help` | CI on Linux (`client-build` job) |
| Repository | `python3 desktop/tools/ci/repo_checks.py` | CI, first job |
| Dashboard | `npm run build` in `desktop/frontend` | CI |
| End to end | the `agent-e2e` job starts the server and the agent | CI |

What is **not** verified here: the client on a real Windows or macOS desktop
(input, screen capture), the PyInstaller build on Windows and macOS, and real Windows/macOS desktop input and screen capture. The native manager and
browser dashboard were exercised visually on Linux. Cross-platform manager builds
and GTK UI tests have their own CI matrix; local Linux execution alone does not
verify other systems.

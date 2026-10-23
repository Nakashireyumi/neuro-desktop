# Changelog

All notable changes to this project should be documented in this file.

This project aims to follow semantic versioning once stable releases begin.

## [Unreleased]

### Added

- Built-in Vision Service controls in native and browser Extensions, with persistent global/per-twin choices, fresh operator approval, and explicit bridge personas. Disabling cancels pending analysis and prevents stale screenshot results without stopping an external vision process.
- Native GTK 4 manager with a real startup splash, standalone packaging, bounded visible-page refresh and restricted Neuro/Evil sessions; the browser dashboard remains available.
- Reviewed marketplace index on a separate catalog branch, pinned source staging, per-extension runtime/management grants and settings, SDK game-presence leases, and opt-in actor-specific viewer feeds.
- Separate integration SDK and Vencord adapter repositories, cross-platform native UI/package CI, and a gated draft-release workflow with checksums and provenance.
- Dashboard operator controls on every tab, a filterable Activity log, and separate Neuro API connection status.
- A Go end-to-end test using the real headless Python client: telemetry, command output, pause enforcement, input release, and resume. Frontend API and shell bundle regression tests run in CI.
- Browser request deadlines, automatic connection recovery feedback, and in-memory sign-in when session storage is unavailable. Failed dashboard sign-ins are audited without credentials.
- Linux/macOS bundle rebuilds preserve operator state; the development launcher starts the local client and can use the project's Python environment.
- Linux GUI loading tolerates missing Tk for the optional MouseInfo inspector. Other GUI failures report their cause and keep shell/telemetry running; status no longer reports stub geometry as a real screen.
- Native Wayland screenshots through `grim` on supported compositors, with capture errors included in desktop context. Verified locally on Hyprland; Windows/macOS capture still uses MSS.

- **Go client, slice 1** (`headless/apps/neuro-client-go`, not shipped yet): the start of the port of the Python client, which the maintainer asked for. It has the executor link (protocol 2, pings, commands, shutdown, reconnects, the same frame limits), the headless rules, `get_status` with the keys the server reads, and `shell_command` with the same firewall, allowlist, deny patterns, timeout and output limit as `shell.py`. Input, screen, window and `run_script` commands are refused with a message that says where they still run. A test fails if the Python agent runs a command the Go client neither handles nor refuses. It cross-compiles for Windows, macOS and Linux, and CI runs its tests on all three.
- **Three programs** (decision recorded in `docs/ARCHITECTURE.md`): the server, the dashboard program, and the client for the PC Neuro controls. The split is at the process boundary, and the client is not rewritten in Go yet.
- **Dashboard program** `headless/apps/neuro-dashboard` (Go, stdlib only): serves the built dashboard page and forwards only `/api/` and `/health` to the server. It adds no token and keeps the caller's. It answers `502` when the server is down. Flags `--listen`, `--server`, and `--ui-dir`, with `NEURO_DASHBOARD_LISTEN`, `NEURO_DASHBOARD_SERVER`, and `NEURO_UI_DIR`. Tests: `dashboard_test.go`.
- **Client build** `desktop/scripts/build-client.sh` (and `build-client.ps1`): packages the Python agent with PyInstaller (pinned, 6.22.3) as `neuro-client`, with its own Python inside. Entry point `headless/apps/neuro-client/neuro_client.py`. The release bundle builds it when it can, and the launcher uses the Python agent when it cannot.
- **CI**: a `client-build` job (builds `neuro-client` on Linux and checks that it reports a missing server), and a dashboard vet and test step in `build-and-test`.
- **Setup** (`neuro-integration setup`, with `--check` and `--rotate-dashboard`): the first-run step. It creates the relay token that the relay host and the server share (`relay-token`, mode 0600), then the dashboard token. The dashboard token is printed once, and only its SHA-256 hash is stored (`dashboard-token`, mode 0600). Running it again keeps what exists. Exit codes: 0 complete, 1 something to fix, 2 bad usage. `setup_test.go` covers the first run, a second run, rotation, and the mismatch checks.
- **Dashboard sign-in**: the dashboard shows a sign-in page until the server accepts the dashboard token. `GET /api/session` checks a token. The browser keeps the token in `sessionStorage`, for that browser session only.
- **Development build tag** `neurodev`: the unsigned-extension switch is compiled in only when the tag is set. `dev.sh` and `dev.ps1` build with it. `/api/config` features now report `dev_build` and `unsigned_extensions_allowed`.
- **CI**: a `Go tests, dev build (-tags neurodev)` step and a `Setup smoke test` step, both on Linux. The smoke test runs `setup` twice and checks the file modes.
- `.gitignore` covers `dashboard-token`, so the hash is not committed by accident.
- CI `bundle` job: builds the release bundle, checks that it contains what the launcher needs, verifies the shipped catalog from inside it, and serves the dashboard from the bundle folder. The release gate requires it.
- `headless/apps/nd-vision-server/README.md` and `requirements.txt`: the vision HTTP contract, its backends, and the optional Pillow and tesseract.

- **Neuro Relay host** (`neuro-integration relay`, Go). It replaces the Python relay shim and the bridge no longer starts any relay process. It checks the token on registration, refuses browsers (any `Origin` header) and binary frames, limits frame rates, and reports `/health`. The bridge is a relay client. Covered by `relay_host_test.go`, an end-to-end test of the real client against the host, and a CI smoke test.
- **Signed extension catalog**: `desktop/catalog/index.json` and `desktop/catalog/publishers.json`. The bridge verifies every signature and refuses unsigned items unless `NEURO_EXTENSIONS_ALLOW_UNSIGNED` is set. `neuro-integration catalog verify` checks the shipped index.
- **MCP bridge**: starts the MCP servers Vedal enables and exposes their tools as actions, gated by the `extensions` scope. The shipped catalog item is `memory`, the pinned `@modelcontextprotocol/server-memory`.
- **Permission requests**: Neuro can call `request_permission` for a scope that Vedal marked requestable. Vedal approves or denies it on the Permissions page.
- **Live runtime state** on the Extensions page: bridge, relay, vision server, and MCP bridge. Install state is shown second.
- **Vision**: `NEURO_VISION_URL` (`NEURO_VISION_SERVER_URL` is still read as an alias), with a live reachability probe, and the `nd-vision-server` Python service.
- **Escape hatch** `reset_controls`: no parameters. It clears queued inputs, then releases every held key and button. It is always allowed, runs while the bridge is paused or killed, and works under a deny-all policy. The reply says what worked and what may still be held.
- `desktop_guide` is always allowed. Refusals name the action and give a valid example, so a small model can correct itself on its next turn.
- **Small-model eval**: `headless/apps/neuro-integration/testdata/weak_model_cases.json` holds recorded weak-model calls with the expected verdict and reply. It runs in `go test` and in the `weak-model-eval` CI job.
- **CI**: `relay-host`, `mcp`, `vision`, `catalog-verify`, and `weak-model-eval` jobs, and a `release-gate` job that fails unless every required job succeeded.

- **Python agent** (`desktop/python/controller/agent.py`): the process
  that executes commands on the machine Neuro controls, replacing the Rust
  executor. Speaks the executor protocol over TCP (connecting out or listening)
  and over the bridge's file-IPC fallback, honours `execute_now`/`clear_after`,
  answers pings, and refuses input on a headless machine with a message that
  points at `shell_command`.
- `docs/ARCHITECTURE.md` — the consolidation decision, the migration table, and
  what is (and is not) on the shipping path.
- `docs/EXECUTOR_PROTOCOL.md` — the server↔agent wire format, the command table,
  queue semantics and file-IPC rules.
- `docs/RELAY.md` — how Neuro Desktop coexists with other integrations through
  Neuro Relay, including the two modes and the upstream limitation that actions
  registered on the intermediary never reach Neuro.
- `docs/LLM_GUIDE.md` and `docs/SAFETY.md` — prompting tactics for small models,
  and every guard with where it lives and how to verify it.
- `desktop/scripts/bundle/templates/` — checked-in `start.sh`, `start.bat` and
  `README.txt` for the release bundle, so the launchers can be syntax-checked.
- `desktop/scripts/build-go.ps1` cross-compiles the server for Windows, Linux and
  macOS; CI cross-compiles the same targets on every change.
- `agent-e2e` CI job: starts the server and the agent together and asserts the
  dashboard sees the agent, so a protocol regression between the two languages
  fails the pipeline.
- Executor keepalive on the hub: a 30-second ping with a 90-second drop, which
  removed a reconnect-per-2-minutes loop (`total_connections` used to climb
  forever).
- `desktop/tools/ci/repo_checks.py`: bundle-template validation, a stale-path
  checker (deleted `native/*` paths may not be referenced any more), and
  documentation link checks for the three new guides.

### Changed

- The macro language is named **Neurcro**; the product remains **Neuro Desktop**. Existing executable names, API actions, scripts and configuration stay compatible.
- Queued game input is tied to its original session and discarded after a session ends or changes. Vision request/response deadlines also stop peers that trickle bytes.

- Executor disconnects clear pending work, release held input, and close monitor listeners. File IPC atomically claims commands, correlates each result with its bridge session and request, refuses incompatible clients, and preserves pre-existing files during startup.
- Windows manager credential fields use GTK's simple input method to avoid a stale native IME callback when replacing signed-in views; ordinary text fields retain their configured input method.
- Installed SDK packages are tested against the real server on Linux, Windows, and macOS, including approval, presence leases, viewer activity, and revocation.
- Macro scripts are preflighted before side effects, preserve keyboard/mouse order, wait once, and execute launches, shell commands, and releases at their source position. Nested commands retain their individual permission checks; retained queues recheck approvals before execution and have bounded size and duration.
- Desktop observations honor explicit vision disablement and recheck live approval around capture, inference, and delivery. Periodic context cannot bypass a revoked vision permission; analysis response reads are bounded.
- Expiring scope approvals stop affected managed MCP services without polling. Permission changes and pause clear viewer consent/history; reapproval cannot restore previously shared events.
- Slow SDK request bodies and response streams no longer hold the shared adapter runtime lock. Native manager sign-out clears open approval dialogs and delayed token responses; authentication requests ignore ambient HTTP proxies.
- **Screenshots travel as bytes, never as paths.** `get_status` returns `screenshot_png_b64` (a PNG scaled to at most 1 600 px and 3 MiB, then base64). The server no longer reads a path that the client names, which was an arbitrary-file read when the server ran on another PC. The server checks the PNG signature, the base64, and the 8 MiB limit. The vision service receives `image_base64`; the server no longer sends `image_path`.
- **Executor frame limits:** the hello is capped at 64 KiB, because it is read before the token check. Later frames are capped at 8 MiB. A frame over its limit closes the connection, so the hub never buffers without bound.
- **Launchers** (`start.sh`, `start.bat`): run `neuro-client` when the bundle has it, else the Python agent. The bundle README describes the three programs.
- **Security:** every `/api` route except `/health` needs the dashboard token, with no exception for loopback. The token travels in the `X-ND-Token` header or as a Bearer token. A `?token=` query parameter is not read. The shell is served with no token and with `Cache-Control: no-store`. Before, reads were open on loopback, and the token was injected into the page as `window.__ND_BOOTSTRAP`.
- **Relay token source:** the relay host and the server read one file, `NEURO_RELAY_TOKEN_FILE` (default `./relay-token`). `NEURO_RELAY_TOKEN` and `NEURO_RELAY_AUTH_TOKEN` are overrides, and each must equal the file. A missing or disagreeing token leaves the relay link off and says why; the rest of the server keeps running. A relay token file that is present but invalid is reported and never overwritten.
- `NEURO_ADMIN_TOKEN` is now an override of the stored dashboard token. It must be at least 16 characters, and it must match the stored token when both exist.
- **Unsigned extensions:** `NEURO_EXTENSIONS_ALLOW_UNSIGNED` works only in a development build. A release build ignores it and logs once that it is ignored.
- Docs: the README, `desktop/README.md`, DEPLOYMENT, RELAY, SAFETY, TROUBLESHOOTING, ARCHITECTURE, CAPABILITIES, LLM_GUIDE, PRODUCTION_TODO, and the bundle's README and launchers describe the setup flow and the token rules. Stale references are gone: a Rust `target/release` path, a `setup-dev.ps1` that does not exist, and the `NEURO_ADMIN_TOKEN=demo` examples.
- **Security:** the executor hub refuses to listen beyond loopback without `NEURO_EXECUTOR_TOKEN`. Before, a hub with no token accepted any client as the executor, and that client received Neuro's commands. Only a warning was logged.
- **Security:** watcher commands that arrive through the relay share the per-scope rate limit with Neuro's actions. Before, they skipped it. The gates are in one place (`denyRelayCommand`) and are tested.
- `reset_controls` now goes through the same accept-then-execute path as other input actions, and it reports its outcome to Neuro whether it worked or not.
- The vision hint in an observation says whether `NEURO_VISION_URL` is missing or the server did not answer. Before, both said to set the URL.
- Documentation rewritten for the current system: `ARCHITECTURE`, `RELAY`, `DEPLOYMENT`, `TROUBLESHOOTING`, `SAFETY`, `LLM_GUIDE`, `PRODUCTION_TODO`, and the README, desktop README, and contributing guide. Each was checked against the code. The `DEPLOYMENT` and `TROUBLESHOOTING` guides lost sections that could not be verified here, such as the Kubernetes and monitoring examples.

- Example permission policy (both copies, kept identical by `repo_checks.py`): `extensions` and `filesystem` are off and requestable; `shell` and `system` are off and not requestable; `install_extension` moved from the deny list to the `extensions` scope.
- Relay status reports `disabled`, `disconnected`, `unreachable`, `connected`, `idle`, or `registered`. The process fields (`process_restarts`, `process_managed_here`, `supervised_by`) are gone.

- The shipped product is **Go server + Python agent + TypeScript dashboard
  client**; the Rust executor and the C++ supervisor are no longer part of the
  build, the bundle, or the CI gate (they remain as reference code).

### Removed

- The agent's `--listen` mode. Nothing connects to it: the server only accepts agents that dial the hub. It bound to `0.0.0.0` by default and executed commands from any peer, with no check on the peer's identity. Agents connect out with `--bridge`, which the hub's token protects. A reverse connection is a design question, not a default, and it is not planned.
- `desktop/config/integration-config.yml`. Nothing read it: the server takes its settings from environment variables and flags. Its ports and package block were stale.

- The bridge no longer starts a relay process. `NEURO_RELAY_COMMAND`, `NEURO_RELAY_CONFIG`, and `NEURO_RELAY_MAX_RESTARTS` are gone, and so is the restart supervision they fed.
- The relay shim (`desktop/tools/relay-compat/`), replaced by the Go relay host.
- The C++ process handler and the Rust executor sources. Nothing in the shipped path used them.
- The stale catalog entries. `desktop/catalog/extensions-state.json` is runtime state and is no longer tracked; it is ignored.

- `desktop/native/{go,c_cpp,rust-core}` — hello-world placeholders that were
  referenced by documentation describing a layout that never existed.
- The root `tests/integration/*.js` files — Jasmine-style tests with no runner
  (they cannot be executed at all).
- The Rust `relay_manager` invocation and the `build-all.ps1` "relay build" step
  (both launched a `neuro-relay` CLI that does not exist), plus every pointer to
  the placeholder Go module under `desktop/native/` that was never written.

### Fixed

- The bundle launcher used `exec` for the server, so its cleanup trap never ran and the local agent could outlive the server and keep the port busy. The server now runs as a child, and the trap stops the agent.
- CI: the `agent-e2e` and `bundle` jobs called the dashboard API without a token, and got `503` since the token rule. They now send a fixed `NEURO_ADMIN_TOKEN`, and the bundle job checks that the dashboard program refuses a request with no token.
- The relay host says `generated a new relay token` when it creates the token file, and the CI check matches that wording.
- Issue #16 (Nakashireyumi/neuro-desktop): the relay and the server no longer depend on a token copied by hand. `setup` writes the one file that both read, and `setup --check` says when they disagree.
- Windows: the audit log is opened per write, so nothing holds it locked; a relay host refusal is no longer reported as a connection reset, because the host waits for the client to read it first.
- macOS: the test binaries are built without cgo, which the server does not need. They no longer fail to load (`missing LC_UUID`).
- CI: the Go format check no longer depends on `mapfile` or `find`, and sources keep LF line endings on every platform (`.gitattributes`).
- The catalog test fixture used a wrong upstream repository URL.
- The relay client's comment still named a removed environment variable.
- `permissions.example.json` (both copies, kept identical): the deny list no longer blocks `install_extension`, which the `extensions` scope now governs; `filesystem` and `extensions` are requestable; `shell` and `system` are off and not requestable.

- The runtime endpoint no longer panics when there is no executor hub.
- When nothing is listening on the relay address, the relay status reports `unreachable` instead of `disconnected`.

- The relay link reported **its own registration** as a coexisting integration
  (the relay echoes `integration_connected` back), so `peer_count` could never
  reach zero and game delegation could point at ourselves.
- A rejected relay registration now reports the relay's own error text
  ("invalid auth token") with a hint at `NEURO_RELAY_TOKEN`, instead of a bare
  "connection failed" that read like a network problem.
- The bridge no longer writes a stale executor result: replaced clients are
  counted (`replaced_connections`) and closing one no longer looks like a crash.
- `sendToExecutor` refuses commands the bridge handles itself, so a forgotten
  command fails loudly instead of silently doing nothing.
- Baseline Windows CI workflow for Rust, Go, Node, and C++ validation.
- Python parser tests for high-level commands and click syntax.
- Go tests for integration documentation loading.
- `run_script` JSON schema (`action-schema.run_script.json`).
- Production backlog and issue verification tracker (`docs/PRODUCTION_TODO.md`).
- Project vision document (`VISION.md`).
- Relay participation built into the Go bridge (register as a relay integration,
  report peers, reserved action names) — replaces the old Rust "relay process
  manager", which invoked a `neuro-relay` CLI that does not exist.
- Docker-based modular test setup (`docker-compose.tests.yml`).
- Optional relay bundling support in PowerShell bundle/build scripts.
- Game interface: game profiles (`desktop/catalog/games`), sessions with
  `nd`/`external`/`hybrid`/`auto` control modes, and `game_*` actions (move, look,
  press, observe, release all).
- Live operator dashboard: permissions editor, extensions manager, games panel and
  status page, served by the bridge at `/ui/`.
- Per-scope actions-per-minute limits, enforced with a sliding window.
- `desktop/tools/fake-executor` protocol simulator for dashboard development.
- `/api/actions` now returns each action's JSON schema plus a plain-language
  `params` rendering and its required list, so the dashboard (and a weak model)
  can see parameter shapes before a call fails. Guarded by
  `TestActionsExposeLLMFriendlyMetadata`.
- `docs/LLM_GUIDE.md`: prompting tactics and parameter shapes for small/weak
  models, and `docs/SAFETY.md`: every safety system, where it lives and how to
  verify it. `desktop/tools/ci/repo_checks.py` fails if they disappear or stop
  being linked from the README.
- Go tests for the executor hub, game profiles, permissions/rate limits, the admin
  API and a full bridge end-to-end run against a fake Neuro backend.
- Python protocol tests for the game input primitives.
- Headless support: `NEURO_HEADLESS` (or simply no `DISPLAY`/`WAYLAND_DISPLAY`)
  now degrades `pyautogui`/`pynput`/`mss`/`pygetwindow` into self-explaining stubs
  instead of failing at import, so the controller runs on a CLI-only OS.
- `shell_command` action plus the `shell` permission scope: one command line per
  call, returning the exit code and truncated output, for headless machines and
  terminal work (`SHELL "…"` script verb too).
- Shell firewall: program allowlist (`NEURO_SHELL_ALLOWLIST`), built-in destructive
  patterns, `NEURO_SHELL_DENYLIST`, timeouts and output caps, enforced in the Go
  bridge and again in the Python executor.
- Safety systems: `NEURO_PAUSED` / `POST /api/control/pause|resume`, a
  kill-switch file (`NEURO_KILL_SWITCH_FILE`), a hard deny list
  (`NEURO_DENY_ACTIONS`) that dashboard edits cannot lift, and a JSON-lines audit
  log (`NEURO_AUDIT_LOG`) with `GET /api/audit?limit=N`.
- `desktop_guide` action and a startup context push, so weak/small models get an
  instruction sheet (order of operations, parameter names, script syntax).
- `relay_protocol_test.go`: a fake intermediary that speaks what upstream
  `intermediary.py` speaks, pinning registration, action announcement, watcher
  commands, peer tracking and bad-token handling.
- CI: repository checks (JSON/catalog/example-policy/doc consistency) and a
  stdlib-only Python job that proves the CLI-only install; the main workflow now
  also runs `go build`, `go vet` and `go test -race` on Linux.

### Changed

- Action script supports high-level desktop commands.
- Integration docs loading is now non-fatal and supports fallback paths.
- Action script documentation updated to match runtime behavior.
- Go integration migrated to `neuro-integration-sdk` with local compatibility patch.
- High-level Windows action coverage expanded (Explorer, Run, Search, snap-left, snap-right).
- Rust executor validation in CI is advisory (`continue-on-error`) until the crate
  is confirmed building on all three runner platforms.
- Relay coexistence guidance: pointing the bridge's Neuro client at the relay's
  Nakurity Backend is documented as the supported "run alongside an integration"
  path; the intermediary registration is documented as the watcher/visibility path.

### Fixed

- Relay: the bridge counted the relay's echo of its own registration as a
  coexisting integration, so `/api/relay` reported a phantom peer and the
  "another integration owns this game" hints could point at itself.
- Relay: a bad `NEURO_RELAY_TOKEN` is now reported deterministically even when
  the relay's error frame arrives before the follow-up writes fail.
- Relay: a successful registration clears `last_error`, and closes that we cause
  ourselves (shutdown/reconnect) are no longer reported as failures.
- Executor hub: replacements are counted (`replaced_connections`, shown by
  `/api/status`) and the replacement log is rate-limited — two executors running
  at once used to produce thousands of identical lines and look like a network
  problem. The fake executor now backs off when the bridge drops it instead of
  reconnecting in a hot loop.
- `ollama-neuro` imports `aiohttp` lazily, so its parser tests run on a CLI-only
  install with no dependencies.

- Concurrent websocket writes in the vendored Neuro SDK (action results racing the
  read loop); caught by `go test -race`.
- Executor responses are published atomically (write + rename) so the bridge can no
  longer read a half-written response file.
- Dashboard counters (`actions_seen`/`actions_failed`/`actions_denied`) were never
  incremented, so the status API always reported zeros.
- `max_actions_per_minute` in a permission policy used to be ignored.
- A policy file with `"version": 1` (number) or `"input": true` (shorthand) no
  longer prevents the bridge from starting.
- PyO3/Python compatibility blocker for local Python 3.14 environments.
- Mismatch between action docs and `CLICK` parser behavior.
- The relay link reported success the moment it wrote its registration, so a wrong
  `NEURO_RELAY_TOKEN` looked like an unexplained disconnect; the relay's
  `{"error": "invalid auth token"}` is now surfaced with the setting to check.
- Action announcements to the relay now use the name -> schema mapping that
  `intermediary.py` stores (a flat description string was silently useless).
- The bridge listed itself as a relay peer (the relay echoes every connection
  event), which made the external-control message point at the wrong integration.
- A policy with `default_allow: true` (or a bare allow-list entry) could hand out
  the new shell capability; `shell` and `system` now require explicit scope consent.
- `shell_command` parameters that are missing, empty, or sent as the wrong type
  are answered with the expected shape instead of a generic refusal.
- The relay link reconnected every ~10 seconds: the registration was probed with
  a short read deadline, and gorilla keeps the first read error forever, so a
  quiet relay poisoned the socket. Liveness is now checked with websocket
  pings, and a rejection frame (or a write racing it) is reported as
  "relay rejected the registration" with the setting to check.
- `shell_command` output was dropped after the action was acknowledged; the
  transcript is now delivered to Neuro as context.

## Earlier Neuro-OS implementation

The earlier Windows-focused Neuro-OS line included window regionalization, local OCR,
and an optional Nakurity vision backend. Original development was credited to
Nakashireyumi, with OCR and backend work credited to the earlier contributors.
That Python/Flask launcher and its root installation path are retired; use the current
[architecture](docs/ARCHITECTURE.md), [setup](desktop/README.md), and
[vision service](headless/apps/nd-vision-server/README.md) documentation. The historical
implementation and its original change log remain available in Git history.

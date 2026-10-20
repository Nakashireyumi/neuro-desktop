# Production TODO

Last updated: 2026-10-18. Each status below was checked against the code in this
repository. `[x]` done, `[~]` partly done (what is missing is written next to it),
`[ ]` open, `[-]` declined with the reason.

## Upstream trackers

Check these directly; this file does not keep a copy of their issue lists.

- This project: `cassitly/neuro-desktop`
- Neuro Relay (the upstream this relay interoperates with): `Nakashireyumi/neuro-relay`
  - Open upstream item that is not fixed here:
    [#6 Nakurity ID Server](https://github.com/Nakashireyumi/neuro-relay/issues/6).
    It belongs to the relay backend and identity architecture, not to the Go host.
- Neuro API: `VedalAI/neuro-sdk`
- The Go SDK port this server uses: `cassitly/neuro-integration-sdk`

## Remaining production work

- [x] **Permission model and runtime enforcement** (#12/#20). Scopes carry `allowed`
  and `requestable`. The default is closed. Per-scope rate limits apply to Neuro and
  to watchers. The stop switch and the kill-switch file block Neuro's actions and watcher
  commands; input release and status stay allowed. Neuro can
  ask for a requestable scope, and Vedal decides on the Permissions page. Tests:
  `permissions_test.go`, `permission_requests_test.go`, `ratelimit_test.go`,
  `relay_host_test.go`. Approvals are in memory, so a restart clears them.

- [x] **UI token synchronization and relay-first onboarding** (#16). The dashboard
  pushes no tokens to other processes, and it no longer injects its token into the page.
  `neuro-integration
  setup` creates the relay token, which the relay host and the server share through one
  0600 file. Then it creates the dashboard token, prints it once, and keeps only its hash.
  `setup --check` confirms that the two sides agree. The dashboard asks for its token on a
  sign-in page, and every `/api` route needs it. Tests: `setup_test.go`,
  `relay_token_test.go`, `dashboard_token_test.go`, `admin_server_test.go`. CI runs a
  setup smoke test. Native and browser sign-in and delegated management were
  exercised on Linux. Windows/macOS packaging and GTK checks are CI requirements;
  real input and capture on those desktops still need device validation.

- [x] **Separate server, native manager, web dashboard, and controlled-PC client.**
  The server (`neuro-integration`, Go) keeps the policy, the Neuro connection, the
  dashboard API, and the vision client. The dashboard program (`neuro-dashboard`, Go,
  stdlib only) serves the page and forwards only `/api` and `/health`, with no secret of
  its own. The client (`neuro-client`) is the Python agent packaged with PyInstaller by
  `scripts/build-client.sh` (`build-client.ps1` for Windows). It holds no policy, no
  dashboard, and no vision. The native GTK manager is its own process with a splash
  and no embedded browser. A delegated manager session lets Neuro manage approved
  integrations without granting new capabilities. The server still serves `/ui/` for
  single-machine use. Tests cover the dashboard proxy, scoped sessions, the actual GTK
  controls, and executor transport. Real Windows/macOS desktop input remains unverified.

- [~] **Client rewrite in Go, in progress.** The maintainer chose to start it now. Slice 1
  is in `headless/apps/neuro-client-go` (Go, standard library only; cross-compiles for
  Windows, macOS and Linux). It covers the executor link (hello, ping, commands, shutdown,
  reconnects, the same frame limits), the headless rules, `get_status` (the same keys the
  server reads), `shell_command` (the same firewall, allowlist, deny patterns, timeout and
  output limit as `shell.py`, with the timeout capped at 120 s), and the lifecycle
  commands. It is not in the bundle. A test fails if the Python agent runs a command that
  the Go client neither handles nor refuses. Still to port, in this order: (1) mouse and
  keyboard input with the headless refusal, per platform; (2) screen capture as PNG bytes
  and window and process telemetry; (3) the action-script runner (`run_script`) and the
  input queue; (4) file IPC for co-located use; (5) packaging the Go client as
  `neuro-client` in the bundle, replacing the PyInstaller build. Steps 1 and 2 must be run on
  Windows and macOS before they are trusted. The Python client is retired only when steps
  1 to 4 pass on all three systems.

- [~] **Verify the Windows and macOS bundles and launchers.** CI now requires native
  GTK builds/tests on all three operating systems and Windows bundle state-preservation
  checks. Both CI runs at `433f336` passed native GTK controls, frozen-manager
  startup and packaging on Linux, Windows and macOS. The release workflow builds
  four native targets and checks the packaged
  client/server transport. These checks do not establish that input permissions,
  screen capture, signing, or installation work on a viewer's actual desktop.

- [ ] **Authenticated reverse connections, and pinned TLS for the executor link.** The
  design allows the server to dial the client, and it requires that connection to be
  authenticated. The plan is a certificate fingerprint pinned on the dialing side, with
  the executor secret sent only over that pinned channel, and the dashboard API served
  the same way. Not built. The executor link is plain TCP today (`docs/SAFETY.md`,
  section 6).

- [x] **Log failed dashboard sign-ins.** Rejected requests write `dashboard_auth`
  decisions to `NEURO_AUDIT_LOG`, with the method and path. Headers, tokens and
  query parameters are excluded. The Activity tab shows these alongside actions
  and pause/resume events. Test: `TestDashboardRejectedTokenIsAuditedWithoutSecrets`.

- [x] **Check a real screenshot end to end on Linux.** Verified on Hyprland using
  `grim`: the real Python client sent PNG bytes through the Go server to a local
  `nd-vision-server` (`stats` backend), and the dashboard's observation showed the
  capture and analysis. Native capture on Windows/macOS and semantic analysis
  against a vision model remain unverified.

- [x] **License file.** Restored upstream's GPL version 3 license and corrected the
  README. The independently authored integration SDK is MIT; the Vencord adapter
  carries GPL-3.0-or-later. Marketplace listings record their own source licenses.

- [~] **Integration manager, marketplace, and signed plugin distribution** (#11/#14/#15).
  Done: a signed catalog (`desktop/catalog/index.json`), publisher keys
  (`publishers.json`), verification on every install, `neuro-integration catalog verify`
  in CI, one managed MCP item (`memory`, pinned), and a public eight-entry source
  marketplace on upstream's independent `catalog` branch. The standalone SDK supports
  existing private backends and Games ownership; grants and settings are per item.
  Listings with missing/custom licenses remain link-only. Still needed: additional
  managed publishers and a key-rotation procedure. Rotate the shipped publisher key
  before relying on it for external distribution.

- [~] **Vision pipeline confidence loop and formalized server contract.** Done: the HTTP
  contract is in `headless/apps/nd-vision-server/README.md`, and the bridge's client is
  tested against it. Missing: a confidence loop (re-capture, or a score the bridge can
  act on).

- [~] **Security hardening: signed bundles, config integrity, least-privilege defaults.**
  Done: least-privilege defaults, signed extensions, scoped adapter/viewer credentials,
  delegated manager sessions, and fresh operator approval. The draft-release workflow
  produces checksums and GitHub provenance. Missing: platform code signing/notarization
  and an integrity check on operator configuration files.

- [~] **Native manager and splash.** GTK 4 desktop application implemented and
  exercised on Linux, including all pages, pause/release/resume, sign-in and policy
  editing. Standalone GTK packaging, native controls and frozen startup passed
  all three OS runners at `433f336`; real desktop input/capture still needs device
  validation. A notification-area
  tray is separate optional work; it is not required to run the native manager.

- [~] **Release quality gates.** Done: the `release-gate` job in CI, which fails unless
  every required job succeeded; repository checks; the race detector; cross-compiles for
  Windows, macOS, and Linux; the weak-model eval; the relay smoke test; and the MCP,
  vision, and catalog jobs. Missing: a coverage target, installer validation (the bundle
  is built, not installed), and a changelog check that is more than a manual review.

## Completed

- Screenshots travel as bytes, never as paths. The status reply carries
  `screenshot_png_b64`, and the server checks the signature, the base64, and the size
  (8 MiB) before use. The server no longer opens a path that a client names, which was an
  arbitrary-file read when the server ran on another PC. Tests: `screenshot_test.go`,
  `vision_test.go`, `test_screenshot_bytes.py`.
- The executor link limits frames: the hello at 64 KiB (read before the token check), and
  every later frame at 8 MiB. Tests: `executor_frame_test.go`.
- The relay host says when it generates its token, and the first-run check in CI matches
  that wording.
- The bundle launcher prefers `neuro-client` and stops the agent when the server stops.
  The old launcher's `exec` skipped the cleanup trap, so the agent could outlive the
  server and hold the port.
- The Go server: Neuro client (vendored SDK port), executor hub, dashboard API,
  permissions, audit, stop switch, and game interface.
- The Python agent (`controller/agent.py`): input, high-level desktop intents, scripts,
  the shell with an allowlist and a firewall, and telemetry.
- The action-script language and its high-level commands, with parser tests, and the
  `run_script` payload schema.
- Integration docs loading, which is non-fatal and has fallback paths.
- Neuro Relay: the built-in host (`neuro-integration relay`) and the server's client, with
  relay-backed routing and relay state on the dashboard.
- Catalog actions (`list_catalog_items`, `find_catalog_items`, `get_catalog_item`) and the
  extension actions (install, enable, disable, uninstall, list), with a state file.
- Desktop context: the agent's snapshot, and the server's periodic context sender
  (`NEURO_CONTEXT_POLL_SECONDS`).
- The vision client (`NEURO_VISION_URL`) and `nd-vision-server`.
- The dashboard (React and Vite, TypeScript): status, permissions with requests,
  extensions with live runtime state, and games.
- Headless and command-line-only operation.
- Removed: the Rust executor, the C++ process handler, the relay shim, and the `native/`
  placeholders. The history is in `CHANGELOG.md`.

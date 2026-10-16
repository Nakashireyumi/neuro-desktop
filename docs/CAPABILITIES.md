# Neuro Desktop — Current Capabilities

Honest snapshot of what works today (post bridge/executor split). This is not a
roadmap; see [VISION.md](../VISION.md) and [PRODUCTION_TODO.md](PRODUCTION_TODO.md)
for direction and backlog.

Last updated: 2026-10-09

## Architecture (what you actually run)

| Piece | Binary / path | Role |
|-------|---------------|------|
| **Bridge (server)** | `neuro-integration` | Neuro WebSocket client, permissions, TCP executor hub `:9876`, admin HTTP `:8300` |
| **Dashboard program** | `neuro-dashboard` (`desktop/apps/neuro-dashboard`) | Optional. Serves the dashboard page and forwards only `/api` and `/health` to the server. No secret of its own |
| **Client** (`neuro-client`) | `desktop/backend/python/controller/agent.py`, packaged by `desktop/scripts/build-client.sh` | Mouse/keyboard/scripts/shell on the controlled machine (the only part that touches it). Runs from source too |
| **Local Neuro mock** | `desktop/tools/ollama-neuro` | Randy-like tester using Ollama + `heredos/rwkv7:2.9b` |
| **Operator dashboard** | `desktop/frontend`, served by the bridge at `/ui/` | Live permissions, extensions, games and status |
| **Fake executor** | `desktop/tools/fake-executor` | Protocol simulator for the dashboard (never injects input) |
| **Relay host** | `neuro-integration relay` | Neuro Relay socket for other integrations and watchers (see `docs/RELAY.md`) |
| **MCP bridge** | inside the bridge | Runs the MCP servers Vedal enabled from the signed catalog, as child processes |
| **Vision server** | `desktop/apps/nd-vision-server` | Optional screen description, reached through `NEURO_VISION_URL` |

**Split machines:** the server runs where Neuro runs and does the heavy work. The client runs on the
PC Neuro should control (`neuro-client --bridge <server-ip>:9876`, or `python3 -m controller.agent --bridge
<server-ip>:9876` from source). The dashboard can run on your own PC through `neuro-dashboard`. One machine:
point the client at loopback, or use `NEURO_IPC_FILE` for file IPC. The Rust executor and the C++
supervisor were removed; see `docs/ARCHITECTURE.md`.

**Co-located:** the agent can run on the same PC. With no TCP connection, the server uses file IPC (`NEURO_IPC_FILE`).

## Platforms

| Capability | Windows | Linux | macOS |
|------------|---------|-------|-------|
| Low-level mouse / keyboard (`pyautogui`) | Yes | Yes* | Yes* |
| High-level desktop intents (start menu, snap, lock, …) | Yes (Win shortcuts) | Best-effort (DE-dependent) | Best-effort (⌘ mappings) |
| Window enumeration | Yes | Weak / optional | Weak / optional |
| Screenshots (`mss`) | Yes | Yes* | Yes* |
| Headless / CLI-only OS (no display) | Supported (`NEURO_HEADLESS` or no session) | Supported (no `DISPLAY`) | Supported (forced) |
| Command lines (`shell_command`) | Yes (allowlist + firewall) | Yes | Yes |

\* Needs a real graphical session. Do **not** run with `sudo` (breaks Wayland/X auth and file ownership). On Omarchy/Hyprland, run from your logged-in desktop session.

## Neuro API integration

Working:

- Connect / reconnect to Neuro (or Ollama mock / Randy) via WebSocket
- `startup`, `actions/register`, `action` → `action/result`, `context`
- Validate quickly, send `action/result`, then run desktop work (API ~20s result window)
- Permission policy: scopes + allow/deny lists (`permissions.json`)
- Periodic desktop context snapshots (Markdown)

Not production-hardened:

- Live Neuro vs Evil character UX beyond startup ack fields
- Voice chat side-channel
- Permission policies are local files. Catalog items are signed; see the catalog section

## Actions Neuro can use

### Always available (cannot be switched off)

- `desktop_guide` (optional `topic`): how to use the desktop, with examples. Neuro should call it first when unsure, and after any refusal.
- `reset_controls` (no parameters): the escape hatch. It clears queued inputs that have not run yet, then releases every held key and button. It runs even while the bridge is paused or killed, and it works under a deny-all policy. The reply says what worked and what may still be held.
- `request_permission` (`scope`, `reason`, optional `minutes`): asks Vedal for a scope that is switched on for requests. Vedal approves it for a chosen time, or denies it, on the Permissions page.

### Low-level (default registered on startup)

- `move_mouse_to`, `mouse_click`, `key_press`, `type_text`
- `run_script` (action-script language)
- `execute_queue`, `clear_action_queue`
- `enable_low_level_controls` / `disable_low_level_controls` (mode switch)

### High-level desktop intents (when HL mode / script intents)

OS-mapped shortcuts, including:

- Start / launcher, show desktop, minimize, close foreground app
- Task manager / Force Quit, file manager, run/search dialogs
- Snap left/right, settings, notifications, clipboard history (where supported)
- Lock workstation, app switcher, screen snip

Unsupported intents on an OS fail with a clear error (e.g. macOS clipboard history).

### Game interface (playing a game with no integration of its own)

- `game_list_profiles`, `game_detect`, `game_start_session`, `game_end_session`,
  `game_status`, `game_observe` — profiles in `desktop/catalog/games/*.json`
- `game_move`, `game_look`, `game_action`, `game_press`, `game_release_all` — bounded
  holds (≤30s), relative mouse-look with sensitivity/inversion/clamping, masked keys
  (`control.allow_raw_keys` for raw key presses)
- Per-session rate budget (`control.max_actions_per_minute`) plus the dashboard's
  per-scope `max_actions_per_minute`
- Every failed primitive releases held input, so a half-applied move cannot leave a
  key stuck down
- `game_launch` runs the profile's launch command and needs the `system` scope

### Shell (headless / terminal work)

- `shell_command {command, cwd?, timeout?}` runs one command line and returns the
  exit code plus truncated stdout/stderr — the only capability that needs nothing
  but an OS, which is what makes a command-line-only machine useful
- Program allowlist (`NEURO_SHELL_ALLOWLIST`, empty by default = run nothing) plus
  built-in deny patterns (`rm -rf /`, `mkfs`, `dd of=/dev/…`, `shutdown`, `sudo`,
  `curl … | sh`, `diskpart`, fork bombs, …); `NEURO_SHELL_DENYLIST` adds patterns
- `NEURO_SHELL_TIMEOUT` (default 20s, cap 120s), `NEURO_SHELL_CWD`,
  `NEURO_SHELL_MAX_OUTPUT` (default 4000 chars, cap 200 000)
- Checked twice: in the Go bridge (before the executor is bothered) and again in
  Python, because scripts and relay watchers are separate entry points
- Denied in every shipped example policy and *not* enabled by `default_allow`;
  the `shell` scope must be turned on by name

### Built for small models

- `desktop_guide {topic}` (always registered, `all|desktop|games|shell|safety`)
  returns the instruction sheet: order of operations, parameter names, examples
- A compact version is pushed as silent context after every (re)connect, so a weak
  model does not have to remember the surface from the conversation
- Action descriptions name the parameter and give an example; the script language
  is summarised in the same context push
- Refusals are written to be actionable: which scope is off, where to enable it,
  which program to allowlist, and which peer integration owns the action
- Missing/empty required parameters are answered with the exact expected shape
  (`{key, value} (required: key)`) instead of "invalid parameters"

### Catalog / extensions / vision

- Catalog list/search/get — metadata from local catalog config
- Extensions: install (`metadata_only` or `git_clone`), enable/disable, uninstall,
  persisted in the extension state file, driven from the dashboard Extensions tab
- Desktop context + optional HTTP vision summary (`NEURO_VISION_URL`; `NEURO_VISION_SERVER_URL` is still read as an alias)
- Extensions come from the signed catalog only. Neuro can install one (`install_extension`) when the `extensions` scope is on, and it can ask for that scope with `request_permission`
- MCP servers from the catalog run as child processes, only after Vedal enables them. Their tools appear as actions under the `extensions` scope
- `game_observe` (and the dashboard's “Show me what Neuro sees”) captures a
  screenshot and asks the vision server for a summary. The screenshot travels to the
  server as PNG bytes, not as a path, so the server can run on another PC

### Working alongside other integrations

- The server connects to Neuro Relay as an integration (`NEURO_RELAY_ENABLED`, `NEURO_RELAY_URL`). Its token comes from the relay token file that `setup` writes, which the built-in host reads too. The relay can be the built-in host (`neuro-integration relay`) or the upstream Python relay (its token in `NEURO_RELAY_TOKEN_FILE`).
- `TestRelayRegistrationMatchesIntermediaryProtocol` pins the client's frames to the intermediary's protocol, and `TestRelayClientAgainstTheGoHost` runs the server's own client against the built-in host in CI.
- Not repeated after the shim was removed: a live run against the upstream Python relay with its own integrations. Re-run it by hand before relying on that combination.
- `NEURO_RESERVED_ACTIONS` keeps another integration's action names out of the registry, so the two cannot shadow each other.
- `control.mode: auto` delegates a game to its dedicated integration when that integration is connected, and drives it from Neuro Desktop when it is not.

## Operator / Vedal controls

Working:

- `permissions.json` enforced in the Go bridge before actions run
- Permission requests: when Neuro asks for a scope that is requestable, the request is listed on the Permissions page with the reason and the time asked for. Vedal approves or denies it. Approvals last until the chosen time or until revoked, and they are in memory, so a restart clears them
- The Extensions page shows live runtime state first (bridge, relay, vision server, MCP bridge), and install state second
- Scopes: `input`, `game`, `shell`, `filesystem`, `process`, `network`, `system`, `vision`, `extensions`
- Scope values accept both `true`/`false` and `{"allowed": …, "requestable": …, "limits": {...}}`;
  per-scope `max_actions_per_minute` is enforced (sliding window, denial explains
  how to raise it)
- Safe defaults: system + filesystem restricted; the example policy denies
  `game_launch` and the `shell` scope
- Operator brake: `NEURO_PAUSED`, `POST /api/control/pause|resume`, and a
  kill-switch file (`NEURO_KILL_SWITCH_FILE`) that blocks actions while it exists;
  input release, status and session end always stay allowed
- Hard deny list (`NEURO_DENY_ACTIONS`) that survives dashboard edits, because it
  is merged into the policy at load time
- Audit log (`NEURO_AUDIT_LOG`, JSON lines) plus `GET /api/audit?limit=N` for the
  dashboard tail: every accepted/refused action with its reason
- `shell` and `system` scopes ignore `default_allow` and bare allow-list entries:
  they have to be enabled by name
- Admin HTTP on `:8300` — `GET/PUT /api/permissions`, `/api/permissions/schema`,
  `/api/actions`, `/api/extensions[/{id}/{action}]`, `/api/games`,
  `/api/games/session`, `/api/games/release`, `/api/games/observe`, `/api/relay`,
  `/api/control` (+`/pause`, `/resume`), `/api/audit`, `/api/status`, `/api/config`,
  and the dashboard itself at `/ui/`
- Every dashboard API call needs the dashboard token that `setup` prints. The sign-in
  page asks for it on any address. Nothing is injected into the page, and the token is
  never read from a URL.
- Live sync: `Save` in the Permissions tab applies the policy to the running bridge

Not done:

- Rich path/process/host allowlists for file and network actions (the shell has a
  real allowlist/denylist; file/network scopes are still whole-capability toggles)
- Permission policies are local files. Catalog items are signed; see the catalog section

## Local testing without live Neuro

`desktop/tools/ollama-neuro`:

- Mimics Neuro API on `ws://127.0.0.1:8000`
- Modes: `manual` (Randy-like), `random`, `ollama` (RWKV7)
- Control UI on `:1337`
- Tuned for slow CPUs (long timeouts, `--warm`, `keep_alive`)

## Explicitly not ready

- A public plugin marketplace. The catalog has one item, the `memory` MCP server, and one publisher key that the maintainer must replace before relying on the signatures.
- A native tray. This was decided against, not postponed: the dashboard is served by the server and works headless, and a tray could not be built or verified in this environment. See `docs/PRODUCTION_TODO.md`.
- A complete Go client. The shipped client is still the Python agent, packaged with PyInstaller. The Go client (`desktop/apps/neuro-client-go`) is slice 1 only: the executor link, status, shell and lifecycle commands. Input, screen, windows and `run_script` still run only in the Python client. See `docs/PRODUCTION_TODO.md`.
- A client build verified on Windows or macOS. The Linux client build is in CI; the Windows and macOS builds have not been run.
- Process supervision by the server. The server does not start other programs (no relay process, no agent). Operators start each process.
- An encrypted executor link. The executor token and the commands travel as plain TCP. Tunnel the port over SSH on an untrusted network. Pinned TLS is open in `docs/PRODUCTION_TODO.md`.
- Reverse connections (the server dialing the agent). They are allowed in the design, and they must be authenticated, but they are not built.

## Quick verify

```bash
cd desktop

# Fix leftover root-owned build dirs if a past sudo broke things:
sudo chown -R "$USER:$USER" frontend/dist dist backend/python/.venv

# Bundle as your user (never sudo):
./scripts/bundle/dev.sh

# Or split:
# terminal A — bridge
cd apps/neuro-integration && go run . -ws-url ws://127.0.0.1:8000
# terminal B — agent (graphical session)
python3 -m controller.agent --bridge 127.0.0.1:9876   # from desktop/backend/python
```

Dashboard without a display, a GPU or pyautogui:

```bash
# terminal A — fake executor (reports the active window from NEURO_FAKE_WINDOW)
python3 desktop/tools/fake-executor/fake_executor.py --addr 127.0.0.1:9876

# terminal B — bridge + dashboard (setup once; it prints the token to sign in with)
cd desktop/apps/neuro-integration
go run . setup
NEURO_UI_DIR=../frontend/dist go run .
# then http://127.0.0.1:8300/ui/
```

Headless / CLI-only verification (no display, no GUI libraries):

```bash
# Python controller: imports and runs with the standard library alone
NEURO_HEADLESS=1 NEURO_SHELL_ALLOWLIST=ls PYTHONPATH=desktop/backend/python \
  python3 -m unittest discover desktop/backend/python/tests -t desktop/backend/python -v

# Repository-level checks (JSON, catalog, example policies, docs)
python3 desktop/tools/ci/repo_checks.py

# Relay host and client (the built-in relay, against the bridge's own client)
cd desktop/apps/neuro-integration && go test -run Relay -v ./...

# Relay, live: set up once (the relay token file is shared), start the host, then the bridge
go run . setup
go run . relay --listen 127.0.0.1:8765 --health 127.0.0.1:8766
# in another shell, the bridge as a relay client (no NEURO_RELAY_TOKEN needed: both read ./relay-token):
NEURO_RELAY_ENABLED=true NEURO_RELAY_URL=ws://127.0.0.1:8765 go run .
# and in a third shell:
curl -s -H "X-ND-Token: <dashboard token>" http://127.0.0.1:8300/api/relay     # connected / registered true
```

For a fake Neuro backend: see [desktop/tools/ollama-neuro/README.md](../desktop/tools/ollama-neuro/README.md).

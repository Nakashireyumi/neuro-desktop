# Troubleshooting

This guide starts from what you can see: the dashboard, the reply Neuro got, and the
terminal the server runs in. It replaces the older guide, which described the Rust
executor and the C++ supervisor. Both were removed (see `CHANGELOG.md`).

## Start here

1. Open the dashboard at `http://127.0.0.1:8300/ui/` and read the **Status** tab. The
   Extensions page shows, in order: the bridge, the relay, the vision server, and the
   MCP bridge. Each shows whether it is running or reachable, and that is live state.
2. Or ask the server directly. `curl -s http://127.0.0.1:8300/health` answers without a
   token. Everything else needs the dashboard token:
   `curl -s -H "X-ND-Token: <dashboard token>" http://127.0.0.1:8300/api/runtime`.
3. Read the terminal that runs the server. Every refusal and error is logged there, and
   every refusal that Neuro sees is in the audit log (`NEURO_AUDIT_LOG`).

## Neuro's action was refused

Neuro gets the refusal text itself, and the text says what to do. These are the
common ones.

| Reply starts with | Why | What to do |
| --- | --- | --- |
| `Neuro Desktop is stopped: the kill switch file … exists.` | the kill-switch file is present | delete the file, or ask Vedal to |
| `Neuro Desktop is paused by the operator, so "…" is refused.` | the bridge is paused | Vedal resumes it on the dashboard (Pause / Resume) |
| `Action "…" is denied by the permission policy (explicit deny list).` | the action is on the deny list | Vedal allows it under Permissions → Explicitly Denied Actions |
| `… is denied by the permission policy` with a scope named | the scope is off | Vedal turns the scope on, or, if the scope is requestable, Neuro asks with `request_permission` |
| `Rate limit reached for the … scope … Try again in about N s` | the scope's `max_actions_per_minute` is used up | wait, or raise the limit in the policy if that is intended |
| a parameter message that names the action and gives an example | Neuro sent the wrong parameters | Neuro retries with the example. Nothing to fix here |

Every refusal is audited (`action`, `relay_command`). Two lines in the audit log say
what was refused and why, which is the first thing to check when Vedal asks "why
didn't it do X".

## The action was accepted but nothing happened on the machine

These come from the executor, which is the agent on the desktop.

| Message | Why | What to do |
| --- | --- | --- |
| `no executor client connected` | no client is connected to the hub | start the client: `start.sh` on this PC, or `neuro-client --bridge <server>:9876 --token …` on the PC Neuro controls (`python3 -m controller.agent …` from source) |
| `timeout after … waiting for executor result (the action may still be running)` | the agent is slow or stuck | do not resend the action. Look at the agent's terminal. `reset_controls` clears queued input and releases held keys |
| `executor disconnected while the command was running` | the agent restarted or lost the network | the agent reconnects by itself. Check that it is running again |
| `invalid or missing executor token` (in the server log) | the agent's token does not match | set the same `NEURO_EXECUTOR_TOKEN` on both sides |
| `timeout waiting for executor response (no TCP client; file IPC timed out)` | the agent is not connected and file IPC has no reader | start the agent on this machine, or connect it over TCP |
| `failed to write IPC command file` | the file IPC directory is not writable | fix `NEURO_IPC_FILE`'s directory permissions |
| `file IPC client does not support execution_session/request_id` | the file client predates request correlation | update `neuro-client` and the server from the same release; TCP is the preferred transport |
| an occupied IPC path stops startup or dispatch | a pending file exists, possibly from another running bridge | stop both programs and inspect the file before removing abandoned work, or select a separate `NEURO_IPC_FILE` path; see the [protocol guide](EXECUTOR_PROTOCOL.md#file-ipc) |

If an action keeps failing, call `desktop_guide` first. It explains what to try, and
it is always allowed.

## Headless and no display

- A machine with no graphical session needs `NEURO_HEADLESS=1` on the agent. Without it
  the GUI libraries can fail while importing (on Linux, `pyautogui` raises when
  `DISPLAY` is unset).
- Screenshots report `this machine has no display session`. That is expected. Use
  `shell_command` for work on a headless machine.
- Set `NEURO_HEADLESS=1` in CI too. The CI jobs do.

## Setup, tokens, and sign-in

Run `neuro-integration setup` once on the server. It creates the relay token that the
server and the relay host share, and it prints the dashboard token once. `setup --check`
reports every mismatch and changes nothing.

| Message | Why | What to do |
| --- | --- | --- |
| `the dashboard has no token yet` (sign-in page, or `503` from the API) | setup has not run | run `neuro-integration setup`, then sign in with the token it prints |
| `NEURO_ADMIN_TOKEN does not match the dashboard token in …` (`503`, and the server log) | the environment variable disagrees with the stored hash | unset `NEURO_ADMIN_TOKEN`, or set it to the printed token |
| `invalid or missing dashboard token` (`401`) | the token typed is not the stored one | type the token that setup printed. If it is lost, run `setup --rotate-dashboard` and restart the server |
| `the dashboard token must be at least 16 characters` | `NEURO_ADMIN_TOKEN` is too short | use the token that setup printed |
| `… does not hold a dashboard token hash` | the hash file is damaged | run `setup --rotate-dashboard`. The old token stops working |
| `no relay token: run neuro-integration setup` (relay link disabled) | no relay token file | run `neuro-integration setup` |
| `NEURO_RELAY_TOKEN differs from the relay token file …` (relay link disabled) | the server's two relay sources disagree | unset `NEURO_RELAY_TOKEN`, or copy the file's value into it |
| `… the relay token must be at least 16 characters` | the relay token file holds a weak value | delete the file, and `setup` makes a new one. Then update the other side |

Sign-in is per browser session. The browser keeps the token in `sessionStorage`, so a
new tab or a new browser session asks again. The token is read from the sign-in form and
the `X-ND-Token` header only. A `?token=` in the URL is ignored.

## The dashboard

- **It does not open.** The server is not running, or it listens somewhere else.
  The address is `NEURO_ADMIN_LISTEN` (default `127.0.0.1:8300`). Check it with
  `curl -s http://127.0.0.1:8300/health`.
- **The sign-in page says the server has no token, or the API answers `503`.** Run
  `neuro-integration setup` on the server (see the table above).
- **The API answers `401`.** The token is wrong or missing. Use the token that setup
  printed. Do not put the token in a URL you share.
- **The page loads but has no assets.** Open the dashboard at `/ui/`, not at the root.
  The bundle's asset links are relative, and CI checks that.
- **The dashboard program answers `502`.** Its JSON reply says `the Neuro Desktop server
  at … did not answer`. The program is running, but the server it forwards to (`--server`,
  default `http://127.0.0.1:8300`) is not. Start the server, or fix the address. Over SSH,
  check that the tunnel is still open.
- **The dashboard program exits with code 2.** It was started with a bad flag or address.
  `--server` must be an `http://` or `https://` URL. The variables are
  `NEURO_DASHBOARD_LISTEN`, `NEURO_DASHBOARD_SERVER`, and `NEURO_UI_DIR`.
- **The dashboard program says `no dashboard found`.** It looks for a folder with an
  `index.html` next to the program, in `frontend/` and `frontend/dist`. Point it at the
  built folder with `--ui-dir`. `--ui-dir … has no index.html` means the folder is not the
  built one: run `npm run build` in `desktop/frontend`.

## The relay

| Message | Why | What to do |
| --- | --- | --- |
| `relay rejected the registration` (status) | the tokens do not match | the server reads the relay token file that `setup` writes. The relay host reads the same file, or `NEURO_RELAY_AUTH_TOKEN` if you set it. For the upstream relay, the token is its `intermediary.auth_token`, which goes in the file named by `NEURO_RELAY_TOKEN_FILE` |
| `invalid auth token` (from the relay) | the same as above, seen on the relay's side | the same |
| `unreachable` (status) | nothing is listening at `NEURO_RELAY_URL` | start `neuro-integration relay`, or fix the URL |
| `refusing the upstream sample token` | the relay was started with `super-secret-token` | choose your own token of at least 16 characters |
| `the relay token must be at least 16 characters` | the token is too short | use a longer random value |
| `too many messages; slow down` | a client sends more than 50 frames a second | fix the client's loop |
| `replaced by a newer connection` | two integrations use the same name | give one of them another `NEURO_RELAY_NAME` |
| `neuro backend not available` | `direct_to_neuro` needs `NEURO_RELAY_NEURO_URL` on the relay host | set it, or stop sending `direct_to_neuro` |

`curl -s http://127.0.0.1:8766/health` on the relay host shows who is connected.

## Extensions and the catalog

- **`Refusing to install …: its catalog signature is invalid`.** The catalog entry
  changed after it was signed, or it is signed by a publisher the server does not trust.
  Check `desktop/catalog/publishers.json` and run `neuro-integration catalog verify`.
  Do not work around it by setting `NEURO_EXTENSIONS_ALLOW_UNSIGNED`. That variable
  exists for a developer who knows what it bypasses. It is honoured only in a development
  build (`-tags neurodev`). A release build ignores it, and logs once that it is ignored.
- **Install is refused because the scope is off.** Neuro needs the `extensions` scope
  on. Vedal turns it on, or Neuro asks for it with `request_permission`.
- **An MCP server shows as not running.** Its runtime state is on the Extensions page.
  The common causes are that `npx` (Node) is not on the PATH, or that
  the server needs an environment variable it does not have. The bridge starts an MCP
  server only after Vedal enables it.

## Vision

- **The vision server shows unreachable.** Check that it is running, and that
  `NEURO_VISION_URL` points at it. `NEURO_VISION_SERVER_URL` is still read as an alias.
- **401 from the vision server.** `NEURO_VISION_TOKEN` differs between the server and the
  vision server. Set the same value on both.
- **422 or 413 from the vision server.** The image is not valid, or it is larger than
  8 MiB. The observation still arrives, with `- Vision summary: unavailable` in it.

## Install and build

- **`go build` fails.** Use Go 1.22 or newer (`go.mod` says 1.22). Build from
  `headless/apps/neuro-integration`.
- **`pip` fails for the agent.** Use Python 3.12, which is what CI runs. The agent's
  stdlib-only parts run without any packages. Install `requirements.txt` only for the GUI
  parts, and skip them on a headless machine.
- **The dashboard does not build.** Use Node 22 (what CI runs), and run `npm ci` before
  `npm run build` in `desktop/frontend`.
- **The bundle is missing a file.** Rebuild it with `scripts/bundle/prod.sh` (or `.ps1`).
  The bundle is written to `desktop/dist/neuro-desktop/`.
- **`neuro-client` was not built.** The build prints `!` and the bundle still works: the
  launcher uses `agent/`. On Linux, the usual cause is missing Python headers, so the
  `evdev` package cannot build: install `python3-dev`. A Python linked without a shared
  `libpython` fails in PyInstaller with `Python shared library … was not found`; use a
  Python that ships one. CI builds the Linux client on Ubuntu with `setup-python`, which
  provides both. On Windows, run the build from a Windows shell with Python 3.11 or 3.12
  on the PATH. The Windows build is not run in CI yet.
- **`neuro-client` says `giving up` and exits 1 with `--once`.** It could not reach the
  server's hub. Check the address and the port, and that `NEURO_EXECUTOR_LISTEN` is not
  loopback-only on the server. Without `--once` it keeps retrying.
- **`neuro-client` connects, then is refused.** The executor token differs. Use the same
  value on both sides (`NEURO_EXECUTOR_TOKEN` on the server, `--token` on the client).

## Platform notes

- **Windows.** An agent that is not elevated cannot send input to a window that runs as
  administrator. Windows blocks that on purpose. Do not run the agent as administrator
  to work around it. Run the target program without elevation, or accept that the
  agent cannot control it.
- **macOS.** Grant the terminal (or the app that starts the agent) Accessibility
  permission for input, and Screen Recording for screenshots. Restart the terminal after
  you grant them.
- **Linux.** Run the agent in your logged-in X11 or Wayland session. Wayland compositors
  restrict synthetic input, so a Wayland session may refuse some actions. Never run the
  agent with `sudo`: it breaks the session's authorisation and makes the files it writes
  root-owned.

## Logs and reporting a problem

- The server logs to standard error, which is the terminal it runs in.
- The audit log (`NEURO_AUDIT_LOG`, JSON lines) records refusals, accepted actions, and
  relay commands. It never records tokens. It does not record a failed dashboard sign-in
  yet; the server counts those in memory only (see `docs/SAFETY.md`, section 8).
- The relay host logs to its terminal, and `/health` shows its live state.

When you report a problem, include the bridge version from the first line of the server
log, the reply Neuro got, the matching audit lines, the agent's terminal output, and the
policy file with secrets removed. Do not include tokens.

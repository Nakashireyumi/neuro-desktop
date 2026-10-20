# Safety

This is the order of defences, where each one lives, and what it does when it refuses.
Read it before you give Neuro a new capability, and before you open a port.

## 1. Where the safety code lives

| Layer | Where | What it does |
|-------|-------|--------------|
| Operator brake | `headless/apps/neuro-integration/safety.go` | pause, and the kill-switch file; a small set of actions still runs while stopped |
| Policy | `permissions.go`, `permission_requests.go`, `admin_permissions.go` | scopes (on, requestable), allow and deny lists, approvals, and the requests Neuro files |
| Rate limits | `ratelimit.go` | a sliding window per scope, shared by Neuro and by watchers |
| Shell firewall | `shellfirewall.go` (server) and `controller/shell.py` (agent) | allowlist, dangerous patterns, timeouts |
| Executor hub | `executor_hub.go` | a token for every agent, and no listening beyond loopback without one. Frame size limits (section 6). The link is plain TCP for now (section 6) |
| Screenshots | `vision.go` (`screenshotPNG`), `controller/desktop.py` | the screenshot arrives as PNG bytes in the reply. The server checks the signature, the base64 and the size, and never opens a path that the client names |
| Dashboard program | `headless/apps/neuro-dashboard` | forwards only `/api/` and `/health`, adds no token, holds no secret. Its tests are in `dashboard_test.go` |
| Dashboard | `admin_server.go`, `dashboard_token.go`, `setup.go` | every `/api` route needs the dashboard token that `setup` makes, with no exception for loopback |
| Setup | `setup.go`, `dashboard_token.go`, `relay_host.go` | one relay token file for both sides; the dashboard token kept only as a SHA-256 hash, mode 0600 |
| Relay | `relay_host.go` (host), `relay.go` (client, and the gates on watcher commands) | a token, browsers refused, frame limits |
| Extensions and catalog | `catalog_trust.go`, `extensions.go`, `mcp.go` | signatures, publishers, install mode, a clean environment for MCP servers |
| Vision | `vision.go`, `headless/apps/nd-vision-server` | a token off loopback, reads only inside one directory |
| Audit | `NEURO_AUDIT_LOG` (JSON lines) | every refusal and every accepted action |

## 2. Layers, in the order an action passes through them

1. **Connection.** Neuro's API is outbound. The executor hub checks its token, over a
   link that is not encrypted yet. Every dashboard API call needs the dashboard token,
   on any address. The relay needs its token, and refuses browsers.
2. **Operator brake.** A paused bridge, or a present kill-switch file, refuses every
   action except the stop-safe set (section 3).
3. **Hard deny.** Actions in `NEURO_DENY_ACTIONS` are refused whatever the policy says.
   The dashboard cannot remove them.
4. **Policy.** The action's scope must be on, and the action must not be on the deny
   list. A scope that is off but requestable is refused with a way to ask (section 4).
5. **Rate limit.** Each scope has an optional `max_actions_per_minute`. Over the limit,
   the refusal says when to try again.
6. **Validation.** Parameters are checked before anything runs. A refusal names the
   action and the parameter, and gives a valid example, so a small model can correct
   itself on its next turn.
7. **Executor.** The agent applies its own checks. The shell firewall is one of them.
8. **Audit.** Each refusal and each accepted action is written to the audit log.

A watcher command that comes through the relay passes through the same gates, in
the same order (`denyRelayCommand`). It is counted against the same rate limit.

## 3. The operator brake

- `NEURO_PAUSED=1` starts the bridge paused. The dashboard's Pause and Resume buttons
  call `POST /api/control/pause` and `POST /api/control/resume`.
- The kill switch is a file. `NEURO_KILL_SWITCH_FILE` names it, and while it exists every
  action is refused. Deleting the file resumes the bridge.
- While the bridge is stopped, these still run: `key_release_all`, `get_status`,
  `send_desktop_context`, `game_release_all`, `game_status`, `game_end_session`,
  `desktop_guide`, and `reset_controls`. They release held input, report status, and
  tell Neuro how to recover. `request_permission` does not run while stopped.

## 4. Permissions

The policy is a JSON file (`NEURO_PERMISSIONS_FILE`). `default_allow` decides what happens to an
action that no scope or list covers, and it is `false` by default. Without a file the server uses a
built-in default, and an action is denied unless its scope is on.

| Scope | Default | Notes |
|-------|---------|-------|
| `input` | on | mouse and keyboard |
| `process`, `network`, `vision`, `game` | on | |
| `filesystem`, `system`, `shell`, `extensions` | **off** | `shell` and `system` need explicit consent |

Each scope has `allowed` and `requestable`:

- **Requestable** means Neuro may call `request_permission` with a reason and a time
  (5 minutes, 15, 1 hour, 4 hours, or until revoked). The request shows on the Permissions
  page. Vedal approves it or denies it. Approvals and requests are in memory, so a restart
  clears them.
- **Always allowed**, under any policy: `desktop_guide`, `reset_controls`, and
  `request_permission`. A deny-all policy cannot lock Neuro out of the way to recover or
  to ask. A test pins this.

The dashboard saves policy changes to `NEURO_PERMISSIONS_FILE` (`PUT /api/permissions`)
and applies them at once. Keep a copy of the file before you edit it on the dashboard.

Scripts are validated in full before execution. Commands inside a script need the
same permissions as their individual actions: `SHELL` needs shell approval and the
firewall, `LAUNCH` needs system approval, and desktop shortcuts retain their own
scopes and action denials. Quoting or escaping a command name does not change its
permissions. Retained queues are checked again before a later action drains them.
Scripts and pending queues are limited to 64 KiB per script, 512 instructions, and
20 seconds of estimated execution time. The client also checks a deadline between
instructions and limits a script's shell command to the remaining time. These
checks do not interrupt an individual operating-system call already in progress.

Desktop context and game observations check live vision permission before
collection, after waiting for the executor, before uploading an image, and before
returning or sending the result. A changed approval discards an in-flight result.
The periodic context loop uses the same checks. Explicitly requesting `vision:false`
does not capture or upload an image; ordinary metadata refreshes do not capture.
Analysis responses are limited to 256 KiB.

## 5. Shell firewall

`shell_command` runs only when all of these hold:

1. the `shell` scope is enabled **and** the action is not denied;
2. the program name is in `NEURO_SHELL_ALLOWLIST` (empty allow-list = nothing
   runs);
3. the command line matches none of the built-in dangerous patterns (recursive
   force-delete of `/`, `dd` onto a block device, fork bombs, piping a download
   straight into a shell, `chmod 777 /`, ...);
4. `NEURO_SHELL_DENY_PATTERNS` (extra regexes) do not match;
5. the command finishes inside `NEURO_SHELL_TIMEOUT` seconds (default 20, max
   120). Output is clipped so it cannot swamp the context window.

The same rules are implemented twice, on purpose: in the bridge (so a bad
command never leaves the machine that owns the policy) and in the Python
controller (so the executor is not defenceless if it is driven directly).

## 6. Network exposure

- **Dashboard.** It binds to `127.0.0.1:8300` by default. Every `/api` route except
  `/health` needs the dashboard token, in the `X-ND-Token` header or as a Bearer token.
  A query parameter is never read, and there is no exception for loopback, so a script,
  a browser, or a proxy on this machine needs the token too. A server with no token
  answers `503` and says to run `setup`. A wrong token gets `401`. The shell page is
  served without a token and with `Cache-Control: no-store`, because it holds no secret.
- **Dashboard program.** `neuro-dashboard` listens on `127.0.0.1:8310` by default and
  forwards only `/api/` and `/health` to the server. It adds no token and keeps the
  caller's `X-ND-Token` or Bearer header, so the server makes the decision: a wrong or
  missing token gets the server's `401`, and a server that is down gets a `502` with a
  JSON reason. The shell is served with `Cache-Control: no-store`. A non-loopback listen
  address or a plain-HTTP server on another machine prints a warning at start-up.
- **Executor hub.** With `NEURO_EXECUTOR_TOKEN` set, every agent must present it. Without
  one, the hub listens on loopback only. It refuses to start on any other address and
  says why. Before this rule, a hub with no token accepted any client as the executor,
  and that client received Neuro's commands. The link is plain TCP, so the token and the
  commands can be read on the wire. On an untrusted network, tunnel the port over SSH.
  TLS with a pinned certificate is an open item in `docs/PRODUCTION_TODO.md`.
  Frames are size-limited: the first frame (the hello, read before the token is
  checked) at 64 KiB, and every later frame at 8 MiB. A frame over its limit closes the
  connection, so a peer that never sends a newline cannot make the server buffer
  without bound.
- **Relay host.** Loopback by default. It refuses any request with an `Origin` header
  (so a web page cannot connect), refuses binary frames, refuses the upstream sample token
  and any token under 16 characters, and limits each connection's frame rate. Anything
  that holds the token can register, so keep the token secret.
- **Vision server.** Loopback by default. Binding elsewhere needs `NEURO_VISION_TOKEN`. It
  reads image files only inside `NEURO_VISION_ROOT`, and never opens a path without one.
  The Neuro server sends it image bytes only (`image_base64`), never a path.
- **MCP servers.** Child processes that the bridge starts only after Vedal enables them,
  and only from the signed catalog. A child gets a small base environment and only the
  variables its catalog entry lists.
- No component opens an inbound port to the internet by itself.

## 7. Extensions and the catalog

- Catalog items are signed. An item whose signature does not match is refused in every
  mode. An item signed by a publisher the server does not list is refused too.
- Installing never takes a URL from Neuro or from the dashboard. The id is looked up in the
  signed index.
- `NEURO_EXTENSION_INSTALL_MODE=metadata_only` (the default) records the install and
  fetches nothing. `git_clone` fetches the item's pinned commit from its `https`
  repository, and only for a verified item.
- `NEURO_EXTENSIONS_ALLOW_UNSIGNED` lets unsigned items be fetched, and only in a
  development build (`go build -tags neurodev`, which `dev.sh` and `dev.ps1` use). A
  release build ignores it, and logs once that it is ignored. It exists for a lab
  machine. Do not set it on a machine Neuro controls.
- The shipped signature uses a key that the maintainer generated for this repository.
  Rotate to your own publisher key before you rely on the signatures. See
  `docs/PRODUCTION_TODO.md`.

## 8. What is audited

`NEURO_AUDIT_LOG` writes JSON lines. Each line has the action name, the decision
(`accepted`, `refused`, or `error`), a reason for a refusal, and the command for a shell
action. Relay commands are recorded as `relay_command`. The dashboard's `/api/audit`
endpoint reads it. Every refusal path records an entry, which is what makes a stuck model
debuggable after the fact. Tokens are never written.

One gap: a request that fails the dashboard token check is answered `401` but is not
written to the audit log. The server counts it in memory only. This is an open item in
`docs/PRODUCTION_TODO.md`.

## 9. Verify it yourself

```bash
cd headless/apps/neuro-integration
go test -race -count=1 ./...                                  # everything
go test -count=1 -run 'Relay|Watcher' -v ./                   # the relay and watcher gates
go test -count=1 -run 'ExecutorHub' -v ./                     # the hub's token and loopback rules
go test -count=1 -run 'WeakModel|EscapeHatch|ExamplePolicy' -v ./   # refusals and the escape hatch
go test -count=1 -run 'Setup|DashboardCredential|RelayClientToken|Admin' -v ./   # setup, the dashboard token, and the API guard
go test -count=1 -tags neurodev -run 'UnsignedEscapeHatch' -v ./     # the dev-only unsigned switch, in a dev build
go test -count=1 -run 'ReadExecutorLine|ExecutorHubDrops|ExecutorHubAccepts|Screenshot' -v ./   # frame limits and screenshot bytes
cd ../neuro-dashboard && go test -count=1 -v ./ && cd ../neuro-integration   # the dashboard program's proxy rules
python3 ../../tools/ci/repo_checks.py                         # the repository checks (run from this folder)
```

The CI workflow runs all of these, and a release is blocked unless every job passes.

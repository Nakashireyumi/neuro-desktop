# Neuro Relay

Neuro Relay lets several integrations share one Neuro connection. Neuro-sama's
game integrations (Minecraft, Balatro, and others) and the desktop integration
can then run side by side, and a watcher (a dashboard, a tool) can see what is
connected.

This repository ships its own relay host, written in Go and built into the
server: `neuro-integration relay`. It implements the intermediary socket that
the upstream project ([Nakashireyumi/neuro-relay](https://github.com/Nakashireyumi/neuro-relay))
speaks, so existing integrations connect to it unchanged. It also refuses the
things the upstream relay gets wrong (see [Differences](#differences-from-the-upstream-relay)).

## Running it

```bash
cd desktop/apps/neuro-integration
go run . setup        # once: writes the relay token file (./relay-token, mode 0600)
go run . relay --listen 127.0.0.1:8765 --health 127.0.0.1:8766
```

| Flag | Env | Default | What it is |
| --- | --- | --- | --- |
| `--listen` | `NEURO_RELAY_LISTEN` | `127.0.0.1:8765` | where integrations and watchers connect (`ws://`) |
| `--health` | `NEURO_RELAY_HEALTH_LISTEN` | `127.0.0.1:8766` | `GET /health`; empty disables it |
| `--token-file` | `NEURO_RELAY_TOKEN_FILE` | `./relay-token` | the token the host and the server share (mode 0600) |
| `--neuro-url` | `NEURO_RELAY_NEURO_URL` | empty | optional Neuro API URL, used only by `direct_to_neuro` |
| | `NEURO_RELAY_AUTH_TOKEN` | the file's token | an override for the host. It must equal the file; `setup --check` reports a difference |
| | `NEURO_RELAY_NEURO_OS_TOKEN` | empty | a second, *enhanced* token for watchers that may send to Neuro |

The host reads the token file. If the file does not exist, the host creates it with
a 48-character hex token (mode 0600). The upstream sample token (`super-secret-token`)
is refused, and so is any token shorter than 16 characters. The host exits with
status 2 if the token it is given is refused.

The host runs in the foreground, and it stops on SIGINT or SIGTERM. Run it as a
service if you want it to start with the machine; the bundle does not install
one for you.

## Connecting the server as an integration

Set these in the server's environment (the bridge is then a relay client):

```bash
NEURO_RELAY_ENABLED=true
NEURO_RELAY_URL=ws://127.0.0.1:8765
NEURO_RELAY_NAME=desktop          # optional; shown to watchers
```

The server takes its token from the relay token file, the file the host reads
(`NEURO_RELAY_TOKEN_FILE`, default `./relay-token`). Nothing is copied by hand.
`NEURO_RELAY_TOKEN` is optional. If you set it, it must equal the file. When it
differs, or when there is no token at all, the link stays off, and the server log and
the dashboard say why. The server never dials with a token that disagrees with the file.

### Using the upstream Python relay

The upstream relay keeps its token in its own YAML (`intermediary.auth_token`), not in
this file. Give the server that value in a file of its own:

```bash
umask 077
printf '%s\n' '<intermediary.auth_token from authentication.yaml>' > "$HOME/upstream-relay-token"
export NEURO_RELAY_ENABLED=true
export NEURO_RELAY_URL=ws://127.0.0.1:8765
export NEURO_RELAY_TOKEN_FILE="$HOME/upstream-relay-token"
```

The value must be at least 16 characters, and it must not be the upstream sample
(`super-secret-token`). You can also run `neuro-integration setup` with
`NEURO_RELAY_TOKEN_FILE` set to a path that does not exist yet. Setup then creates the
file with a new token, and you copy that value into the upstream `intermediary.auth_token`.

The Extensions page shows the relay state: `disabled`, `disconnected`
(configured but not connected yet), `unreachable` (nothing listening at the
URL), `connected` (socket open but not registered yet), `idle` (registered, no
other peers), or `registered` (registered, with other peers).

## The socket protocol

Every connection starts with **one JSON frame** that registers it:

```json
{"type": "integration", "name": "minecraft", "auth_token": "…"}
```

`type` is `integration` (a game or tool that exposes actions) or `neuro-os` (an
enhanced watcher). `name` is 1 to 64 letters, digits, spaces, dots, dashes, or
underscores.

Refusals are a JSON error frame, after which the connection closes:

| Reply | Why |
| --- | --- |
| `{"error": "registration must be JSON"}` | the first frame was not JSON |
| `{"error": "invalid auth token"}` | the token is wrong (or was not provided) |
| `{"error": "unknown registration type"}` | `type` is neither `integration` nor `neuro-os` |
| `{"error": "name must be 1-64 letters, digits, spaces, dots, dashes or underscores"}` | bad name |
| `{"error": "binary frames are not accepted"}` | a binary frame was sent |
| `{"error": "too many messages; slow down"}` | more than 50 frames per second on one connection |

A connection that sends a `Origin` header (any browser) is refused with HTTP 403
before the socket upgrades. A newcomer with the same name replaces the older
connection, and the older one is told `replaced by a newer connection with the same name`.

### Integrations

After registering, an integration:

* announces its actions with `{"event": "register_actions", "actions": {…}}`.
  Watchers are told the names: `{"event": "integration_registered_actions", "from": "minecraft", "actions": ["…"]}`;
* sends any other JSON frame, which watchers receive as
  `{"event": "integration_message", "from": "minecraft", "payload": …}`.

Integration messages go to watchers. The relay does not forward them to Neuro
by itself. An integration that wants Neuro to see something sends it to the
Neuro API through its own connection, as it would without a relay.

### Watchers

A watcher receives events:

* `integration_connected` / `integration_disconnected` with `name`;
* `neuroos_connected` / `neuroos_disconnected` with `name` and `privileges` (`enhanced` or `standard`);
* `integration_message` and `integration_registered_actions`, as above.

A watcher can send two kinds of command:

* `{"target": "minecraft", "cmd": {…}}` delivers the command to that integration
  as `{"from_watcher": "<watcher name>", "cmd": {…}}`. The watcher gets
  `{"status": "sent"}`, or `{"error": "invalid target/cmd"}` if the target is not
  connected or the command is malformed.
* `{"direct_to_neuro": true, "payload": {…}}` forwards `payload` to the Neuro API.
  Only an `enhanced` watcher may send it, and the host must have
  `NEURO_RELAY_NEURO_URL`. The reply is `{"status": "forwarded_to_neuro"}`, or
  `{"error": "neuro backend not available"}`.

A watcher message that is not JSON gets `{"error": "watcher messages must be JSON"}`.

## Limits

| Limit | Value |
| --- | --- |
| frame size | 256 KiB |
| frames per second, per connection | 50 |
| integrations / watchers at once | 64 / 32 |
| registration must arrive within | 10 s |
| idle timeout (pings every 30 s) | 90 s |

## `/health`

```bash
curl -s http://127.0.0.1:8766/health
```

```json
{"ok": true, "uptime_seconds": 412, "integrations": ["minecraft (4 actions)"], "watchers": ["dashboard"], "neuro_link": "not_configured"}
```

`neuro_link` is `not_configured` (no `NEURO_RELAY_NEURO_URL`), `disconnected`, or `connected`.
The response has names only, never tokens or payloads, so it is safe to show on the
dashboard.

## Differences from the upstream relay

| | Upstream (Python) | This host (Go) |
| --- | --- | --- |
| default token | a sample token (`super-secret-token`) in its YAML | none: refuses the sample, generates a 48-hex token into a 0600 file |
| configuration | YAML, `src/resources/authentication.yaml` | flags and environment variables |
| browsers | not refused | refused (any `Origin` header) |
| binary frames | written to disk as `upload_<name>.bin` | refused (writing them to disk was a path-traversal risk) |
| integration messages | forwarded toward Neuro | sent to watchers only |
| `direct_to_neuro` | enhanced watchers | enhanced watchers, with `NEURO_RELAY_NEURO_URL` |
| process management | none | none: the server does not start a relay process |

The Go host and the upstream relay agree on the registration frame, the event
names, and the watcher commands above, and `relay_host_e2e_test.go` runs the
server's own relay client against the host to keep it that way.

## Testing

```bash
cd desktop/apps/neuro-integration
go test -race -count=1 -run Relay -v ./
```

CI runs the same tests, plus a live smoke test of the built binary: the sample
token is refused, a generated token is written with mode 600, and `/health`
answers.

## Security notes

* Put the relay on loopback, or behind a firewall you control. Anything that can
  reach the socket and knows the token can register as an integration.
* The token file is a secret. It is written with mode 0600, and `.gitignore` keeps
  `relay-token` and `dashboard-token` out of Git. Do not copy it into a shared folder.
* Use a random token of at least 16 characters, and do not reuse the enhanced
  token for integrations.
* A watcher command that reaches the bridge through the relay is treated like
  one of Neuro's actions. It runs only if the stop switch, the permission policy,
  and the per-scope rate limit all allow it. Each refusal is audited as
  `relay_command`. See [SAFETY.md](SAFETY.md).

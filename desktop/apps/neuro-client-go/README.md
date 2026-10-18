# neuro-client-go (slice 1 of the Go client)

The Go port of the Python client (`desktop/backend/python/controller`). It is **not
shipped yet**: the bundle still packages the Python client as `neuro-client`. Read
`docs/PRODUCTION_TODO.md` ("Client rewrite in Go") for what is left.

## What this slice does

- Connects out to the server's executor hub with `--bridge` and `--token`, speaks
  protocol version 2 (`docs/EXECUTOR_PROTOCOL.md`), answers pings, and runs commands.
- Reconnects with the same backoff as the Python client. `--once` exits after the
  first session, or gives up when the hub is not reachable.
- Headless rules as in `gui_stub.py`: `NEURO_HEADLESS`, and on Linux `DISPLAY` or
  `WAYLAND_DISPLAY`.
- `shell_command`: the same firewall as `shell.py` (chaining operators, allowlist from
  the server and this machine, deny patterns, timeout, output limit). The timeout is
  capped at 120 s even when the server asks for more.
- `get_status` (the keys the server reads), `heartbeat`, `shutdown_*`,
  `execute_queue`, `clear_action_queue`.

## What it refuses

Mouse, keyboard, screen, window and `run_script` commands. On a headless machine the
refusal is the same text as the Python client's. Otherwise it says the command still
runs in the Python client (`neuro-client`).

## Not in this slice

File IPC (`--ipc-file`), input, screen capture, window and process telemetry, the
action-script runner, and the input queue.

## Build and test

```bash
cd desktop/apps/neuro-client-go
go vet ./... && go test -count=1 ./...
go build -o neuro-client-go .          # Linux, macOS, Windows (CGO not needed)
./neuro-client-go --bridge 127.0.0.1:9876 --token "$NEURO_EXECUTOR_TOKEN"
```

`agent_test.go` fails if the Python agent runs a command that this client neither
handles nor refuses. `shell_test.go` and `protocol_test.go` run a real shell (Unix only;
the Windows shell path is not verified) and a fake hub.

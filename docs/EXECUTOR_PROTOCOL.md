# Executor protocol (server ↔ agent)

The server decides *what* to do; the agent is the only process that touches a
machine. They use two transports with the same command and result objects.
TCP frames are newline-delimited JSON (one object per line, UTF-8, no length
prefix); file IPC uses one JSON object per file.

* **agent → server** (`neuro-client --bridge host:9876` on the PC Neuro controls, or
  `python3 -m controller.agent --bridge host:9876` from source; both run the same agent).
  The agent always dials out to the hub, so it needs no inbound port. The server's side is
  `NEURO_EXECUTOR_LISTEN` (`-executor-listen`), which defaults to `127.0.0.1:9876`.
* **file IPC** (co-located only): the server writes the command to
  `NEURO_IPC_FILE` and polls `NEURO_IPC_FILE.response`.

`PROTOCOL_VERSION` is `2`; the server sends its version in `hello_ack` for
diagnostics. This version is not capability negotiation: use a client built from
the same release as the server. Current file IPC additionally requires the
session and request echoes described below; older file clients fail with an
explicit update message.

## Frames

```jsonc
// agent -> server, first frame
{"type": "hello", "role": "executor", "version": "2", "token": "<optional>"}

// server -> agent
{"type": "hello_ack", "role": "bridge", "version": "2"}
{"type": "hello_nack", "error": "invalid or missing executor token"}

// server -> agent
{"type": "command", "id": "1730000000000000000",
 "command": {"type": "shell_command", "params": {"command": "ls -la"},
             "execution_session": "<bridge-instance nonce>"}}

// agent -> server
{"type": "result", "id": "1730000000000000000", "success": true,
 "data": {"output": "exit code: 0\nstdout:\n…"}, "error": ""}

// liveness, both directions; the server sends one every 30s and drops an agent
// that has not answered for 90s
{"type": "ping", "id": "…"} / {"type": "pong", "id": "…"}
```

The handshake is bounded (10 s): a client that connects and never says hello is
closed, so a port probe cannot hold a slot. The token is compared in constant
time and is required whenever the hub is not bound to loopback.

**Frame size.** A frame is one line, and the hub reads it with a limit:

* the hello is capped at **64 KiB**, because it is read before the token is checked;
* every later frame is capped at **8 MiB**;
* a frame over its limit closes the connection. The hub never buffers past the limit,
  even when no newline ever arrives.

The largest normal frame is a status reply with a screenshot: the agent caps the PNG
at 3 MiB, which is about 4 MiB as base64.

## Commands

The authoritative list is `executorCommands` in
`desktop/apps/neuro-integration/executor_commands.go`, mirrored by
`EXECUTOR_COMMANDS` in `desktop/backend/python/controller/agent.py` and pinned by
a test on both sides.

| Command | Params | Notes |
| --- | --- | --- |
| `move_mouse_to` | `x`, `y`, `duration?` | clamped to the screen by the agent |
| `move_mouse_relative` | `dx`, `dy`, `duration?` | mouse-look (no absolute cursor) |
| `mouse_click` | `button?` | `left`, `right`, `middle` |
| `mouse_hold_for` | `button?`, `seconds?` | hold a button down, then release |
| `key_press` | `key` | one key or a shortcut string |
| `key_combo` | `keys[]` | pressed together |
| `key_hold_for` | `key`, `seconds` | for movement keys |
| `key_release_all` | — | safety release |
| `type_text` | `text` | types into the focused window |
| `run_script` | `script` | the ACTION script language (`TYPE`, `MOVE`, `SHELL`, …) |
| `shell_command` | `command`, `cwd?`, `timeout?` | shell firewall + allowlist; output is returned in `data.output` |
| `get_status` | `max_open_windows?`, `max_processes?`, `max_actions?`, `capture_screenshot?` | telemetry; `headless` tells the truth about the session; the screenshot comes back as bytes (below) |
| `execute_queue` / `clear_action_queue` | — | queue control (below) |
| `shutdown_gracefully` / `shutdown_immediately` | — | releases input, then exits |

Anything else in the action registry (desktop shell intents, catalog, extension
commands, `game_*`) is handled **inside the server** and never forwarded;
`sendToExecutor` rejects them with an explicit error rather than sending them to
an agent that could not run them.

## Screenshots travel as bytes

`get_status` with `capture_screenshot: true` returns the screen in the reply:

```jsonc
{"status": "running", "headless": false, ..., "screenshot_png_b64": "iVBORw0KGgo…"}
```

* `screenshot_png_b64` is a base64 PNG, scaled so its long side is at most 1 600 px and
  its size is at most 3 MiB. It is `null` when no screenshot was asked for, when the
  session is headless, or when capture failed.
* The server never opens a path that the agent names. An older `screenshot_path` field
  is ignored, and the server does not read files on the agent's behalf. The reason is
  that the server may run on another PC, and a path there would be an arbitrary file
  read. The server checks the bytes (PNG signature, base64, size limit of 8 MiB) before
  it uses them.
* The vision service receives the same bytes as `image_base64`. It also accepts
  `image_path`, but only inside its own `NEURO_VISION_ROOT`, and the server does not send it.

## Queueing

Each command may carry `execute_now` and `clear_after` (both default `true`), the
same contract the Rust executor had:

* input commands are queued; `execute_now` runs the queue, `clear_after` empties
  it afterwards. `{"execute_now": false}` therefore batches several primitives
  into one motion (used by the game layer to press a key *while* looking around).
* `execute_queue` and `clear_action_queue` do those two steps explicitly.

The agent preserves source order across keyboard, mouse, wait, shell, launch,
and release operations. Scripts are preflighted completely before any side
effect. Nested operations keep their individual permission checks, and the
server rechecks the capabilities of retained work before draining it. Limits
are 64 KiB per script, 512 pending operations, and 20 seconds of estimated delay.
Execution also checks a cooperative 20-second deadline between operations;
an individual OS input call cannot be interrupted by that check. Shell calls
receive the remaining timeout.

An execution failure clears pending work and releases held input, so retrying
does not replay an earlier operation. A preflight rejection leaves previously
accepted deferred work intact. Disconnect, authentication failure, and shutdown
end the TCP agent session, clear its queues, release input, and stop its input
monitor. Reconnecting creates a fresh agent session.

Failures are values, not exceptions: `success: false` with a message written for
a reader that has to decide what to do next ("This machine has no display
session… use the `shell_command` action").

## File IPC

Used when the server has no TCP client (the classic one-PC case). The agent polls
`NEURO_IPC_FILE`, **atomically claims it before reading or executing**, and writes the response to
`NEURO_IPC_FILE.response` through a temporary file + rename, because the server
polls that path and must never read a half-written object. If the agent cannot
consume the command file, it refuses execution. A server timeout removes its
own unconsumed command; it cannot cancel an operation the agent already started.

Each command includes an `execution_session` nonce for the running bridge and
a `request_id` unique within that session. The agent echoes both in its result.
The server accepts only an exact pair, ignores late results from other requests,
and refuses a response with missing echoes. A changed session clears deferred
work and releases held input before the next command. These identifiers prevent
accidental queue reuse and result confusion; they are not credentials.
Missing echoes are detected when the result arrives, after an older client may
already have executed the command. They are not a pre-execution compatibility
handshake, so keep server and client releases matched.

Use a separate IPC path for every running bridge in an operator-owned directory.
At startup, the server refuses either occupied path and preserves its contents.
Protocol-shaped JSON alone cannot establish that another bridge has abandoned
the file. Stop both programs and inspect pending files before removing abandoned
ones, or choose a separate path. Dispatch also refuses an occupied command path;
request cleanup removes only its own correlated command or response. An older server
can use an updated agent for immediate, self-clearing commands, but retained
file queues require a session identifier. Update both programs together.

## Safety on the agent side

The agent trusts the server for intent and permission decisions. Local bounds
include the shell firewall, headless refusal, key validation, frame/queue limits,
and the 4 000-character shell output cap in `desktop/backend/python/controller/`.
These checks do not provide an OS sandbox. Protect the bridge, executor token,
IPC directory, and operator account; run the client with the privileges needed
for the intended applications.

## Testing

* Server: `desktop/apps/neuro-integration/executor_hub_test.go`,
  `executor_commands_test.go`, `executor_liveness_test.go` (round trip, token
  rejection, slow command, timeout, replacement, ping/drop), `executor_frame_test.go`
  (the frame limits, an oversized hello refused before auth, a screenshot-sized reply
  kept whole), and `screenshot_test.go` (the PNG check and the size limit).
  `executor_session_test.go` covers correlated results, occupied-path handling,
  and timeout cleanup; `game_detection_guard_test.go` covers observation cache
  revocation and replacement approvals.
* Agent: `desktop/backend/python/tests/test_agent.py` (handshake, ping, malformed
  frames, rejection, queue semantics, file IPC, and the cross-language command
  parity check), `test_script_execution.py` (ordered, bounded execution and
  session cleanup), and `test_screenshot_bytes.py` (the screenshot is scaled and
  encoded as bytes, and the status reply carries it).
* Simulator: `desktop/tools/fake-executor/fake_executor.py` — no input is ever
  generated, so the whole server can be exercised on a headless machine.

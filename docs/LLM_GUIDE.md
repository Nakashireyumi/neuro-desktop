# Context and actions for Neuro and Evil

Neuro Desktop exposes registered actions, a short connection brief, and help on
demand. This follows the [Neuro SDK best practices](https://github.com/VedalAI/neuro-sdk/blob/main/API/BEST_PRACTICES.md):
use readable, meaningful context, concise action descriptions, and prompt results
after validation. Reconnecting does not erase Neuro's existing context.

## What is sent automatically

After action registration, the bridge sends one brief of approximately 800 UTF-8
bytes. It explains observation, acceptance versus completion, permissions, and
recovery. It does not read a manual from the working directory or send a second
script reference. Re-registering actions, including after a mode change, does not
repeat the brief. `NEURO_PROMPT_ASSIST=false` disables it.

Detailed help uses `desktop_guide {"topic":"script"}`. The default topic is
`overview`; other topics are `desktop`, `script`, `games`, `shell`, `safety`, and
`all`. Every result is limited to 2 KiB of valid UTF-8. `all` summarizes every
topic; request a named topic for detail. Help describes the registered mode: it
does not advertise low-level scripts when only high-level controls are available.

Periodic observation is separately opt-in with `NEURO_CONTEXT_POLL_SECONDS`
(default `0`, disabled). Automatic updates send meaningful changes such as the
active application, available input, or a changed visual description. Pointer
position, action counters, and screenshot byte size do not create updates on their
own. A failed send does not consume the change: the next poll can retry it.
Repeated explicit observations still return a fresh snapshot.

Desktop context is capped at 2 KiB. Titles, process names, error details, and
visual summaries have smaller field budgets; newlines and control characters in
these observed values are flattened. Text from a window remains untrusted data,
even after formatting. The global Vision permission is checked before collection
and again before delivery. Image capture additionally needs the built-in Vision
Service enabled for this connection's configured persona; a changed approval or
service configuration invalidates an observation already in flight.

## A compact operator prompt

Use this only if the backend needs an additional prompt. The bridge already
provides its startup brief and registered action descriptions.

```text
Use only your registered Neuro Desktop actions with their exact parameters.
Request get_desktop_context {} before acting on an unknown desktop; read the
snapshot that follows. Accepted work is queued, not proof that an action finished.
Use short sequences and observe again when the next step depends on screen state.
For syntax help, request desktop_guide with the relevant topic.
Correct a malformed call once. Do not repeat a permission denial or pending
permission request: Vedal decides approvals. If input is stuck, reset_controls {}.
```

## Public action shapes

Neuro sees the currently registered schemas. `/api/actions` exposes the same
surface to the manager. High-level and low-level modes have different controls;
the following table is a reference, not a second action list to inject into every
turn.

| Action | Example | Meaning |
| --- | --- | --- |
| `get_desktop_context` | `{}` | Requests platform, input availability, geometry, and application context in either mode; the snapshot follows as a message. |
| `move_mouse_to` | `{"x":640,"y":360}` | Low-level absolute pointer movement, in screen pixels. |
| `mouse_click` | `{"button":"left"}` | Clicks at the pointer; move to the target first. |
| `type_text` | `{"text":"hello"}` | Types into the focused control. |
| `key_press` | `{"key":"enter"}` | Taps one key. |
| `run_script` | `{"script":"COMBO ctrl c"}` | Low-level ordered script; use `command` on macOS where appropriate. |
| `shell_command` | `{"command":"echo hello"}` | Requires the shell scope and effective program allowlist. Output follows acceptance as a message. |
| `desktop_guide` | `{"topic":"desktop"}` | Bounded help for the current mode; `{}` means overview. |
| `reset_controls` | `{}` | Clears pending input and releases held controls, including while paused; partial failures are reported. |
| `request_permission` | `{"scope":"extensions","reason":"install a memory tool","minutes":15}` | Requests a scope only when you have allowed requests for it; it does not grant access. |
| `game_list_profiles` | `{}` | Lists configured profile IDs and their control maps. |
| `game_start_session` | `{"profile_id":"minecraft"}` | Selects a configured profile. It does not grant permissions or override a dedicated integration's ownership. |
| `game_move` | `{"direction":"forward","seconds":1}` | Uses profile bindings; directions are forward, back, left, right, up, down. |
| `game_look` | `{"dx":120,"dy":0}` | Relative mouse-look offsets. |
| `game_action` | `{"action":"jump"}` | Uses a named action from the selected profile's control map. |
| `game_observe` | `{}` | Requests game/window state, with approved vision when configured. |

`get_status` and `key_combo` are executor protocol commands, not public Neuro
actions. Use `get_desktop_context` for observation and `COMBO` inside a registered
`run_script` for key combinations. There is no `game_list` action.

An executor connection alone cannot prove input works. A snapshot reports a missing
display or unavailable input backend honestly. Use approved command-line work on
a headless machine. Desktop shortcuts also depend on the OS and desktop settings.

## Results and recovery

Validate required arguments and policy before acknowledging a call. Accepted work
runs after acknowledgement; outcome messages report failures and useful output.
Acceptance means queued, so a following decision that depends on the screen needs
a requested observation. A timeout cannot undo input, shell commands, or launches
that already happened. Inspect before retrying an uncertain operation.

Keep scripts short: a script is limited to 64 KiB, 512 pending instructions, and a
20-second estimated delay budget, with a cooperative execution deadline. The
whole script is checked before it is appended to one ordered queue. Parsing never
runs a shell command or launches an application. A runtime failure clears pending
work and releases held input; it cannot roll back completed OS effects.

Incorrect parameter names and types receive corrective refusals. A permission
refusal is different from a malformed call: repeating it does not create approval.
`reset_controls` reports whether queue clearing and release succeeded. If it only
partly succeeds, stop and inspect the affected executor.

## Verification

The Go suite includes a real WebSocket test for registration-before-brief,
startup opt-out, and one-time briefing. It checks help in both control modes,
automatic deduplication, failed-send retries, UTF-8 limits, and observed-field
formatting. `testdata/weak_model_cases.json` checks corrective validation without
requiring a model or touching the desktop.

```bash
cd desktop/apps/neuro-integration
go test ./... -run 'StartupContext|PromptAssist|DesktopGuide|AutomaticContext|ContextBounds|WeakModel'
```

For script syntax and valid examples, see the [language reference](action_script/LANGUAGE_REFERENCE.md).

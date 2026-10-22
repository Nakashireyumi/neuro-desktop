# Action Script Language Reference

Action scripts are short, ordered instructions for the controlled computer. They
run through the Python executor, using the same input, shell, and system
permissions as individual actions. A script can change the real desktop, start
programs, or run an approved command; it is not a sandbox or a way around policy.

## Calling and execution

When low-level controls are registered, call the public `run_script` action:

```json
{"script":"MOVE 640 360\nCLICK left\nTYPE \"hello\"\nPRESS enter"}
```

The bridge validates nested command permissions before acknowledging work. The
executor validates the whole script before appending it to one ordered queue.
Parsing does not type, click, run a shell command, release input, or launch a
program. Keyboard, mouse, timing, shell, and launch commands execute in source
order. `WAIT` pauses once; it is not duplicated between keyboard and mouse queues.

`execute_now` and `clear_after` both default to `true`: run the pending queue, then
discard it. For deliberate batching, set both to `false`, then use
`execute_queue {}` to run and clear it or `clear_action_queue {}` to discard it.
`clear_after:true` discards pending work even when `execute_now:false`; do not use
that combination to retain a script. Executing with `clear_after:false` retains
completed instructions, so a later drain can replay them. Prefer the defaults.

Acceptance means queued, not completed. Read the outcome message and request a
fresh `get_desktop_context {}` before a decision that depends on the resulting
screen. A timed-out request may already have affected the OS; inspect before
retrying. `reset_controls {}` clears pending input and releases held keys and
buttons, including while paused.

## Syntax and limits

- One command per line. Command names are case-insensitive.
- Blank lines and **whole-line** comments beginning with `#` are ignored.
- Inline comments are not supported. `WAIT 0.2 # wait` is invalid.
- Arguments use POSIX-style `shlex` quoting. Quote text and paths; escape literal
  backslashes in double quotes. JSON adds its own escaping when sending a script.
- Coordinates are signed 32-bit integers, with `(0,0)` at the screen's top-left;
  x grows right and y grows down. Normalized coordinates are finite values in
  `0..1`, mapped to the executor's screen geometry and clamped to its bounds.
- Maximum script size: **64 KiB of UTF-8**. Maximum retained queue: **512
  instructions**, including earlier deferred work.
- The combined pending queue must fit a **20-second estimated delay budget**.
  Typing, movement, holds, path steps, waits, and backend pauses count toward it.
  Execution also checks a cooperative 20-second wall-clock deadline between
  instructions; a blocking OS call can finish after that deadline.

A parse or policy failure does not append a partial script. Previously deferred
work remains queued. A runtime failure clears pending work and releases held
input. Already completed side effects cannot be rolled back. Disconnecting or
closing the updated executor also discards its pending session and releases input.

## Keyboard commands

| Syntax | Behavior |
| --- | --- |
| `TYPE "hello world"` | Types printable ASCII into the focused control, at about 20 ms per character. Non-ASCII or control characters are rejected before any character is typed; this is not Unicode text insertion and it never substitutes the clipboard. |
| `PRESS enter` | Presses and releases one supported key. |
| `ENTER` | Alias for pressing Enter. |
| `HOLD shift` | Holds a key until released; pair it with `RELEASE`. |
| `RELEASE shift` | Releases the named key. |
| `HOLD_FOR w 0.5` | Holds one key for a bounded duration, then releases it. Duration must be positive and fit the queue budget. |
| `COMBO ctrl c` | Presses a key combination. `SHORTCUT` is an alias. Use `command` instead of `ctrl` for the relevant macOS shortcut. |
| `RELEASE_ALL` | Releases held keys and mouse buttons at this point in the script. It does not discard later instructions. |

Key names include `enter`, `tab`, `space`, `backspace`, `delete`, `esc`, arrow
names, letters, digits, and supported function keys. `control`, `windows`,
`return`, and `escape` normalize to `ctrl`, `win`, `enter`, and `esc`. Unsupported
keys fail validation. A successful input call does not prove an application
received it: focus and OS permissions still matter.

```text
# Copy the current selection
COMBO ctrl c
WAIT 0.1
# Paste into the already focused target
COMBO ctrl v
```

## Mouse commands

| Syntax | Behavior |
| --- | --- |
| `MOVE x y [duration]` | Moves to absolute pixels. Optional duration defaults to `0.1` seconds. Movement may follow a smoothed path. |
| `MOVE_REL dx dy [duration]` | Moves by signed offsets; optional duration defaults to `0`, capped at `2` seconds. Useful for relative mouse-look. |
| `MOVE_N nx ny` | Moves to normalized coordinates, using the executor's actual screen size. |
| `CLICK [button]` | Clicks and releases at the current pointer position. Optional button is `left` (default), `right`, or `middle`. It takes **no coordinates**. |
| `CLICK_N nx ny [button]` | Moves to normalized coordinates, then clicks. |
| `LINE x1 y1 x2 y2 [STEPS n]` | Moves through interpolated points. Default 50 steps; optional integer steps `1..1000`. All steps count toward execution time. |
| `PATH x1 y1 x2 y2 ...` | Moves through 1 to 256 coordinate pairs; interpolation counts toward the time budget. |

```text
# Click at an absolute location
MOVE 500 300
CLICK left
WAIT 0.2
# Click near the center, independent of screen resolution
CLICK_N 0.5 0.5
```

`LINE` and `PATH` move the pointer without holding a button. They do not draw or
drag by themselves. `CLICK`, `MOVE`, `CLICK` is two clicks, not drag-and-drop.
There is no `DRAG` command or persistent mouse-button-down command in this script
grammar. Use an integration that exposes the needed operation instead of guessing
unsupported syntax. Normalized coordinates scale positions; they do not recognize
controls or compensate for layout changes.

## Timing, launch, and shell

| Syntax | Behavior |
| --- | --- |
| `WAIT seconds` | Pauses once at this point in execution. Finite nonnegative duration; the total queue must fit the 20-second budget. |
| `LAUNCH "target"` | Opens a path or URL through the OS opener. Requires the system scope; opening the target can have side effects. |
| `SHELL "echo hello"` | Runs one command line under the effective shell allowlist and deny rules. Requires the shell scope. Output is returned through the bridge. |

Shell lines cannot use chaining, pipes, or redirection. A shell line uses the
remaining execution budget as its timeout; this does not make every child process
or OS side effect reversible. Headless execution permits approved shell, launch,
wait, and release operations; it refuses desktop input without a usable display.

```text
# A launch occurs here, after validation and preceding commands
LAUNCH "https://example.com"
WAIT 0.5
```

On Windows, a literal path inside script double quotes needs escaped backslashes:

```text
LAUNCH "C:\\Users\\Neuro\\Documents"
```

## Desktop intents

The following zero-argument commands map to OS-specific shortcuts:

```text
OPEN_START_MENU
OPEN_WINDOWS_MENU
SHOW_DESKTOP
MINIMIZE_ALL_WINDOWS
CLOSE_FOREGROUND_APP
OPEN_TASK_MANAGER
CLOSE_ALL_APPS
OPEN_FILE_EXPLORER
OPEN_RUN_DIALOG
OPEN_SEARCH
SNAP_WINDOW_LEFT
SNAP_WINDOW_RIGHT
OPEN_SETTINGS
OPEN_WINDOWS_SETTINGS
OPEN_NOTIFICATION_CENTER
OPEN_CLIPBOARD_HISTORY
LOCK_WORKSTATION
SWITCH_APP_NEXT
SWITCH_APP_PREVIOUS
OPEN_POWER_USER_MENU
TAKE_SCREEN_SNIP
```

The `WINDOWS` aliases remain for compatibility. macOS uses alternate mappings
such as Spotlight for the launcher, while Linux mappings depend on desktop
shortcut settings. An intent with no real mapping is rejected before anything
runs instead of substituting a different action:

- `CLOSE_ALL_APPS` is unsupported everywhere; use individual window controls.
- On macOS, `OPEN_WINDOWS_MENU`, `MINIMIZE_ALL_WINDOWS`,
  `OPEN_WINDOWS_SETTINGS`, `OPEN_NOTIFICATION_CENTER`,
  `OPEN_POWER_USER_MENU` and `OPEN_CLIPBOARD_HISTORY` have no reliable native
  shortcut and are rejected.
- On Linux, `OPEN_POWER_USER_MENU` is rejected; other mappings depend on the
  desktop environment.

Some supported intents are still approximations: `OPEN_FILE_EXPLORER` on macOS
uses the new-folder shortcut and needs a suitable Finder context. Inspect the
current context before using an intent and observe its outcome. These names do
not guarantee equivalent behavior on every desktop configuration.

## A short workflow

Choose a target from a fresh observation first. The following example assumes a
text field is at the specified location and that Enter submits it:

```text
MOVE 500 300
CLICK left
TYPE "Neurcro"
PRESS enter
```

Request another observation after this chunk. Prefer small state-aware chunks to
guessing a long chain of coordinates and sleeps. Scripts have no variables,
conditions, loops, functions, image recognition, or built-in retry logic. The
caller observes and decides the next chunk.

## Verification and implementation

The executor grammar is in
[`actions.py`](../../desktop/python/controller/actions.py). The bridge's
matching grammar enforces nested permissions in
[`script_permissions.go`](../../headless/apps/neuro-integration/script_permissions.go).
Python tests cover ordered mixed input, whole-script preflight, deferred shell and
launch, single waits, limits, failure cleanup, and session isolation. Go tests
check bridge/executor grammar compatibility and nested permission refusal.

For model context and public actions, see the [LLM guide](../LLM_GUIDE.md).

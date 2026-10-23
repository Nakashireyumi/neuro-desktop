# Neurcro macros for Neuro Desktop

Use the [language reference](../../../../docs/action_script/LANGUAGE_REFERENCE.md)
for the complete grammar, limits, permissions, and valid examples. This compact
reference ships with the bridge for operators; it is not automatically injected
into Neuro's context. Neuro can request `desktop_guide {"topic":"script"}`.

## Execution

Call `run_script` only when it is registered. Acknowledge validation first; the
outcome follows as a message. The whole script is checked before it is appended
to one ordered queue. Keyboard, mouse, waits, shell, launch, and release commands
execute in source order. Parsing has no OS side effects; `WAIT` pauses once.

`execute_now` and `clear_after` both default to `true`. To retain pending work,
set both to `false`; `execute_queue {}` runs and clears it, while
`clear_action_queue {}` discards it. `clear_after:false` after execution retains
completed instructions, so a later drain can replay them. Prefer the defaults.

Limits: 64 KiB UTF-8 per script, 512 pending instructions, and a 20-second
estimated delay budget. A cooperative deadline also checks elapsed execution
time. Whole-script rejection leaves earlier deferred work intact; runtime failure
clears pending work and releases held input. Completed effects cannot be undone.

## Syntax

One case-insensitive command per line. Blank lines and whole-line `#` comments
are ignored. Inline comments are unsupported. Quote text and paths using
POSIX-style `shlex` rules; escape backslashes inside double quotes. JSON requires
its own escaping when passing a script as a string.

```text
# Focus a target identified from a fresh observation
MOVE 500 300
CLICK left
TYPE "Hello world"
PRESS enter
WAIT 0.2
COMBO ctrl c
RELEASE_ALL
```

`CLICK [left|right|middle]` clicks the current pointer; it never takes pixel
coordinates. Use `MOVE` first, or `CLICK_N nx ny [button]` for normalized
coordinates. `MOVE_N nx ny` uses the same `0..1` range. `MOVE_REL dx dy [duration]`
uses signed offsets. `LINE` and `PATH` only move the pointer; they do not hold a
mouse button. Two clicks separated by movement do not perform drag-and-drop.

Other keyboard commands are `ENTER`, `HOLD key`, `RELEASE key`, and
`HOLD_FOR key seconds`. `SHORTCUT` aliases `COMBO`. Match shortcuts to the OS;
desktop intents resolve through platform-specific mappings and may depend on
desktop settings. Input requires a usable display and OS input permissions.

`SHELL "echo hello"` requires the shell scope and effective program allowlist;
chaining, pipes, and redirection are refused. `LAUNCH "target"` uses the OS opener
and requires the system scope. Both are deferred until execution. This is a real
desktop automation interface, not a sandbox.

Scripts have no loops, conditionals, variables, functions, or screen-reading
commands. Request `get_desktop_context {}` outside the script when the next step
depends on screen state. Do not retry an uncertain timeout without inspecting the
outcome. `reset_controls {}` clears pending input and releases held controls,
including while paused; it cannot undo completed OS effects.

The payload schema is [`action-schema.run_script.json`](action-schema.run_script.json).

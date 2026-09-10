# Neuro Desktop Action Script System

**AI Context Specification**

This system defines a **line-based action scripting language** used to control keyboard and mouse input on a desktop environment.
Scripts are **parsed**, **validated**, **queued**, and then **executed sequentially**.

The language is intentionally simple, deterministic, and side-effect safe.

---

## 1. Execution Model (Critical)

1. Scripts are executed **top to bottom**, one line at a time.
2. Each command:

   * Is parsed
   * Recorded to a monitor (for auditing / feedback)
   * Converted into one or more queued instructions
3. **Nothing executes immediately** during parsing.
4. Execution happens later via:

   ```python
   keyboard.execute()
   mouse.execute()
   ```
5. Keyboard and mouse have **separate queues** but preserve internal order.

**Important:**
The AI must assume **real OS-level input**. Mistakes affect the real desktop.

---

## 2. Script Structure

* One command per line
* Case-insensitive command names
* Arguments are space-separated
* Quoted strings are supported (`shlex.split`)
* Blank lines and comments are ignored

### Comments

```text
# This is a comment
```

### JSON Schema

The `run_script` payload schema lives at:

```text
integration-docs/action-schema.run_script.json
```

---

## 3. Keyboard Commands

### TYPE

Types literal text.

```text
TYPE "Hello world"
```

* Quoted text required if spaces exist
* Simulates human typing
* Default character interval ~ 20ms

---

### ENTER

Presses the Enter key.

```text
ENTER
```

---

### PRESS

Presses and releases a single key.

```text
PRESS a
PRESS enter
PRESS backspace
```

---

### HOLD

Presses a key **without releasing** it.

```text
HOLD shift
```

---

### RELEASE

Releases a previously held key.

```text
RELEASE shift
```

---

### SHORTCUT

Presses multiple keys simultaneously (hotkey).

```text
SHORTCUT ctrl c
SHORTCUT ctrl shift esc
```

Order matters.

---

## 4. High-Level Desktop Commands

These commands map to common Windows intents and avoid raw coordinate usage.

### OPEN_WINDOWS_MENU / OPEN_START_MENU

```text
OPEN_WINDOWS_MENU
OPEN_START_MENU
```

Opens the Windows Start menu (`win` key press).

---

### SHOW_DESKTOP

```text
SHOW_DESKTOP
```

Shows desktop (`win + d`).

---

### MINIMIZE_ALL_WINDOWS

```text
MINIMIZE_ALL_WINDOWS
```

Minimizes all windows (`win + m`).

---

### CLOSE_FOREGROUND_APP

```text
CLOSE_FOREGROUND_APP
```

Closes currently focused application (`alt + f4`).

---

### OPEN_TASK_MANAGER

```text
OPEN_TASK_MANAGER
```

Opens Task Manager (`ctrl + shift + esc`).

---

### CLOSE_ALL_APPS

```text
CLOSE_ALL_APPS
```

Non-destructive fallback that maps to `SHOW_DESKTOP` behavior (`win + d`).

---
### OPEN_FILE_EXPLORER

```text
OPEN_FILE_EXPLORER
```

Opens File Explorer (`win + e`).

---

### OPEN_RUN_DIALOG

```text
OPEN_RUN_DIALOG
```

Opens Run dialog (`win + r`).

---

### OPEN_SEARCH

```text
OPEN_SEARCH
```

Opens Windows search (`win + s`).

---

### SNAP_WINDOW_LEFT

```text
SNAP_WINDOW_LEFT
```

Snaps active window to left half (`win + left`).

---

### SNAP_WINDOW_RIGHT

```text
SNAP_WINDOW_RIGHT
```

Snaps active window to right half (`win + right`).

---

### OPEN_WINDOWS_SETTINGS

```text
OPEN_WINDOWS_SETTINGS
```

Opens Windows Settings (`win + i`).

---

### OPEN_NOTIFICATION_CENTER

```text
OPEN_NOTIFICATION_CENTER
```

Opens notification center / quick settings (`win + a`).

---

### OPEN_CLIPBOARD_HISTORY

```text
OPEN_CLIPBOARD_HISTORY
```

Opens clipboard history (`win + v`).

---

### LOCK_WORKSTATION

```text
LOCK_WORKSTATION
```

Locks workstation (`win + l`).

---

### SWITCH_APP_NEXT

```text
SWITCH_APP_NEXT
```

Cycles to next app (`alt + tab`).

---

### SWITCH_APP_PREVIOUS

```text
SWITCH_APP_PREVIOUS
```

Cycles to previous app (`alt + shift + tab`).

---

### OPEN_POWER_USER_MENU

```text
OPEN_POWER_USER_MENU
```

Opens power-user menu (`win + x`).

---

### TAKE_SCREEN_SNIP

```text
TAKE_SCREEN_SNIP
```

Opens screen snipping overlay (`win + shift + s`).

---

## 5. Mouse Commands (Absolute Coordinates)

Screen coordinates are **pixel-based**, origin `(0, 0)` is top-left.

---

### MOVE

Moves the mouse to a position.

```text
MOVE x y [duration]
```

Examples:

```text
MOVE 500 400
MOVE 800 200 0.3
```

* Duration defaults to `0.1` seconds

---

### CLICK

Clicks at the current cursor position.

```text
CLICK [button]
```

Examples:

```text
MOVE 500 400
CLICK
CLICK right
```

Buttons:

* `left` (default)
* `right`
* `middle`

---

## 6. Normalized Mouse Commands (AI-Friendly)

Normalized coordinates range from **0.0 to 1.0**, relative to screen size.

---

### MOVE_N

```text
MOVE_N nx ny
```

Example:

```text
MOVE_N 0.5 0.5   # center of screen
```

---

### CLICK_N

```text
CLICK_N nx ny [button]
```

Example:

```text
CLICK_N 0.25 0.75
CLICK_N 0.25 0.75 right
```

---

## 7. Mouse Drawing Commands

Used for gestures, drags, drawing, or human-like motion.

---

### LINE

Draws a straight line.

```text
LINE x1 y1 x2 y2 [STEPS n]
```

Example:

```text
LINE 100 100 800 600 STEPS 100
```

* Default steps: `50`
* Higher steps = smoother motion

---

### PATH

Draws a connected polyline through points.

```text
PATH x1 y1 x2 y2 x3 y3 ...
```

Example:

```text
PATH 300 300 400 400 500 350
```

* Requires an even number of coordinates
* Internally interpolated

---

## 8. Timing Control

### WAIT

Pauses execution.

```text
WAIT seconds
```

Example:

```text
WAIT 0.5
```

This inserts:

* A keyboard wait
* A mouse wait

Used to:

* Allow UI updates
* Synchronize with animations
* Avoid race conditions

---

## 9. Error Handling Rules

If **any line fails**, parsing stops and raises:

```text
ActionParseError
```

Error messages include:

* Line number
* Raw command
* Reason for failure

The AI should:

* Prefer safe defaults
* Avoid malformed commands
* Avoid guessing argument counts

---

## 10. Safety & Constraints (AI MUST RESPECT)

* No conditionals
* No loops
* No variables
* No branching
* No system introspection
* No screen reading

This is a **pure output action language**, not a programming language.

---

## 11. Recommended AI Behavior

✔ Prefer `MOVE_N` / `CLICK_N` when screen size is unknown
✔ Insert `WAIT` after window-opening actions
✔ Use `SHORTCUT` for OS commands
✔ Keep scripts short and deterministic
✘ Do not spam clicks
✘ Do not assume UI state without waits

---

## 12. Example Full Script

```text
# Open browser
SHORTCUT ctrl l
WAIT 0.2
TYPE "https://google.com"
ENTER

WAIT 2.0

# Click search box (center-ish)
CLICK_N 0.5 0.3
WAIT 0.1

TYPE "Neuro-sama"
ENTER
```


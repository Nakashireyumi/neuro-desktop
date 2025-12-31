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

---

## 3. Keyboard Commands

### TYPE

Types literal text.

```text
TYPE "Hello world"
```

* Quoted text required if spaces exist
* Simulates human typing
* Default character interval ≈ 20ms

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

## 4. Mouse Commands (Absolute Coordinates)

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

Moves and clicks at a position.

```text
CLICK x y [button]
```

Examples:

```text
CLICK 500 400
CLICK 500 400 right
```

Buttons:

* `left` (default)
* `right`
* `middle`

---

## 5. Normalized Mouse Commands (AI-Friendly)

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
CLICK_N nx ny
```

Example:

```text
CLICK_N 0.25 0.75
```

---

## 6. Mouse Drawing Commands

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

## 7. Timing Control

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

## 8. Error Handling Rules

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

## 9. Safety & Constraints (AI MUST RESPECT)

* No conditionals
* No loops
* No variables
* No branching
* No system introspection
* No screen reading

This is a **pure output action language**, not a programming language.

---

## 10. Recommended AI Behavior

✔ Prefer `MOVE_N` / `CLICK_N` when screen size is unknown
✔ Insert `WAIT` after window-opening actions
✔ Use `SHORTCUT` for OS commands
✔ Keep scripts short and deterministic
✘ Do not spam clicks
✘ Do not assume UI state without waits

---

## 11. Example Full Script

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
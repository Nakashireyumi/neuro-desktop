# Action Script Language Reference

> **A simple, deterministic scripting language for desktop automation**

## Table of Contents

- [Overview](#overview)
- [Syntax](#syntax)
- [Commands](#commands)
  - [Keyboard Commands](#keyboard-commands)
  - [Mouse Commands](#mouse-commands)
  - [Timing Commands](#timing-commands)
- [Advanced Features](#advanced-features)
- [Best Practices](#best-practices)
- [Examples](#examples)
- [Limitations](#limitations)

## Overview

The Action Script Language is a **line-based, imperative language** designed for desktop automation. Scripts are parsed top-to-bottom, commands are queued, and then executed sequentially.

### Design Principles

- **Deterministic**: Same script → Same result (every time)
- **Safe**: No file I/O, no network, no loops
- **Simple**: One command per line
- **Readable**: Natural language-like syntax

### Execution Model

```
Script → Parser → Instruction Queue → Executor → OS
```

**Important**: Commands are **queued**, not executed immediately. Call `execute()` to run them.

## Syntax

### Basic Structure

```text
COMMAND arg1 arg2 arg3
```

- **Case-insensitive**: `TYPE`, `type`, `TyPe` all work
- **Space-separated**: Arguments split by spaces
- **Quoted strings**: Use quotes for multi-word text
- **Comments**: Lines starting with `#`

### Examples

```text
# This is a comment
TYPE "hello world"
MOVE 500 300 0.5
CLICK 800 600 left
```

### Quoting Rules

```text
# Quotes required for spaces
TYPE "Hello World"     # ✓ Correct
TYPE Hello World       # ✗ Wrong (2 arguments)

# Quotes optional for single words
TYPE hello             # ✓ Works
TYPE "hello"           # ✓ Also works

# Escape quotes (if needed)
TYPE "She said \"hi\""
```

## Commands

### Keyboard Commands

#### TYPE

Type literal text as if typing on keyboard.

**Syntax**: `TYPE "text"`

**Parameters**:
- `text` (string): Text to type

**Example**:
```text
TYPE "Hello, Neuro!"
TYPE "Multiple words need quotes"
TYPE "Special chars: !@#$%"
```

**Notes**:
- Default interval: 20ms between characters
- Simulates human typing speed
- Works with all printable characters

---

#### ENTER

Press the Enter/Return key.

**Syntax**: `ENTER`

**Example**:
```text
TYPE "google.com"
ENTER
```

---

#### PRESS

Press and release a single key.

**Syntax**: `PRESS key`

**Parameters**:
- `key` (string): Key name

**Common Keys**:
```text
PRESS a              # Letter
PRESS enter          # Enter key
PRESS backspace      # Backspace
PRESS delete         # Delete
PRESS tab            # Tab
PRESS escape         # Escape
PRESS space          # Space
PRESS up             # Arrow up
PRESS down           # Arrow down
PRESS left           # Arrow left
PRESS right          # Arrow right
PRESS home           # Home
PRESS end            # End
PRESS pageup         # Page Up
PRESS pagedown       # Page Down
PRESS f1             # Function keys (f1-f12)
```

---

#### HOLD

Press a key down without releasing.

**Syntax**: `HOLD key`

**Parameters**:
- `key` (string): Key to hold

**Example**:
```text
HOLD shift
PRESS a              # Types "A"
RELEASE shift
```

**Warning**: Always `RELEASE` after `HOLD`!

---

#### RELEASE

Release a previously held key.

**Syntax**: `RELEASE key`

**Parameters**:
- `key` (string): Key to release

**Example**:
```text
HOLD ctrl
PRESS c
RELEASE ctrl
```

---

#### SHORTCUT

Press multiple keys simultaneously (hotkey).

**Syntax**: `SHORTCUT key1 key2 ...`

**Parameters**:
- `keys` (variadic): Keys to press together

**Common Shortcuts**:
```text
SHORTCUT ctrl c          # Copy
SHORTCUT ctrl v          # Paste
SHORTCUT ctrl a          # Select all
SHORTCUT ctrl s          # Save
SHORTCUT ctrl z          # Undo
SHORTCUT alt f4          # Close window
SHORTCUT win d           # Show desktop
SHORTCUT ctrl shift esc  # Task manager
```

**Example**:
```text
# Copy text
SHORTCUT ctrl a
WAIT 0.1
SHORTCUT ctrl c
```

---

### High-Level Desktop Commands

These intent-style commands map to common Windows operations without raw
pixel coordinates.

#### OPEN_WINDOWS_MENU / OPEN_START_MENU

Open Windows Start menu.

**Syntax**: `OPEN_WINDOWS_MENU` or `OPEN_START_MENU`

---

#### SHOW_DESKTOP

Show desktop by minimizing all windows to taskbar.

**Syntax**: `SHOW_DESKTOP`

---

#### MINIMIZE_ALL_WINDOWS

Minimize all windows.

**Syntax**: `MINIMIZE_ALL_WINDOWS`

---

#### CLOSE_FOREGROUND_APP

Close the currently focused application (`alt + f4`).

**Syntax**: `CLOSE_FOREGROUND_APP`

---

#### OPEN_TASK_MANAGER

Open Task Manager (`ctrl + shift + esc`).

**Syntax**: `OPEN_TASK_MANAGER`

---

#### CLOSE_ALL_APPS

Non-destructive fallback that behaves like `SHOW_DESKTOP`.

**Syntax**: `CLOSE_ALL_APPS`

---

#### OPEN_FILE_EXPLORER

Open File Explorer (`win + e`).

**Syntax**: `OPEN_FILE_EXPLORER`

---

#### OPEN_RUN_DIALOG

Open Run dialog (`win + r`).

**Syntax**: `OPEN_RUN_DIALOG`

---

#### OPEN_SEARCH

Open Windows search (`win + s`).

**Syntax**: `OPEN_SEARCH`

---

#### SNAP_WINDOW_LEFT

Snap the active window to the left half (`win + left`).

**Syntax**: `SNAP_WINDOW_LEFT`

---

#### SNAP_WINDOW_RIGHT

Snap the active window to the right half (`win + right`).

**Syntax**: `SNAP_WINDOW_RIGHT`

---

#### OPEN_WINDOWS_SETTINGS

Open Windows Settings (`win + i`).

**Syntax**: `OPEN_WINDOWS_SETTINGS`

---

#### OPEN_NOTIFICATION_CENTER

Open Notification Center / Quick Settings (`win + a`).

**Syntax**: `OPEN_NOTIFICATION_CENTER`

---

#### OPEN_CLIPBOARD_HISTORY

Open clipboard history (`win + v`).

**Syntax**: `OPEN_CLIPBOARD_HISTORY`

---

#### LOCK_WORKSTATION

Lock the current workstation (`win + l`).

**Syntax**: `LOCK_WORKSTATION`

---

#### SWITCH_APP_NEXT

Cycle to next application (`alt + tab`).

**Syntax**: `SWITCH_APP_NEXT`

---

#### SWITCH_APP_PREVIOUS

Cycle to previous application (`alt + shift + tab`).

**Syntax**: `SWITCH_APP_PREVIOUS`

---

#### OPEN_POWER_USER_MENU

Open Windows power-user menu (`win + x`).

**Syntax**: `OPEN_POWER_USER_MENU`

---

#### TAKE_SCREEN_SNIP

Open snipping overlay for selecting a screenshot area (`win + shift + s`).

**Syntax**: `TAKE_SCREEN_SNIP`

---

### Mouse Commands

#### MOVE

Move mouse cursor to absolute coordinates.

**Syntax**: `MOVE x y [duration]`

**Parameters**:
- `x` (integer): X coordinate (pixels from left)
- `y` (integer): Y coordinate (pixels from top)
- `duration` (float, optional): Movement time in seconds (default: 0.1)

**Example**:
```text
MOVE 500 300           # Move to (500, 300) in 0.1s
MOVE 800 600 0.5       # Move to (800, 600) in 0.5s
MOVE 0 0               # Top-left corner
```

**Notes**:
- Screen origin (0,0) is top-left
- Coordinates are clamped to screen bounds
- Uses Bezier curves for human-like movement

---

#### MOVE_N

Move mouse using normalized coordinates (0.0 to 1.0).

**Syntax**: `MOVE_N nx ny`

**Parameters**:
- `nx` (float): Normalized X (0.0 = left edge, 1.0 = right edge)
- `ny` (float): Normalized Y (0.0 = top edge, 1.0 = bottom edge)

**Example**:
```text
MOVE_N 0.5 0.5      # Center of screen
MOVE_N 0.0 0.0      # Top-left corner
MOVE_N 1.0 1.0      # Bottom-right corner
MOVE_N 0.25 0.75    # 25% from left, 75% from top
```

**Benefits**:
- Resolution-independent
- Easier to reason about positions
- Good for AI agents (no screen size knowledge needed)

---

#### CLICK

Click mouse button at current position or specific coordinates.

**Syntax**: `CLICK [button]`

**Parameters**:
- `button` (string, optional): Mouse button (default: "left")

**Buttons**:
- `left` - Left mouse button
- `right` - Right mouse button
- `middle` - Middle mouse button

**Example**:
```text
MOVE 500 300
CLICK                # Left click at current position

CLICK left           # Explicit left click
CLICK right          # Right click (context menu)
CLICK middle         # Middle click
```

---

#### CLICK_N

Click at normalized coordinates.

**Syntax**: `CLICK_N nx ny [button]`

**Parameters**:
- `nx` (float): Normalized X
- `ny` (float): Normalized Y
- `button` (string, optional): Mouse button (`left`, `right`, `middle`)

**Example**:
```text
CLICK_N 0.5 0.5     # Click center of screen
CLICK_N 0.9 0.1     # Click near top-right
CLICK_N 0.9 0.1 right
```

---

#### LINE

Draw a straight line by moving mouse through intermediate points.

**Syntax**: `LINE x1 y1 x2 y2 [STEPS n]`

**Parameters**:
- `x1`, `y1` (integer): Start coordinates
- `x2`, `y2` (integer): End coordinates
- `STEPS n` (optional): Number of interpolation steps (default: 50)

**Example**:
```text
LINE 100 100 800 600            # 50 steps (smooth)
LINE 100 100 800 600 STEPS 100  # 100 steps (very smooth)
LINE 100 100 800 600 STEPS 10   # 10 steps (choppy)
```

**Use Cases**:
- Drawing applications
- Drag operations
- Signature simulation

---

#### PATH

Move mouse through multiple connected points (polyline).

**Syntax**: `PATH x1 y1 x2 y2 x3 y3 ...`

**Parameters**:
- `points` (variadic): Pairs of X,Y coordinates

**Example**:
```text
# Triangle
PATH 300 300 400 400 500 350

# Complex path
PATH 100 100 200 150 300 100 400 200 500 100
```

**Notes**:
- Requires even number of coordinates
- Each segment automatically interpolated
- Good for complex gestures

---

### Timing Commands

#### WAIT

Pause execution for specified duration.

**Syntax**: `WAIT seconds`

**Parameters**:
- `seconds` (float): Duration to wait

**Example**:
```text
SHORTCUT win
WAIT 0.5           # Wait for start menu
TYPE "notepad"
ENTER
WAIT 1.0           # Wait for notepad to open
TYPE "Hello!"
```

**Common Patterns**:
```text
WAIT 0.1    # Brief pause
WAIT 0.5    # Short delay
WAIT 1.0    # Medium delay
WAIT 2.0    # Long delay
```

---

## Advanced Features

### Complex Workflows

```text
# Open application and create new document
SHORTCUT win
WAIT 0.3
TYPE "word"
ENTER
WAIT 2.0
SHORTCUT ctrl n
WAIT 0.5
TYPE "Created by Neuro"
SHORTCUT ctrl s
```

### Conditional-Like Behavior

While the language doesn't support conditionals, you can work around it:

```text
# Try to click button A, if fails, try button B
# (Requires external retry logic)
CLICK_N 0.5 0.6
```

### Macro Sequences

```text
# Data entry macro
TYPE "John Doe"
PRESS tab
TYPE "john@example.com"
PRESS tab
TYPE "555-1234"
PRESS tab
PRESS enter
```

### Mouse Gestures

```text
# Circular gesture
PATH 500 300 550 350 600 300 550 250 500 300

# Drag and drop
MOVE 100 100
CLICK left
WAIT 0.1
MOVE 500 500
CLICK left
```

## Best Practices

### 1. Always Add Waits

```text
# ✗ Bad: No waits
SHORTCUT win
TYPE "notepad"
ENTER
TYPE "Hello"

# ✓ Good: Strategic waits
SHORTCUT win
WAIT 0.3
TYPE "notepad"
ENTER
WAIT 1.0
TYPE "Hello"
```

**Why?** UIs need time to update.

### 2. Use Normalized Coordinates for Portability

```text
# ✗ Bad: Hard-coded for 1920x1080
CLICK 960 540

# ✓ Good: Works on any resolution
CLICK_N 0.5 0.5
```

### 3. Keep Scripts Short

```text
# ✗ Bad: Monolithic script
# (200 lines of actions)

# ✓ Good: Break into logical chunks
# Script 1: Open app
# Script 2: Enter data
# Script 3: Save and close
```

### 4. Comment Your Intent

```text
# ✓ Good: Clear intent
# Open start menu
SHORTCUT win
WAIT 0.3

# Search for notepad
TYPE "notepad"
ENTER
WAIT 1.0

# Enter text
TYPE "Hello Neuro!"
```

### 5. Handle Edge Cases

```text
# Always escape special characters
TYPE "Email: user@example.com"
TYPE "Path: C:\\Users\\Neuro"

# Always wait after window opens
SHORTCUT win
WAIT 0.5  # Critical for slow systems
```

## Examples

### Example 1: Open Browser and Search

```text
# Open browser
SHORTCUT win
WAIT 0.3
TYPE "chrome"
ENTER
WAIT 2.0

# Navigate to Google
SHORTCUT ctrl l
WAIT 0.2
TYPE "google.com"
ENTER
WAIT 1.5

# Search
TYPE "Neuro-sama"
ENTER
```

### Example 2: Create and Save Document

```text
# Open Notepad
SHORTCUT win
TYPE "notepad"
ENTER
WAIT 1.0

# Write content
TYPE "Meeting Notes"
ENTER
ENTER
TYPE "- Discussed project timeline"
ENTER
TYPE "- Assigned tasks"
ENTER
TYPE "- Next meeting: Friday"

# Save
SHORTCUT ctrl s
WAIT 0.5
TYPE "meeting_notes.txt"
ENTER
```

### Example 3: Screenshot Workflow

```text
# Take screenshot
PRESS printscreen
WAIT 0.5

# Open Paint
SHORTCUT win
TYPE "paint"
ENTER
WAIT 1.5

# Paste
SHORTCUT ctrl v
WAIT 0.5

# Save
SHORTCUT ctrl s
WAIT 0.5
TYPE "screenshot.png"
ENTER
```

### Example 4: Data Entry Form

```text
# Navigate to first field
PRESS tab

# Fill form
TYPE "Neuro Desktop"
PRESS tab
TYPE "v0.0.3b"
PRESS tab
TYPE "AI Desktop Control"
PRESS tab

# Select dropdown (example)
PRESS space
WAIT 0.2
PRESS down
PRESS down
PRESS enter

# Submit
PRESS tab
PRESS enter
```

### Example 5: Drawing

```text
# Draw a square
LINE 200 200 400 200  # Top
LINE 400 200 400 400  # Right
LINE 400 400 200 400  # Bottom
LINE 200 400 200 200  # Left

# Draw a star
PATH 300 100 320 180 400 180 340 240 360 320 300 270 240 320 260 240 200 180 280 180
```

## Limitations

### What the Language **Cannot** Do

❌ **No conditionals**: No `if/else`, `switch`
❌ **No loops**: No `for`, `while`, `repeat`
❌ **No variables**: No state storage
❌ **No functions**: No reusable subroutines
❌ **No file I/O**: Cannot read/write files
❌ **No network**: Cannot make HTTP requests
❌ **No screen reading**: Cannot check pixel colors
❌ **No branching**: Strictly linear execution

### Design Rationale

These limitations are **intentional** for:
- **Safety**: Prevent infinite loops and resource exhaustion
- **Predictability**: Same script always does the same thing
- **Simplicity**: Easy for AI to generate and humans to read

### Working Within Limitations

**Instead of loops**, repeat commands:
```text
# Send multiple clicks
CLICK 500 300
WAIT 0.1
CLICK 500 300
WAIT 0.1
CLICK 500 300
```

**Instead of conditionals**, use external logic:
```python
# Python decides which script to run
if condition:
    run_script("script_a.txt")
else:
    run_script("script_b.txt")
```

**Instead of variables**, use command-line arguments:
```python
# Generate script dynamically
script = f"""
TYPE "{username}"
PRESS tab
TYPE "{password}"
ENTER
"""
run_script(script)
```

## Error Handling

### Syntax Errors

```text
# Error: Missing quotes
TYPE hello world
→ ActionParseError: TYPE requires quoted text

# Error: Wrong number of arguments
MOVE 500
→ ActionParseError: MOVE requires x y [duration]

# Error: Invalid command
NOTACOMMAND
→ ActionParseError: Unknown command: NOTACOMMAND
```

### Runtime Errors

```text
# Error: Coordinate out of bounds
MOVE -100 -100
→ Coordinates clamped to (0, 0)

# Error: Invalid key name
PRESS invalidkey
→ pyautogui.FailSafeException: Invalid key name
```

### Validation Best Practices

Always validate before execution:
```python
try:
    parser.parse(script)
    controller.execute()
except ActionParseError as e:
    print(f"Script error: {e}")
```

## Performance Tips

### Minimize WAITs

```text
# Slow (total: 5s)
WAIT 1.0
CLICK 500 300
WAIT 1.0
CLICK 600 400
WAIT 1.0

# Faster (total: 0.3s)
WAIT 0.1
CLICK 500 300
WAIT 0.1
CLICK 600 400
WAIT 0.1
```

### Batch Similar Actions

```text
# All clicks together
MOVE 100 100
CLICK
MOVE 200 200
CLICK
MOVE 300 300
CLICK
```

### Use Direct Coordinates

```text
# Faster
MOVE 500 300

# Slower (extra calculation)
MOVE_N 0.26 0.27  # Requires screen size lookup
```

## Conclusion

The Action Script Language is designed for **safe, predictable desktop automation**. Its simplicity makes it easy for AI models to generate and for humans to understand and debug.

**Key Takeaways**:
- One command per line
- Commands are queued, then executed
- No conditionals, loops, or variables (by design)
- Always add WAITs for UI responsiveness
- Use normalized coordinates for portability

For more examples, see the [examples directory](../examples/) or the [integration tests](../tests/).


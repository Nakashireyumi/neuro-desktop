# Neuro Desktop Integration Guide

## Architecture Overview

```
┌──────────────────┐
│   Neuro API      │  (WebSocket)
│  (ws://...)      │
└────────┬─────────┘
         │
         │ WebSocket
         │
┌────────▼──────────┐
│  Go Integration   │  (WebSocket Client + IPC)
│  go-neuro-int...  │
└────────┬──────────┘
         │
         │ File IPC (JSON)
         │ (neuro_ipc.json)
         │
┌────────▼──────────┐
│  Rust Binary      │  (Main Process)
│  neuro-desktop    │
└────────┬──────────┘
         │
         │ PyO3 FFI
         │
┌────────▼──────────┐
│  Python Driver    │  (pyautogui)
│  controller/      │
└───────────────────┘
```

## Components

### 1. **Go WebSocket Integration** (`native/go-neuro-integration/`)
- Connects to Neuro API via WebSocket
- Registers 5 action types:
  - `move_mouse`: Move cursor to X,Y
  - `click_mouse`: Click at position
  - `type_text`: Type text
  - `press_key`: Press keys with modifiers
  - `run_script`: Execute multi-command scripts
- Converts Neuro actions to IPC commands
- Sends results back to Neuro

### 2. **Rust IPC Handler** (`apps/neuro-desktop/src/ipc_handler.rs`)
- Reads JSON commands from `neuro_ipc.json`
- Executes commands via Python controller
- Writes JSON responses to `neuro_ipc.json.response`
- Runs in background thread (non-blocking)

### 3. **Python Controller** (`backend/python/controller/`)
- Existing cross-platform input controller:
- Action parser for script language
- Mouse controller with path drawing
- Keyboard controller with queuing
- Desktop monitor for telemetry

## Setup

### Prerequisites

```bash
# Go 1.25+
go version

# Rust
cargo --version

# Python 3.8+ with venv
python --version
```

## Running

### Development Mode

**Terminal 1 - Rust Binary:**
```powershell
# Set IPC path
$env:NEURO_IPC_FILE = "./neuro_ipc.json"

# Run Rust binary
cd apps/neuro-desktop
cargo run --release
```

**Terminal 2 - Go Integration:**
```powershell
# Set WebSocket URL and IPC path
$env:NEURO_SDK_WS_URL = "ws://localhost:8000"
$env:NEURO_IPC_FILE = "./neuro_ipc.json"

# Run Go integration
cd native/go-neuro-integration
./go-neuro-integration
```

**Terminal 3 - Randy (for testing):**
```bash
cd Randy
npm install
npm start
```

### Production Bundle

Update your `scripts/bundle/prod.ps1`:

```powershell
# ... existing code ...

# ---------- Build Go Integration ----------
Write-Host "Building Go integration..."
Push-Location native/go-neuro-integration
go build -o go-neuro-integration.exe main.go
Pop-Location

Copy-Item `
  native/go-neuro-integration/go-neuro-integration.exe `
  $DIST

# ... rest of existing code ...
```

## File IPC Protocol

### Command Format (Go → Rust)
Written to: `neuro_ipc.json`

```json
{
  "type": "mouse_move",
  "params": {
    "x": 500,
    "y": 300
  }
}
```

### Response Format (Rust → Go)
Written to: `neuro_ipc.json.response`

```json
{
  "success": true,
  "data": null,
  "error": null
}
```

## Action Types

### 1. Move Mouse
```json
{
  "type": "mouse_move",
  "params": { "x": 500, "y": 300 }
}
```

### 2. Click Mouse
```json
{
  "type": "mouse_click",
  "params": {
    "x": 500,
    "y": 300,
    "button": "left"
  }
}
```

### 3. Type Text
```json
{
  "type": "key_type",
  "params": { "text": "Hello from Neuro!" }
}
```

### 4. Press Key
```json
{
  "type": "key_press",
  "params": {
    "key": "enter",
    "modifiers": ["ctrl", "shift"]
  }
}
```

### 5. Run Script
```json
{
  "type": "run_script",
  "params": {
    "script": "TYPE \"git status\"\nENTER\nWAIT 0.5"
  }
}
```

## Script Language

Your Python controller already supports a powerful script language:

```
TYPE "hello world"
ENTER
WAIT 0.5
MOVE 400 300
CLICK 400 300
SHORTCUT ctrl c
PRESS escape
```

Commands supported:
- `TYPE "text"` - Type text
- `ENTER` - Press enter
- `PRESS key` - Press a key
- `SHORTCUT key1 key2 ...` - Press key combination
- `MOVE x y [duration]` - Move mouse
- `CLICK x y [button]` - Click mouse
- `WAIT seconds` - Wait/pause
- `LINE x1 y1 x2 y2` - Draw line
- `PATH x1 y1 x2 y2 x3 y3 ...` - Draw path

## Environment Variables

```powershell
# Neuro WebSocket URL
$env:NEURO_SDK_WS_URL = "ws://localhost:8000"

# IPC file path
$env:NEURO_IPC_FILE = "./neuro_ipc.json"
```

### File Structure

```
desktop/
├── apps/neuro-desktop/
│   └── src/
│       ├── main.rs          (updated)
│       ├── controller.rs    (your existing code)
│       └── ipc_handler.rs   (new)
├── native/
│   └── go-neuro-integration/
│       ├── main.go          (new)
│       └── go.mod           (new)
└── backend/python/
    └── controller/          (your existing code)
```

## Testing

### 1. Test IPC Manually

Write to `neuro_ipc.json`:
```json
{"type":"mouse_move","params":{"x":500,"y":500}}
```

Check `neuro_ipc.json.response`:
```json
{"success":true}
```

### 2. Test with Randy

1. Start Randy: `cd Randy && npm start`
2. Start Rust: `cargo run --release`
3. Start Go: `./go-neuro-integration`
4. Randy will force random actions

### 3. Test Script Execution

Have Neuro run:
```
TYPE "notepad"
ENTER
WAIT 1
TYPE "Hello from Neuro!"
```

## Benefits of This Architecture

1. **Separation of Concerns**
   - Go handles WebSocket complexity
   - Rust handles system integration
   - Python handles cross-platform input

2. **Simple IPC**
   - Just JSON files
   - No sockets or pipes
   - Easy to debug

3. **Your Existing Code**
   - No changes to Python controller
   - Minimal changes to Rust
   - Leverages all your work

4. **Scalable**
   - Easy to add new actions
   - Easy to extend script language
   - Easy to add error handling

## Troubleshooting

### "IPC file not found"
- Check `NEURO_IPC_FILE` environment variable
- Ensure Rust binary is running
- Check file permissions

### "Timeout waiting for Rust response"
- Rust binary may have crashed
- Check Rust logs for Python errors
- Verify Python controller is initialized

### "WebSocket connection failed"
- Check `NEURO_SDK_WS_URL` is correct
- Ensure Randy or Neuro API is running
- Check firewall settings

<!-- ## Next Steps

1. **Add More Actions**: Extend both Go and Rust to support new commands
2. **Add Vision**: Integrate screenshot analysis
3. **Add Telemetry**: Use your `DesktopMonitor` to send context to Neuro
4. **Add Safety**: Implement action rate limiting
5. **Add UI**: Show what Neuro is doing in your frontend -->

## Example Neuro Interactions

**User**: "Neuro, open notepad and write hello"

**Neuro calls**:
1. `run_script`: `"SHORTCUT win\nTYPE \"notepad\"\nENTER\nWAIT 1"`
2. `type_text`: `"Hello from Neuro!"`

**User**: "Click on the start button"

**Neuro calls**:
1. `move_mouse`: `{x: 10, y: 1070}` (bottom-left)
2. `click_mouse`: `{button: "left"}`

# Neuro Desktop - Complete Setup Guide

## Overview

The Rust binary now **automatically starts and manages** the Go integration. You only need to run one executable!

```
You run: neuro-desktop.exe
   ↓
Rust starts: go-neuro-integration.exe
   ↓
Both communicate via: neuro_ipc.json
```

## Quick Start

### 1. Initial Setup

```powershell
# Run the dev setup script (installs all dependencies)
.\scripts\setup-dev.ps1
```

### 2. Build Everything

```powershell
# Build all components
make all

# Or manually:
cd native/go-neuro-integration
go build -o go-neuro-integration.exe main.go
cd ../..

cd apps/neuro-desktop
cargo build --release
cd ../..
```

### 3. Bundle for Development

```powershell
# This copies Go binary + Python + frontend to Rust's target/release
.\scripts\bundle\dev.ps1
```

### 4. Run

```powershell
# Just run the Rust binary - it handles everything!
cd apps/neuro-desktop/target/release
.\neuro-desktop.exe
```

That's it! The Rust binary will:
1. ✅ Initialize Python controllers
2. ✅ Start IPC handler  
3. ✅ Launch Go integration automatically
4. ✅ Monitor and restart Go if it crashes

## Project Structure

```
desktop/
├── apps/neuro-desktop/          # Main Rust application
│   └── src/
│       ├── main.rs              # Entry point + Go manager
│       ├── controller.rs        # Python FFI (your existing code)
│       ├── ipc_handler.rs       # IPC command processor (NEW)
│       └── go_manager.rs        # Go process manager (NEW)
│
├── native/
│   └── go-neuro-integration/    # Go WebSocket client (NEW)
│       ├── main.go              # Neuro API connector
│       └── go.mod
│
└── backend/python/controller/   # Python drivers (your existing code)
    ├── lib.py
    ├── actions.py
    ├── mouse.py
    ├── keyboard.py
    └── desktop.py
```

## What Each Component Does

### Rust Binary (`neuro-desktop.exe`)
- **Main orchestrator**
- Initializes Python controllers
- Spawns Go integration as child process
- Monitors Go health and restarts if needed
- Handles IPC commands from Go
- Executes actions via Python

### Go Integration (`go-neuro-integration.exe`)
- **Neuro API client**
- Connects to Neuro WebSocket
- Registers 5 action types
- Converts Neuro actions → IPC commands
- Sends results back to Neuro
- **Managed automatically by Rust**

### Python Controller (already exists)
- **Cross-platform input driver**
- Mouse/keyboard control via pyautogui
- Powerful script language
- Action telemetry
- **Called by Rust via PyO3**

## Development Workflow

### Daily Development

```powershell
# Terminal 1: Randy (Neuro mock server)
cd Randy
npm start

# Terminal 2: Your app
cd apps/neuro-desktop/target/release
.\neuro-desktop.exe
```

The Rust binary will automatically start Go and connect to Randy!

### Making Changes

**Changed Rust code?**
```powershell
cd apps/neuro-desktop
cargo build --release
cd target/release
.\neuro-desktop.exe
```

**Changed Go code?**
```powershell
cd native/go-neuro-integration
go build
cd ../../apps/neuro-desktop/target/release
copy ..\..\..\..\..\native\go-neuro-integration\go-neuro-integration.exe .
.\neuro-desktop.exe
```

**Changed Python code?**
```powershell
# Just re-run! Python is interpreted
cd apps/neuro-desktop/target/release
.\neuro-desktop.exe
```

### Full Rebuild

```powershell
# Clean everything
make clean

# Build everything
make all

# Bundle for dev
.\scripts\bundle\dev.ps1

# Run
cd apps/neuro-desktop/target/release
.\neuro-desktop.exe
```

## Production Build

### Create Distribution Package

```powershell
.\scripts\bundle\prod.ps1
```

This creates `dist/neuro-desktop/` with:
```
neuro-desktop/
├── neuro-desktop.exe          ← Main binary
├── go-neuro-integration.exe   ← Bundled automatically
├── python/                     ← Python runtime
│   ├── Lib/                    ← Dependencies
│   └── controller/             ← Your drivers
├── frontend/                   ← Web UI
├── README.txt                  ← Instructions
└── start.bat                   ← Easy launcher
```

### Distribute

1. Zip the entire `dist/neuro-desktop/` folder
2. Users extract and run `neuro-desktop.exe`
3. Everything works out of the box!

## Configuration

### Environment Variables

```powershell
# WebSocket URL (optional, default: ws://localhost:8000)
$env:NEURO_SDK_WS_URL = "ws://your-neuro-api:8080"

# IPC file path (optional, auto-generated in exe directory)
$env:NEURO_IPC_FILE = "C:\custom\path\neuro_ipc.json"

# Run
.\neuro-desktop.exe
```

### For End Users

No configuration needed! Just:
1. Extract zip
2. Run `neuro-desktop.exe`
3. It connects to `ws://localhost:8000` by default

## Monitoring

### Console Output

```
=======================================================
           Neuro Desktop Control System
=======================================================

Configuration:
  - Neuro WebSocket: ws://localhost:8000
  - IPC File:        C:\...\neuro_ipc.json

[1/3] Initializing Python controller drivers...
      ✓ Python drivers loaded

[2/3] Starting IPC handler...
      ✓ IPC handler running on: C:\...\neuro_ipc.json

[3/3] Starting Go WebSocket integration...
Starting Go integration at: C:\...\go-neuro-integration.exe
Go integration started with PID: 12345
      ✓ Go integration connected to Neuro

=======================================================
  Neuro Desktop is ready!
  Neuro can now control your computer.
=======================================================

Press Ctrl+C to stop
```

### Health Monitoring

The Rust binary checks Go every 5 seconds:
- If Go crashes → **Auto-restarts**
- If Go restart fails → **Exits gracefully**

## Troubleshooting

### "Go integration binary not found"

**Cause**: `go-neuro-integration.exe` not in same folder as `neuro-desktop.exe`

**Fix**:
```powershell
# Rebuild the bundle
.\scripts\bundle\dev.ps1

# Or manually copy
copy native\go-neuro-integration\go-neuro-integration.exe apps\neuro-desktop\target\release\
```

### "Failed to initialize controller drivers"

**Cause**: Python dependencies missing

**Fix**:
```powershell
.\scripts\setup-dev.ps1
.\scripts\bundle\dev.ps1
```

### "Go integration crashed! Attempting restart..."

**Cause**: Go couldn't connect to Neuro API

**Fix**:
1. Start Randy: `cd Randy && npm start`
2. Or set correct URL: `$env:NEURO_SDK_WS_URL = "ws://..."`

### Go keeps restarting in loop

**Cause**: WebSocket URL is wrong or unreachable

**Fix**:
```powershell
# Check Randy/Neuro is running
curl ws://localhost:8000

# Set correct URL
$env:NEURO_SDK_WS_URL = "ws://correct-url:port"
```

## Testing

### Test IPC Manually

While Rust is running, create `neuro_ipc.json`:
```json
{"type":"mouse_move","params":{"x":800,"y":600}}
```

Check console for:
```
IPC: Executing mouse_move
Mouse moved to (800, 600)
```

### Test with Randy

```powershell
# Terminal 1
cd Randy
npm start

# Terminal 2  
cd apps/neuro-desktop/target/release
.\neuro-desktop.exe

# Watch Randy send random actions!
```

### Test Script Execution

In Randy's console or via Neuro:
```javascript
// Force action
{
  "action": "run_script",
  "params": {
    "script": "TYPE \"Hello Neuro!\"\nENTER"
  }
}
```

## Advanced: Cross-Platform Build

Build Go for all platforms:

```powershell
.\scripts\bundle\build-go.ps1
```

```powershell
.\scripts\bundle\build-go.ps1
```

Outputs:
- `go-neuro-integration-windows-amd64.exe`
- `go-neuro-integration-linux-amd64`
- `go-neuro-integration-darwin-amd64`
- `go-neuro-integration-darwin-arm64`

Then update `go_manager.rs` to use the right binary for each OS.

<!-- ## What's Next?

1. **Add UI**: Show Neuro's actions in your frontend
2. **Add Safety**: Implement action rate limiting  
3. **Add Vision**: Send screenshots to Neuro
4. **Add Telemetry**: Use your DesktopMonitor data
5. **Add Shortcuts**: Hotkeys to pause/resume Neuro -->

## Example Session

```powershell
PS> .\neuro-desktop.exe
=======================================================
           Neuro Desktop Control System
=======================================================

Configuration:
  - Neuro WebSocket: ws://localhost:8000
  - IPC File:        C:\Users\You\neuro_ipc.json

[1/3] Initializing Python controller drivers...
      ✓ Python drivers loaded

[2/3] Starting IPC handler...
      ✓ IPC handler running

[3/3] Starting Go WebSocket integration...
Go integration started with PID: 15420
      ✓ Go integration connected to Neuro

=======================================================
  Neuro Desktop is ready!
=======================================================

Press Ctrl+C to stop

# Neuro does her thing...
IPC: Executing mouse_move (800, 600)
IPC: Executing type_text "Hello from Neuro!"
IPC: Executing run_script

# You press Ctrl+C
^C
Shutting down...
Stopping Go integration...
Go integration stopped
Neuro Desktop stopped
```

Perfect! 🎉
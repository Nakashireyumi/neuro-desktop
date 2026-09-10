# Neuro Desktop Architecture

> **Deep dive into the technical architecture and design decisions of Neuro Desktop**

## Table of Contents

- [Overview](#overview)
- [System Design](#system-design)
- [Component Architecture](#component-architecture)
- [Communication Patterns](#communication-patterns)
- [Data Flow](#data-flow)
- [Security Model](#security-model)
- [Performance Considerations](#performance-considerations)
- [Design Decisions](#design-decisions)

## Overview

Neuro Desktop is a **bridge + executor** architecture:

- **Bridge (`neuro-integration`, Go)** — WebSocket client of the
  [Neuro API](https://github.com/VedalAI/neuro-sdk). Registers actions, enforces
  Vedal's permission policy, and returns `action/result` promptly (SDK requires
  results within ~20s; long work happens on the executor).
- **Executor (`neuro-desktop` + Python)** — Runs on the controlled machine.
  Receives validated IPC commands and drives mouse/keyboard/scripts.

Both usually co-locate today (file IPC). The split is the seam for a future
remote executor under a managed bridge.

### Design Philosophy

1. **Right Tool for the Job**: Each component uses the language best suited for its task
2. **Fault Isolation**: Process boundaries prevent cascading failures
3. **Simple Communication**: JSON-based IPC for easy debugging and extensibility
4. **Graceful Degradation**: Automatic recovery from component failures
5. **Safety First**: Permission scopes + allow/deny lists before execution
6. **Cross-Platform Intents**: High-level actions map to OS-specific shortcuts

## System Design

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Neuro API                               │
│                    (External WebSocket)                         │
└───────────────────────────────┬─────────────────────────────────┘
                                │
                                │ WS Protocol
                                │ (JSON Messages)
                                │
┌───────────────────────────────▼─────────────────────────────────┐
│                     Go Integration                              │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  • WebSocket Client (gorilla/websocket)                 │   │
│  │  • Action Registry (15+ action types)                   │   │
│  │  • Message Router                                       │   │
│  │  • IPC Writer                                          │   │
│  └─────────────────────────────────────────────────────────┘   │
└───────────────────────────────┬─────────────────────────────────┘
                                │
                                │ File IPC
                                │ (neuro_ipc.json)
                                │
┌───────────────────────────────▼─────────────────────────────────┐
│                     Rust Main Process                           │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  IPC Handler Thread                                     │   │
│  │  • File Watcher (50ms poll)                            │   │
│  │  • JSON Parser & Validator                             │   │
│  │  • Command Router                                      │   │
│  │  • Response Writer                                     │   │
│  └─────────────────────────────────────────────────────────┘   │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  Go Process Manager                                     │   │
│  │  • Spawns Go integration as child                      │   │
│  │  • Health monitoring (5s heartbeat)                    │   │
│  │  • Auto-restart on crash (max 5 attempts)              │   │
│  └─────────────────────────────────────────────────────────┘   │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  Python FFI Bridge (PyO3)                               │   │
│  │  • Python interpreter embedded                          │   │
│  │  • GIL-safe calls                                       │   │
│  │  • Exception handling                                   │   │
│  └─────────────────────────────────────────────────────────┘   │
└───────────────────────────────┬─────────────────────────────────┘
                                │
                                │ PyO3 FFI
                                │ (Function Calls)
                                │
┌───────────────────────────────▼─────────────────────────────────┐
│                  Python Controller                              │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  Action Parser                                          │   │
│  │  • Lexer (shlex)                                        │   │
│  │  • Command dispatcher                                   │   │
│  │  • Error handling                                       │   │
│  └─────────────────────────────────────────────────────────┘   │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  Mouse Controller                                       │   │
│  │  • Instruction queue                                    │   │
│  │  • Algorithmic pathfinding (Bézier + Perlin)          │   │
│  │  • Coordinate clamping                                  │   │
│  └─────────────────────────────────────────────────────────┘   │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  Keyboard Controller                                    │   │
│  │  • Instruction queue                                    │   │
│  │  • Key mapping                                          │   │
│  │  • Timing control                                       │   │
│  └─────────────────────────────────────────────────────────┘   │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  Desktop Monitor                                        │   │
│  │  • Mouse tracking (pynput)                              │   │
│  │  • Action history (1000 events)                         │   │
│  │  • Window detection                                     │   │
│  └─────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

## Component Architecture

### Go Integration Layer

**Purpose**: Bridge between Neuro API and local system

**Responsibilities**:
- Maintain WebSocket connection to Neuro
- Register available actions with Neuro
- Parse incoming action commands
- Translate to IPC format
- Handle action results

**Key Files**:
- `main.go` - Entry point, WebSocket loop
- `action-registry.go` - Action schema definitions
- `action-handling.go` - Action → IPC translation
- `utils.go` - Helper functions
- `types.go` - Data structures

**Design Patterns**:
- Event loop with goroutines
- Channel-based communication
- Graceful shutdown handling

**Error Handling**:
```go
// Automatic reconnection
for {
    err := integration.listen()
    if err != nil {
        log.Printf("Connection lost: %v", err)
        time.Sleep(5 * time.Second)
        integration.reconnect()
    }
}
```

### Rust Main Process

**Purpose**: System integration and process orchestration

**Responsibilities**:
- Start and monitor Go integration
- Process IPC commands
- Bridge to Python via FFI
- Manage application lifecycle

**Key Files**:
- `main.rs` - Entry point, orchestration
- `ipc_handler.rs` - IPC command processor
- `controller.rs` - Python FFI bridge
- `go_manager.rs` - Go process lifecycle

**Design Patterns**:
- Actor model (threads with message passing)
- Builder pattern for initialization
- RAII for resource management

**Thread Architecture**:
```
Main Thread
├── IPC Handler Thread (background)
│   └── Polls neuro_ipc.json every 50ms
├── Go Process Monitor Thread (background)
│   └── Health check every 5s
└── Signal Handler (Ctrl+C)
```

**Safety Features**:
- Mutex-protected state
- AtomicBool for shutdown coordination
- Panic handlers with recovery

### Python Controller Layer

**Purpose**: Cross-platform input control

**Responsibilities**:
- Parse action scripts
- Control mouse and keyboard
- Track telemetry
- Provide cross-platform abstraction

**Key Files**:
- `lib.py` - Initialization
- `actions.py` - Script parser
- `controls/mouse.py` - Mouse control
- `controls/keyboard.py` - Keyboard control
- `desktop.py` - System monitoring
- `libraries/mouse_pathfinder.py` - Motion algorithms

**Design Patterns**:
- Command pattern (instruction queues)
- Strategy pattern (different motion algorithms)
- Observer pattern (telemetry)

**Instruction Queue System**:
```python
# Instructions are queued, not executed immediately
mouse.queue_move(500, 300)
mouse.queue_click("left")
mouse.queue_wait(0.5)

# Execute all at once
mouse.execute()
mouse.clear()
```

## Communication Patterns

### File-Based IPC

**Why Files?**
- ✅ Simple to implement and debug
- ✅ No port conflicts
- ✅ Easy to inspect (just open the JSON)
- ✅ Works across all platforms
- ❌ Slower than pipes/sockets (~50ms latency)

**Protocol**:

1. **Command File** (`neuro_ipc.json`):
```json
{
  "type": "move_mouse_to",
  "params": {
    "x": 500,
    "y": 300
  },
  "execute_now": true,
  "clear_after": true
}
```

2. **Response File** (`neuro_ipc.json.response`):
```json
{
  "success": true,
  "data": null,
  "error": null,
  "timestamp": 1704067200,
  "execution_time_ms": 12
}
```

**Lifecycle**:
```
Go                    Rust                    Python
│                     │                       │
├─ Write command ────>│                       │
│                     ├─ Read & parse         │
│                     ├─ Validate             │
│                     ├─ Call Python ────────>│
│                     │                       ├─ Execute
│                     │<──── Return ──────────┤
│                     ├─ Write response       │
│<─── Read ───────────┤                       │
├─ Delete files       │                       │
```

### PyO3 FFI Bridge

**Why PyO3?**
- ✅ Type-safe Rust ↔ Python calls
- ✅ Automatic GIL handling
- ✅ Exception propagation
- ✅ Zero-copy data transfer (where possible)

**Example Call**:
```rust
// Rust side
Python::with_gil(|py| {
    self.keyboard
        .bind(py)
        .getattr("type")?
        .call1((text,))?;
    Ok::<(), PyErr>(())
})
```

```python
# Python side
class KeyboardController:
    def type(self, text: str):
        self.queue.append(TypeText(text))
```

### WebSocket Protocol

**Message Format** (Neuro → Go):
```json
{
  "command": "action",
  "data": {
    "id": "action-12345",
    "name": "move_mouse_to",
    "data": "{\"x\": 500, \"y\": 300}"
  }
}
```

**Message Format** (Go → Neuro):
```json
{
  "command": "action/result",
  "game": "Neuro Desktop",
  "data": {
    "id": "action-12345",
    "success": true,
    "message": ""
  }
}
```

## Data Flow

### Action Execution Flow

```
Neuro API
    │
    │ 1. Send action command
    │    {"name": "type_text", "data": {...}}
    ▼
Go Integration
    │
    │ 2. Parse action data
    │    Extract parameters
    ▼
    │ 3. Build IPC command
    │    Convert to standard format
    ▼
    │ 4. Write to neuro_ipc.json
    │    {"type": "type_text", "params": {...}}
    ▼
Rust IPC Handler
    │
    │ 5. Read & validate JSON
    │    Check syntax, schema
    ▼
    │ 6. Dispatch to handler
    │    Route by command type
    ▼
    │ 7. Call Python (PyO3)
    │    controller.type_text("...")
    ▼
Python Controller
    │
    │ 8. Queue instruction
    │    keyboard.queue.append(TypeText(...))
    ▼
    │ 9. Execute instruction
    │    pyautogui.write(...)
    ▼
    │ 10. Return result
    │     Ok(()) or Err(...)
    ▼
Rust IPC Handler
    │
    │ 11. Write response
    │     {"success": true, ...}
    ▼
Go Integration
    │
    │ 12. Read response
    │     Parse JSON
    ▼
    │ 13. Send result to Neuro
    │     {"id": "...", "success": true}
    ▼
Neuro API
```

**Timing**:
- Average latency: ~50-100ms
- Breakdown:
  - Go → Rust (file write): ~5ms
  - Rust polling: ~0-50ms (depends on timing)
  - Python execution: ~10-50ms (varies by action)
  - Response write: ~5ms

### Error Propagation

```
Python Exception
    │
    ├─> PyO3 catches
    │   └─> Converts to Result<T, PyErr>
    │
    ├─> Rust handler catches
    │   └─> Creates error response
    │       {"success": false, "error": "..."}
    │
    ├─> Go reads error response
    │   └─> Sends to Neuro
    │       {"success": false, "message": "..."}
    │
    └─> Neuro retries or logs
```

## Security Model

### Input Validation

**Layer 1: Go Integration**
- JSON schema validation
- Parameter type checking
- Range bounds enforcement

**Layer 2: Rust IPC Handler**
- JSON syntax validation
- Command whitelist check
- Payload size limits (5MB max)
- Rate limiting (100 commands/sec)

**Layer 3: Python Controller**
- Coordinate clamping
- Text length limits
- Key name validation

### Sandboxing

**Process Isolation**:
- Go runs as separate process
- Can be killed without affecting Rust
- No shared memory (only files)

**Python Isolation**:
- Embedded interpreter (controlled by Rust)
- No filesystem access beyond controller package
- No network access
- No subprocess spawning (except pyautogui internals)

### Safe Shutdown

```rust
// Graceful shutdown on Ctrl+C
tokio::select! {
    _ = tokio::signal::ctrl_c() => {
        println!("Shutting down...");
        
        // 1. Stop accepting new commands
        ipc_handler.store(false, Ordering::SeqCst);
        
        // 2. Stop Go integration
        go_manager.stop();
        
        // 3. Wait for pending actions
        thread::sleep(Duration::from_millis(500));
        
        // 4. Clean up Python
        controller.shutdown()?;
        
        // 5. Exit
        break;
    }
}
```

## Performance Considerations

### Optimization Strategies

**1. Lazy Execution**
```python
# Commands are queued, not executed immediately
mouse.queue_move(100, 100)  # Fast: just appends to list
mouse.queue_move(200, 200)
mouse.queue_move(300, 300)

# Execute all at once
mouse.execute()  # Batch execution reduces overhead
```

**2. Instruction Batching**
```python
# Bad: Execute after each action
for i in range(100):
    mouse.queue_move(i, i)
    mouse.execute()  # 100 executions!

# Good: Execute once at the end
for i in range(100):
    mouse.queue_move(i, i)
mouse.execute()  # 1 execution
```

**3. Adaptive Motion**
```python
# Distance-based duration
distance = math.sqrt(dx**2 + dy**2)
duration = 0.0005 + (distance / 1000) * 0.3
duration = min(duration, 0.0012)  # Cap at max

# Fewer steps for short distances
steps = max(15, int(distance / 10))
steps = min(steps, 60)  # Cap for long distances
```

### Bottlenecks

**Identified**:
1. File I/O polling (50ms worst case)
2. JSON serialization/deserialization
3. Python GIL contention
4. pyautogui internal delays

**Mitigation**:
1. Could use named pipes (lower latency)
2. Could use binary format (faster parsing)
3. Release GIL where possible
4. Set `pyautogui.PAUSE = 0`

### Benchmarks

**Action Latency** (Go → Python execution):
```
move_mouse_to:    50-100ms
click:            50-80ms
type_text:        50ms + 20ms/char
run_script:       varies (depends on script)
```

**Throughput**:
```
Commands/sec:     ~100 (rate limited)
Mouse movements:  ~60 steps/sec (smooth)
Key presses:      ~50 keys/sec
```

## Design Decisions

### Why Multi-Language?

**Go for Neuro API**:
- ✅ Excellent WebSocket libraries
- ✅ Goroutines for concurrent connections
- ✅ Easy JSON handling
- ✅ Simple deployment (single binary)

**Rust for Main Process**:
- ✅ System-level control
- ✅ FFI capabilities (PyO3)
- ✅ Memory safety
- ✅ Process management
- ✅ Zero-cost abstractions

**Python for Input Control**:
- ✅ pyautogui (battle-tested)
- ✅ Cross-platform support
- ✅ Rapid development
- ✅ Easy to extend

### Why File-Based IPC?

**Alternatives Considered**:
1. ❌ Named Pipes - Platform-specific code
2. ❌ TCP Sockets - Port conflicts, overhead
3. ❌ Shared Memory - Complex, unsafe
4. ✅ **Files - Simple, debuggable, works everywhere**

Trade-off: Simplicity over performance

### Why Instruction Queues?

**Benefits**:
- Atomic execution of complex sequences
- Easy to inspect before execution
- Can be cleared/modified before running
- Separates intent from execution

**Drawback**:
- Extra memory for queue storage

Trade-off: Flexibility over immediate execution

### Future Improvements

1. **Optional STDIO IPC**: Faster than files
2. **Binary Protocol**: Reduce JSON overhead
3. **Shared Memory for Vision**: Large image data
4. **Plugin System**: External action providers
5. **Distributed Mode**: Control remote machines

## Conclusion

Neuro Desktop's architecture prioritizes:
1. **Reliability** - Multiple recovery mechanisms
2. **Debuggability** - Clear data flow, inspectable state
3. **Safety** - Validation at every layer
4. **Extensibility** - Easy to add new actions
5. **Cross-platform** - Works on Windows, Linux, macOS

The multi-language approach allows each component to use the best tool for its job while maintaining clear boundaries and simple communication protocols.

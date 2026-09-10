# Neuro Desktop Process Handler - Architecture Documentation

## Table of Contents
1. [Overview](#overview)
2. [Architecture](#architecture)
3. [Process Communication](#process-communication)
4. [Adding New Processes](#adding-new-processes)
5. [Testing Strategy](#testing-strategy)
6. [Deployment](#deployment)
7. [Troubleshooting](#troubleshooting)

## Overview

The Process Handler is the central orchestrator for Neuro Desktop. It manages all child processes, handles inter-process communication, monitors health, and provides automatic recovery from crashes.

### Key Benefits

- **Centralized Management**: Single point of control for all processes
- **Automatic Recovery**: Crashed processes are automatically restarted
- **Flexible Communication**: Multiple IPC methods (File, STDIO, Named Pipes, etc.)
- **Health Monitoring**: Continuous health checks with heartbeat mechanism
- **Easy Extension**: Simple API to add new processes

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│              Process Handler (C++)                      │
│  ┌───────────────────────────────────────────────────┐  │
│  │           Process Manager                         │  │
│  │  - Lifecycle Management                          │  │
│  │  - Health Monitoring                             │  │
│  │  - Dependency Resolution                         │  │
│  └───────────────────────────────────────────────────┘  │
│  ┌───────────────────────────────────────────────────┐  │
│  │           Message Router                          │  │
│  │  - Command Routing                               │  │
│  │  - Message Validation                            │  │
│  │  - Handler Registration                          │  │
│  └───────────────────────────────────────────────────┘  │
│  ┌───────────────────────────────────────────────────┐  │
│  │           Communication Hub                       │  │
│  │  - File IPC                                      │  │
│  │  - STDIO Pipes                                   │  │
│  │  - Named Pipes                                   │  │
│  │  - Shared Memory                                 │  │
│  └───────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
                          │
        ┌─────────────────┼─────────────────┐
        │                 │                 │
        ▼                 ▼                 ▼
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│ Rust Main    │  │ Go           │  │ Python       │
│ (neuro-      │  │ Integration  │  │ Controller   │
│  desktop)    │  │              │  │              │
└──────────────┘  └──────────────┘  └──────────────┘
```

### Component Responsibilities

#### Process Handler (C++)
- **Main Orchestrator**: Spawns and manages all child processes
- **Health Monitor**: Checks process health via heartbeats
- **Message Router**: Routes messages between processes
- **Crash Recovery**: Automatically restarts failed processes

#### Rust Main (neuro-desktop)
- **Python Integration**: Manages Python controller via PyO3
- **IPC Handler**: Processes commands from Go integration
- **Action Execution**: Executes mouse/keyboard actions

#### Go Integration (neuro-integration)
- **Neuro API Client**: Connects to Neuro WebSocket
- **Action Registration**: Registers available actions
- **Command Translation**: Converts Neuro actions to IPC commands

#### Python Controller
- **Input Execution**: Controls mouse and keyboard
- **Script Parsing**: Parses and executes action scripts
- **Cross-Platform**: Works on Windows, Linux, macOS

## Process Communication

### Communication Methods

#### 1. File IPC
Best for: Simple command-response patterns

```cpp
FileIPCChannel channel("ipc_process.json");
channel.send(message);
```

**Pros:**
- Simple to debug (just read the JSON file)
- No complex setup required
- Works across all platforms

**Cons:**
- Slower than other methods
- File system overhead

#### 2. STDIO Pipes
Best for: High-throughput communication

```cpp
StdioChannel channel;
channel.send(message);
channel.receive(response, 1000);
```

**Pros:**
- Fast and efficient
- Built into process spawning
- Low overhead

**Cons:**
- Harder to debug
- No persistence

#### 3. Named Pipes
Best for: Low-latency bidirectional communication

```cpp
NamedPipeChannel channel("neuro_pipe");
channel.send(message);
```

**Pros:**
- Very fast
- Bidirectional
- OS-optimized

**Cons:**
- Platform-specific implementation
- Requires setup

### Message Format

All messages follow this JSON structure:

```json
{
  "type": 0,  // MessageType enum
  "source_process": "go_integration",
  "target_process": "rust_main",
  "command": "move_mouse",
  "data": "{\"x\": 100, \"y\": 200}",
  "timestamp": 1704067200,
  "message_id": "msg-001"
}
```

### Message Validation

The system validates all messages:

```cpp
std::string error;
if (!MessageValidator::validate_message(msg, error)) {
    // Message rejected
}
```

**Validation Checks:**
- Source and target are not empty
- Command is specified
- Data is valid JSON
- Payload size is within limits
- Rate limiting per source

## Adding New Processes

### Step 1: Create Process Configuration

```cpp
ProcessConfig my_service_config;
my_service_config.type = ProcessType::CUSTOM;
my_service_config.name = "my_service";
my_service_config.executable_path = "./my-service.exe";
my_service_config.args = {"--port", "3000"};
my_service_config.comm_methods = {CommMethod::FILE_IPC};
my_service_config.auto_restart = true;
my_service_config.max_restart_attempts = 3;
my_service_config.restart_delay = std::chrono::seconds(5);
my_service_config.enable_heartbeat = true;
my_service_config.heartbeat_interval = std::chrono::seconds(10);
my_service_config.depends_on = {"rust_main"};  // Optional
```

### Step 2: Register with Process Manager

```cpp
manager.register_process(my_service_config);
```

### Step 3: Implement Message Handlers

```cpp
manager.register_message_handler("my_command", 
    [](const Message& msg) {
        // Handle message
        std::cout << "Received: " << msg.command << std::endl;
    }
);
```

### Step 4: Build and Test

```powershell
# Build your service
cd my-service
cargo build --release  # or your build system

# Test with process handler
.\scripts\build-all.ps1
cd apps/neuro-desktop/target/release
.\process-handler.exe
```

## Testing Strategy

### Unit Tests (C++)

Using Google Test framework:

```cpp
TEST_F(ProcessManagerTest, RegisterProcess) {
    ProcessConfig config;
    config.name = "test_process";
    config.executable_path = "./test.exe";
    
    EXPECT_TRUE(manager->register_process(config));
}
```

Run tests:
```bash
cd native/process-handler/build
ctest --output-on-failure
```

### Integration Tests (JavaScript)

```javascript
describe('IPC Communication', () => {
    it('should send and receive messages', (done) => {
        const command = {
            type: 'move_mouse_to',
            params: { x: 100, y: 200 }
        };
        
        fs.writeFileSync('test_ipc.json', JSON.stringify(command));
        // Wait for response...
    });
});
```

Run tests:
```bash
cd tests
npm test
```

### Manual Testing

1. **Start Process Handler**:
   ```powershell
   cd apps/neuro-desktop/target/release
   .\process-handler.exe
   ```

2. **Verify Processes Started**:
   - Check console output for process PIDs
   - All processes should show "RUNNING" state

3. **Test Communication**:
   ```powershell
   # Send test command
   echo '{"type":"move_mouse_to","params":{"x":500,"y":500}}' > neuro-integration-code-ipc.json
   
   # Check response
   type neuro-integration-code-ipc.json.response
   ```

4. **Test Crash Recovery**:
   - Kill a child process manually
   - Verify it restarts automatically
   - Check restart counter

## Deployment

### Development Build

```powershell
# Full build with tests
.\scripts\build-all.ps1

# Skip tests for faster builds
.\scripts\build-all.ps1 -SkipTests

# Clean build
.\scripts\build-all.ps1 -Clean
```

### Production Build

```powershell
.\scripts\bundle\prod.ps1
```

This creates `dist/neuro-desktop/` with:
```
neuro-desktop/
├── process-handler.exe     ← Main entry point
├── neuro-desktop.exe       ← Rust process
├── neuro-integration.exe   ← Go process
├── python/                 ← Python runtime
├── frontend/               ← Web UI
├── config/                 ← Configuration
└── README.txt              ← User instructions
```

### Distribution

1. **Zip the package**:
   ```powershell
   Compress-Archive -Path "dist/neuro-desktop" -DestinationPath "neuro-desktop-v1.0.zip"
   ```

2. **Users extract and run**:
   ```
   - Extract ZIP
   - Run process-handler.exe
   - Everything starts automatically
   ```

## Troubleshooting

### Process Won't Start

**Symptoms**: Process shows "CREATED" but never "RUNNING"

**Check:**
1. Executable exists and is not corrupted
2. Dependencies are met (check `depends_on`)
3. Environment variables are set correctly
4. Process has permission to execute

**Debug:**
```cpp
// Enable verbose logging
log::set_max_level(log::LevelFilter::Debug);
```

### Communication Timeout

**Symptoms**: "Timeout waiting for response" errors

**Check:**
1. Target process is running
2. IPC file path is correct
3. Target process is reading IPC file
4. No file permission issues

**Debug:**
```powershell
# Monitor IPC files
Get-ChildItem . -Filter "*ipc*.json" | Get-Content -Wait
```

### Process Keeps Crashing

**Symptoms**: Process restarts repeatedly, hits max attempts

**Check:**
1. Process logs for error messages
2. Python dependencies installed
3. Configuration files present
4. Port conflicts (if using TCP)

**Debug:**
```cpp
// Check crash reason
ProcessInfo info = manager.get_process_info("process_name");
std::cout << "Last error: " << info.last_error << std::endl;
```

### Memory Leaks

**Symptoms**: Process Handler memory usage grows over time

**Check:**
1. IPC files are being cleaned up
2. Message handlers don't retain references
3. Processes are properly cleaned up on exit

**Debug:**
```cpp
// Use memory profiler
#ifdef DEBUG
    // Add memory tracking
#endif
```

### High CPU Usage

**Symptoms**: Process Handler uses excessive CPU

**Check:**
1. Polling interval isn't too aggressive (default: 50ms)
2. No tight loops in message handlers
3. Processes aren't spamming messages

**Debug:**
```cpp
// Add timing metrics
auto start = std::chrono::steady_clock::now();
// ... operation ...
auto duration = std::chrono::steady_clock::now() - start;
std::cout << "Operation took: " << duration.count() << "ms" << std::endl;
```

## Performance Optimization

### Reduce Latency

1. **Use STDIO or Named Pipes** instead of File IPC
2. **Disable heartbeats** for low-latency processes
3. **Batch messages** when possible
4. **Reduce polling interval** (but watch CPU)

### Reduce Memory

1. **Limit message queue sizes**
2. **Clean up old messages** periodically
3. **Use message pooling** for frequently sent messages
4. **Disable verbose logging** in production

### Improve Reliability

1. **Increase heartbeat timeout** for slow processes
2. **Increase max restart attempts** for flaky processes
3. **Add retry logic** in message handlers
4. **Implement circuit breakers** for external services

## Security Considerations

### Message Validation

- Always validate message size
- Check for malicious JSON
- Implement rate limiting
- Sanitize file paths

### Process Isolation

- Run processes with minimal permissions
- Use separate users for different processes (on Linux)
- Limit file system access
- Validate all executable paths

### IPC Security

- Use secure file permissions
- Encrypt sensitive data in messages
- Authenticate message sources
- Validate message signatures

## Advanced Usage

### Custom Communication Channels

Implement `ICommChannel` interface:

```cpp
class MyCustomChannel : public ICommChannel {
public:
    bool initialize() override {
        // Setup your channel
    }
    
    bool send(const Message& msg) override {
        // Send logic
    }
    
    bool receive(Message& msg, int timeout_ms) override {
        // Receive logic
    }
    
    CommMethod get_method() const override {
        return CommMethod::CUSTOM;
    }
};
```

### Dynamic Process Management

Add/remove processes at runtime:

```cpp
// Add new process
ProcessConfig config = create_dynamic_config();
manager.register_process(config);
manager.start_process(config.name);

// Remove process
manager.stop_process("dynamic_process");
manager.unregister_process("dynamic_process");
```

### Metrics and Monitoring

```cpp
// Get statistics
auto stats = ipc_handler.get_statistics();
std::cout << "Total commands: " << stats.total_commands << std::endl;
std::cout << "Success rate: " 
          << (stats.successful_commands * 100.0 / stats.total_commands) 
          << "%" << std::endl;
```

## Future Enhancements

1. **Web Dashboard**: Real-time process monitoring UI
2. **Log Aggregation**: Centralized logging from all processes
3. **Distributed Mode**: Run processes on different machines
4. **Load Balancing**: Multiple instances of same process
5. **Hot Reload**: Update processes without full restart
6. **Configuration UI**: Visual process configuration editor

## Support

For issues or questions:
- GitHub: https://github.com/Nakashireyumi/neuro-desktop
- Documentation: See `/docs` folder
- Tests: See `/tests` folder

---

*Last Updated: 2025-01-02*
*Version: 1.0.0*
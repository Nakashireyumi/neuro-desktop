# Troubleshooting Guide

> **Solutions to common problems when using Neuro Desktop**

## Table of Contents

- [Installation Issues](#installation-issues)
- [Runtime Errors](#runtime-errors)
- [Performance Problems](#performance-problems)
- [Connection Issues](#connection-issues)
- [Platform-Specific Issues](#platform-specific-issues)
- [Advanced Debugging](#advanced-debugging)

## Installation Issues

### Python Dependencies Failed to Install

**Symptoms**:
```
ERROR: Could not find a version that satisfies the requirement pyautogui
```

**Solutions**:

1. **Check Python version**:
   ```bash
   python --version  # Should be 3.10 or higher
   ```

2. **Upgrade pip**:
   ```bash
   python -m pip install --upgrade pip
   ```

3. **Install dependencies individually**:
   ```bash
   pip install pyautogui
   pip install psutil
   pip install Pillow
   pip install mss
   pip install pynput
   pip install pywin32  # Windows only
   ```

4. **Use virtual environment**:
   ```bash
   python -m venv .venv
   .venv\Scripts\activate  # Windows
   source .venv/bin/activate  # Linux/macOS
   pip install -r requirements.txt
   ```

---

### Rust Build Fails

**Symptoms**:
```
error: could not compile `neuro-desktop`
```

**Solutions**:

1. **Update Rust**:
   ```bash
   rustup update
   ```

2. **Check Rust version**:
   ```bash
   rustc --version  # Should be 1.70+
   ```

3. **Clean and rebuild**:
   ```bash
   cargo clean
   cargo build --release
   ```

4. **Check for missing dependencies** (Linux):
   ```bash
   sudo apt-get install build-essential pkg-config libssl-dev
   ```

---

### Go Build Fails

**Symptoms**:
```
go: module github.com/gorilla/websocket: not found
```

**Solutions**:

1. **Initialize Go modules**:
   ```bash
   cd apps/neuro-integration
   go mod init
   go mod tidy
   ```

2. **Download dependencies**:
   ```bash
   go get github.com/gorilla/websocket
   ```

3. **Verify Go installation**:
   ```bash
   go version  # Should be 1.22+
   go env GOPATH
   ```

---

### Frontend Build Fails

**Symptoms**:
```
npm ERR! Missing script: "build"
```

**Solutions**:

1. **Install dependencies**:
   ```bash
   cd frontend
   npm install
   ```

2. **Clear npm cache**:
   ```bash
   npm cache clean --force
   rm -rf node_modules package-lock.json
   npm install
   ```

3. **Use correct Node version**:
   ```bash
   nvm install --lts
   nvm use --lts
   ```

---

## Runtime Errors

### "Go integration binary not found"

**Symptoms**:
```
Error: Neuro integration binary not found at: ./neuro-integration.exe
```

**Solutions**:

1. **Check if binary exists**:
   ```bash
   ls apps/neuro-desktop/target/release/neuro-integration.exe
   ```

2. **Rebuild Go integration**:
   ```bash
   cd apps/neuro-integration
   go build -o neuro-integration.exe .
   cp neuro-integration.exe ../neuro-desktop/target/release/
   ```

3. **Run bundle script**:
   ```bash
   .\scripts\bundle\dev.ps1
   ```

---

### "Failed to initialize Python controller"

**Symptoms**:
```
Error: Failed to initialize controller drivers
PyErr: No module named 'pyautogui'
```

**Solutions**:

1. **Check Python path** (in Rust code):
   ```rust
   // Should point to: .../target/release/python
   let python_path = get_python_packages_path();
   println!("Python path: {:?}", python_path);
   ```

2. **Verify Python files copied**:
   ```bash
   ls apps/neuro-desktop/target/release/python/
   # Should contain: Lib/ and controller/
   ```

3. **Rebuild bundle**:
   ```bash
   .\scripts\bundle\dev.ps1
   ```

4. **Test Python directly**:
   ```bash
   cd backend/python
   .venv\Scripts\activate
   python -c "from controller.lib import initialize_driver; initialize_driver()"
   ```

---

### "WebSocket connection refused"

**Symptoms**:
```
Error: dial tcp 127.0.0.1:8000: connect: connection refused
```

**Solutions**:

1. **Start Randy (test server)**:
   ```bash
   cd Randy
   npm install
   npm start
   ```

2. **Check if port is in use**:
   ```bash
   # Windows
   netstat -ano | findstr :8000
   
   # Linux/macOS
   lsof -i :8000
   ```

3. **Verify WebSocket URL**:
   ```bash
   # Check environment variable
   echo $env:NEURO_SDK_WS_URL  # Windows
   echo $NEURO_SDK_WS_URL      # Linux/macOS
   
   # Should be: ws://localhost:8000
   ```

4. **Test WebSocket**:
   ```bash
   curl -i -N \
     -H "Connection: Upgrade" \
     -H "Upgrade: websocket" \
     http://localhost:8000
   ```

---

### "IPC timeout"

**Symptoms**:
```
Error: timeout waiting for Rust response
```

**Solutions**:

1. **Check IPC file**:
   ```bash
   # Should exist when running
   ls neuro-integration-code-ipc.json
   ```

2. **Verify Rust process running**:
   ```bash
   # Windows
   tasklist | findstr neuro-desktop
   
   # Linux/macOS
   ps aux | grep neuro-desktop
   ```

3. **Check file permissions**:
   ```bash
   # Linux/macOS
   ls -la neuro-integration-code-ipc.json
   # Should be readable/writable
   ```

4. **Increase timeout** (if needed):
   ```go
   // In utils.go
   for i := 0; i < 1000; i++ {  // Increase from 500
       // ...
   }
   ```

---

### "Python GIL deadlock"

**Symptoms**:
- Application freezes
- No error messages
- CPU usage at 0%

**Solutions**:

1. **Check for blocking calls**:
   ```python
   # Bad: Blocks forever
   while True:
       time.sleep(1)
   
   # Good: Allows interruption
   for _ in range(10):
       time.sleep(1)
   ```

2. **Release GIL where possible**:
   ```rust
   Python::with_gil(|py| {
       let result = py.allow_threads(|| {
           // CPU-intensive work here
       });
   })
   ```

3. **Restart application**:
   - Press Ctrl+C
   - Wait 5 seconds
   - Restart

---

## Performance Problems

### Slow Mouse Movement

**Symptoms**:
- Mouse moves in jerky steps
- Takes too long to reach target

**Solutions**:

1. **Adjust duration**:
   ```text
   # Faster
   MOVE 500 300 0.05
   
   # Slower (smoother)
   MOVE 500 300 0.3
   ```

2. **Reduce noise**:
   ```python
   # In mouse_pathfinder.py
   PathProfile(noise_scale=0.3)  # Lower = smoother
   ```

3. **Increase smoothing**:
   ```python
   PathProfile(smoothing_factor=0.8)  # Higher = smoother
   ```

---

### High CPU Usage

**Symptoms**:
- CPU at 50-100%
- Fan running loud
- System lag

**Solutions**:

1. **Reduce IPC polling rate**:
   ```rust
   // In ipc_handler.rs
   thread::sleep(Duration::from_millis(100));  // Increase from 50
   ```

2. **Disable verbose logging**:
   ```rust
   log::set_max_level(log::LevelFilter::Error);
   ```

3. **Limit action rate**:
   ```python
   # Add delays between actions
   time.sleep(0.1)
   ```

---

### Memory Leak

**Symptoms**:
- Memory usage grows over time
- Eventually crashes with OOM

**Solutions**:

1. **Clear action queues**:
   ```python
   mouse.clear()
   keyboard.clear()
   ```

2. **Limit history size**:
   ```python
   DesktopMonitor(
       max_mouse_history=100,      # Reduce from 500
       max_action_history=200,     # Reduce from 1000
   )
   ```

3. **Restart periodically**:
   ```bash
   # Cron job to restart daily
   0 4 * * * systemctl restart neuro-desktop
   ```

---

## Connection Issues

### Cannot Connect to Remote Neuro API

**Symptoms**:
```
Error: dial tcp: i/o timeout
```

**Solutions**:

1. **Check firewall**:
   ```bash
   # Windows
   New-NetFirewallRule -DisplayName "Neuro" -Direction Outbound -Action Allow
   
   # Linux
   sudo ufw allow out 8000/tcp
   ```

2. **Verify network connectivity**:
   ```bash
   ping 192.168.1.100
   telnet 192.168.1.100 8000
   ```

3. **Check NAT/port forwarding**:
   - Ensure router forwards port 8000 to correct internal IP
   - Check for ISP blocking

4. **Use VPN/tunnel**:
   ```bash
   # SSH tunnel
   ssh -L 8000:localhost:8000 user@remote-server
   ```

---

### SSL/TLS Errors

**Symptoms**:
```
Error: x509: certificate signed by unknown authority
```

**Solutions**:

1. **Use `ws://` not `wss://`**:
   ```yaml
   # config/integration-config.yml
   neuro-backend: "ws://localhost:8000"  # Not wss://
   ```

2. **Accept self-signed certs** (if using wss://):
   ```go
   // In WebSocket client
   dialer := websocket.Dialer{
       TLSClientConfig: &tls.Config{
           InsecureSkipVerify: true,
       },
   }
   ```

3. **Install root CA**:
   ```bash
   # Linux
   sudo cp ca-cert.crt /usr/local/share/ca-certificates/
   sudo update-ca-certificates
   ```

---

## Platform-Specific Issues

### Windows: "Access Denied" Errors

**Symptoms**:
```
Error: Access is denied. (os error 5)
```

**Solutions**:

1. **Run as Administrator**:
   - Right-click `neuro-desktop.exe`
   - Select "Run as administrator"

2. **Check antivirus**:
   - Add exception for Neuro Desktop folder
   - Temporarily disable real-time protection

3. **Disable UAC** (not recommended):
   - Control Panel → User Accounts → Change UAC settings

---

### Linux: "Permission Denied" for Input Devices

**Symptoms**:
```
PermissionError: [Errno 13] Permission denied: '/dev/uinput'
```

**Solutions**:

1. **Add user to input group**:
   ```bash
   sudo usermod -a -G input $USER
   # Log out and back in
   ```

2. **Set uinput permissions**:
   ```bash
   sudo chmod 666 /dev/uinput
   ```

3. **Load uinput module**:
   ```bash
   sudo modprobe uinput
   echo 'uinput' | sudo tee -a /etc/modules
   ```

---

### macOS: "Accessibility Permissions Required"

**Symptoms**:
```
Error: This process does not have permission to use Accessibility services
```

**Solutions**:

1. **Grant accessibility permissions**:
   - System Preferences → Security & Privacy → Accessibility
   - Click lock to make changes
   - Add `neuro-desktop` to allowed apps

2. **From terminal**:
   ```bash
   sudo sqlite3 /Library/Application\ Support/com.apple.TCC/TCC.db \
     "INSERT INTO access VALUES('kTCCServiceAccessibility','com.apple.Terminal',0,1,1,NULL,NULL);"
   ```

3. **Reset PPCP** (if still fails):
   ```bash
   tccutil reset Accessibility
   ```

---

### macOS: "Unidentified Developer" Warning

**Symptoms**:
```
"neuro-desktop" can't be opened because it is from an unidentified developer
```

**Solutions**:

1. **Allow app**:
   - System Preferences → Security & Privacy → General
   - Click "Open Anyway"

2. **Remove quarantine attribute**:
   ```bash
   xattr -d com.apple.quarantine neuro-desktop
   ```

3. **Disable Gatekeeper** (not recommended):
   ```bash
   sudo spctl --master-disable
   ```

---

## Advanced Debugging

### Enable Debug Logging

**Rust**:
```rust
// In main.rs
env_logger::Builder::from_default_env()
    .filter_level(log::LevelFilter::Debug)
    .init();
```

**Go**:
```go
// In main.go
log.SetFlags(log.LstdFlags | log.Lshortfile)
log.SetOutput(os.Stdout)
```

**Python**:
```python
import logging
logging.basicConfig(level=logging.DEBUG)
```

---

### Capture Logs

**Windows**:
```powershell
.\neuro-desktop.exe > debug.log 2>&1
type debug.log
```

**Linux/macOS**:
```bash
./neuro-desktop > debug.log 2>&1
tail -f debug.log
```

---

### Inspect IPC Files

**Real-time monitoring**:
```bash
# Windows
Get-Content -Wait neuro-integration-code-ipc.json

# Linux/macOS
tail -f neuro-integration-code-ipc.json
```

**Manual inspection**:
```bash
cat neuro-integration-code-ipc.json
cat neuro-integration-code-ipc.json.response
```

---

### Attach Debugger

**Rust** (GDB/LLDB):
```bash
# Build with debug symbols
cargo build

# Run in debugger
gdb target/debug/neuro-desktop
(gdb) run
(gdb) bt  # Backtrace on crash
```

**Python**:
```python
import pdb; pdb.set_trace()  # Breakpoint
```

---

### Profile Performance

**Rust** (Flamegraph):
```bash
cargo install flamegraph
sudo flamegraph -- target/release/neuro-desktop
```

**Python** (cProfile):
```python
import cProfile
cProfile.run('controller.execute()')
```

---

### Memory Profiling

**Rust** (Valgrind):
```bash
valgrind --leak-check=full target/debug/neuro-desktop
```

**Python** (memory_profiler):
```python
from memory_profiler import profile

@profile
def expensive_function():
    pass
```

---

### Network Debugging

**Wireshark**:
1. Start capture on loopback interface
2. Filter: `tcp.port == 8000`
3. Analyze WebSocket frames

**tcpdump**:
```bash
sudo tcpdump -i lo port 8000 -A
```

---

## Getting Help

If none of these solutions work:

1. **Search existing issues**: [GitHub Issues](https://github.com/Nakashireyumi/neuro-desktop/issues)
2. **Ask on Discord**: Link in README
3. **Create new issue**: Include:
   - OS and version
   - Neuro Desktop version
   - Full error message
   - Steps to reproduce
   - Relevant logs
   - What you've already tried

---

## Common Error Messages Reference

| Error | Meaning | Solution |
|-------|---------|----------|
| `ModuleNotFoundError: No module named 'pyautogui'` | Python dependencies missing | Run `pip install -r requirements.txt` |
| `dial tcp: connection refused` | Neuro API not running | Start Randy or check connection |
| `Permission denied` | Insufficient permissions | Run as admin or fix permissions |
| `timeout waiting for Rust response` | IPC communication broken | Restart both processes |
| `Failed to initialize controller drivers` | Python setup failed | Check Python installation |
| `Neuro integration binary not found` | Missing Go binary | Rebuild and copy binary |
| `coordinate out of bounds` | Invalid mouse coordinates | Check screen resolution |

---

**Still stuck?** Don't hesitate to ask for help! The community is here to support you.

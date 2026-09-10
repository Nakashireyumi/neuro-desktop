# Deployment Guide

> **Complete guide for deploying Neuro Desktop in various environments**

## Table of Contents

- [Development Setup](#development-setup)
- [Building from Source](#building-from-source)
- [Production Deployment](#production-deployment)
- [Configuration](#configuration)
- [Troubleshooting](#troubleshooting)

## Development Setup

### Prerequisites

Install all required tools:

#### Windows

```powershell
# Install Rust
Invoke-WebRequest -Uri https://sh.rustup.rs -OutFile rustup-init.exe
.\rustup-init.exe

# Install Go
winget install GoLang.Go

# Install Python 3.10+
winget install Python.Python.3.10

# Install Node.js
winget install OpenJS.NodeJS

# Verify installations
rustc --version
go version
python --version
node --version
```

#### Linux/macOS

```bash
# Install Rust
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh

# Install Go (Linux)
sudo apt update
sudo apt install golang-go

# Install Go (macOS)
brew install go

# Install Python
sudo apt install python3.10 python3.10-venv  # Linux
brew install python@3.10                      # macOS

# Install Node.js
curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.3/install.sh | bash
nvm install --lts

# Verify installations
rustc --version
go version
python3 --version
node --version
```

### Clone Repository

```bash
git clone https://github.com/Nakashireyumi/neuro-desktop.git
cd neuro-desktop/desktop
```

### Setup Python Environment

```bash
cd backend/python

# Create virtual environment
python -m venv .venv

# Activate (Windows)
.venv\Scripts\activate

# Activate (Linux/macOS)
source .venv/bin/activate

# Install dependencies
pip install -r requirements.txt
```

### Setup Frontend

```bash
cd frontend

# Install dependencies
npm install

# Optional: Test build
npm run build
```

### Run Development Build

```powershell
# Windows
.\scripts\bundle\dev.ps1

# Linux/macOS
./scripts/bundle/dev.sh
```

This will:
1. Build all components
2. Copy files to `apps/neuro-desktop/target/release`
3. Launch the application

## Building from Source

### Full Build

```powershell
# Windows - Complete build with tests
.\scripts\build-all.ps1

# Linux/macOS
./scripts/build-all.sh
```

### Skip Tests (Faster)

```powershell
.\scripts\build-all.ps1 -SkipTests
```

### Build Specific Components

```bash
# Build Rust only
cd apps/neuro-desktop
cargo build --release

# Build Go only
cd apps/neuro-integration
go build -o neuro-integration.exe .

# Build Frontend only
cd frontend
npm run build

# Setup Python only
cd backend/python
python -m venv .venv
.venv\Scripts\activate
pip install -r requirements.txt
```

### Cross-Platform Go Builds

```powershell
.\scripts\build-go.ps1
```

Creates binaries for:
- Windows (amd64)
- Linux (amd64)
- macOS (amd64, arm64)

## Production Deployment

### Create Production Bundle

```powershell
# Windows
.\scripts\bundle\prod.ps1

# Linux/macOS
./scripts/bundle/prod.sh
```

This creates a complete, standalone package in `dist/neuro-desktop/`:

```
neuro-desktop/
├── neuro-desktop.exe         # Main application
├── neuro-integration.exe     # Neuro API client
├── python/                   # Embedded Python runtime
│   ├── Lib/                  # Standard library + dependencies
│   └── controller/           # Control drivers
├── frontend/                 # Web UI assets
├── config/                   # Configuration files
├── integration-docs/         # Documentation
├── README.txt               # User instructions
└── start.bat                # Quick launcher (Windows)
```

### Distribution

#### Option 1: Zip Archive

```powershell
# Create distributable archive
Compress-Archive -Path "dist/neuro-desktop" -DestinationPath "neuro-desktop-v0.0.3b.zip"
```

Users extract and run:
```bash
# Windows
cd neuro-desktop
.\neuro-desktop.exe

# Linux/macOS
cd neuro-desktop
chmod +x neuro-desktop neuro-integration
./neuro-desktop
```

#### Option 2: Installer (Future)

- NSIS installer (Windows)
- .deb package (Debian/Ubuntu)
- .rpm package (Fedora/RHEL)
- .dmg installer (macOS)

### System Requirements

**Minimum**:
- OS: Windows 10+, Ubuntu 20.04+, macOS 11+
- CPU: 2 cores, 2.0 GHz
- RAM: 4 GB
- Disk: 500 MB

**Recommended**:
- OS: Windows 11, Ubuntu 22.04+, macOS 12+
- CPU: 4 cores, 3.0 GHz
- RAM: 8 GB
- Disk: 1 GB

## Configuration

### Configuration Files

**Location**: `config/integration-config.yml`

```yaml
connection:
  # Neuro API WebSocket URL
  neuro-backend: "ws://localhost:8000"
  
  # Plugin server (future feature)
  neuro-desktop-plugins-server: "ws://localhost:2328"
  
  # Web UIs (future features)
  neuro-desktop-admin-dashboard: "http://localhost:8300"
  neuro-desktop-internal-thinking: "http://localhost:8400"

package:
  name: "neuro-desktop"
  version: "0.0.3b-dev"
  build: "release/28-12-2025"
  package-id: "builtby.nakashireyumi/neuro-desktop"
  description: "Desktop control integration for Neuro-sama"
```

### Environment Variables

Override config values with environment variables:

```powershell
# Windows
$env:NEURO_SDK_WS_URL = "ws://192.168.1.100:8000"
$env:NEURO_IPC_FILE = "C:\neuro\ipc.json"

# Linux/macOS
export NEURO_SDK_WS_URL="ws://192.168.1.100:8000"
export NEURO_IPC_FILE="/var/neuro/ipc.json"
```

**Available Variables**:
- `NEURO_SDK_WS_URL` - WebSocket URL for Neuro API
- `NEURO_IPC_FILE` - Path to IPC communication file

### Network Configuration

#### Firewall Rules

If connecting to remote Neuro API:

```powershell
# Windows Firewall
New-NetFirewallRule -DisplayName "Neuro Desktop" -Direction Outbound -Program "C:\neuro-desktop\neuro-desktop.exe" -Action Allow

# Linux (ufw)
sudo ufw allow out 8000/tcp

# macOS
# Add to Security & Privacy → Firewall → Firewall Options
```

#### Port Forwarding

To allow external Neuro API connections:

```bash
# Router configuration (example)
External Port: 8000
Internal IP: 192.168.1.100
Internal Port: 8000
Protocol: TCP
```

## Troubleshooting

### Common Issues

#### "Python module not found"

**Symptom**: `ModuleNotFoundError: No module named 'pyautogui'`

**Fix**:
```bash
cd backend/python
.venv\Scripts\activate  # Windows
pip install -r requirements.txt
```

Or rebuild bundle:
```bash
.\scripts\bundle\prod.ps1
```

---

#### "Go binary not found"

**Symptom**: `Neuro integration binary not found at: ...`

**Fix**:
```bash
cd apps/neuro-integration
go build -o neuro-integration.exe .
cp neuro-integration.exe ../neuro-desktop/target/release/
```

---

#### "WebSocket connection failed"

**Symptom**: `Failed to connect to Neuro: dial tcp: connection refused`

**Fix**:
1. Check Neuro API is running:
   ```bash
   curl -I http://localhost:8000
   ```

2. Start Randy for testing:
   ```bash
   cd Randy
   npm install
   npm start
   ```

3. Check firewall settings

---

#### "Permission denied"

**Symptom**: `Permission denied (Linux/macOS)`

**Fix**:
```bash
chmod +x neuro-desktop
chmod +x neuro-integration
```

---

#### "Python version mismatch"

**Symptom**: `ImportError: cannot import name '_imaging' from 'PIL'`

**Fix**:
```bash
# Ensure Python 3.10+
python --version

# Recreate virtual environment
rm -rf backend/python/.venv
python -m venv backend/python/.venv
# ... reinstall dependencies
```

---

#### "IPC timeout"

**Symptom**: `timeout waiting for Rust response`

**Fix**:
1. Check Rust process is running
2. Check IPC file permissions
3. Increase timeout (if needed)
4. Restart both processes

---

### Debug Mode

Enable verbose logging:

```rust
// In main.rs
log::set_max_level(log::LevelFilter::Debug);
```

Or set environment variable:
```bash
export RUST_LOG=debug
```

### Log Files

Logs are output to console. Redirect to file:

```bash
# Windows
.\neuro-desktop.exe > neuro.log 2>&1

# Linux/macOS
./neuro-desktop > neuro.log 2>&1
```

### Testing Installation

1. **Run Randy**:
   ```bash
   cd Randy
   npm start
   ```

2. **Run Neuro Desktop**:
   ```bash
   .\neuro-desktop.exe
   ```

3. **Verify**:
   - Should see "Connected to Neuro!" in logs
   - Randy should show registered actions
   - Randy will send random test actions

### Health Checks

```bash
# Check if processes are running
ps aux | grep neuro-desktop      # Linux/macOS
tasklist | findstr neuro         # Windows

# Check IPC file exists
ls neuro_ipc.json                # Should exist when communicating

# Check network connection
netstat -an | grep 8000          # Should show connection to :8000
```

## Security Considerations

### Running as Service

**Windows (NSSM)**:
```powershell
# Install NSSM
winget install NSSM

# Install service
nssm install NeuroDesktop "C:\neuro-desktop\neuro-desktop.exe"
nssm start NeuroDesktop
```

**Linux (systemd)**:
```bash
# Create service file
sudo nano /etc/systemd/system/neuro-desktop.service
```

```ini
[Unit]
Description=Neuro Desktop Control System
After=network.target

[Service]
Type=simple
User=neuro
WorkingDirectory=/opt/neuro-desktop
ExecStart=/opt/neuro-desktop/neuro-desktop
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
# Enable and start
sudo systemctl enable neuro-desktop
sudo systemctl start neuro-desktop
```

### Sandboxing (Advanced)

Run in restricted environment:

```bash
# Linux (firejail)
firejail --net=none --private=~/.neuro neuro-desktop

# Windows (AppContainer)
# Use Windows Sandbox or VM
```

### Network Isolation

Only allow local connections:

```yaml
# config/integration-config.yml
connection:
  neuro-backend: "ws://127.0.0.1:8000"  # Only localhost
```

## Performance Tuning

### Optimize Python

```bash
# Use PyPy for faster execution (experimental)
pypy3 -m venv .venv
```

### Reduce Logging

```rust
log::set_max_level(log::LevelFilter::Error);
```

### Increase IPC Poll Rate

```rust
// In ipc_handler.rs
thread::sleep(Duration::from_millis(10));  // Faster (was 50ms)
```

**Warning**: Higher CPU usage

### Pre-compile Python

```bash
python -m compileall backend/python/controller
```

## Monitoring

### Metrics Collection

```python
# Add to Python controller
import time
import psutil

def get_metrics():
    return {
        "cpu_percent": psutil.cpu_percent(),
        "memory_mb": psutil.Process().memory_info().rss / 1024 / 1024,
        "actions_executed": controller.action_count,
    }
```

### External Monitoring

```bash
# Prometheus exporter (future)
curl http://localhost:9090/metrics
```

## Backup and Recovery

### Backup Configuration

```bash
# Backup config
cp -r config config.backup

# Backup user data (if any)
cp -r ~/.neuro-desktop ~/.neuro-desktop.backup
```

### Disaster Recovery

```bash
# Restore from backup
cp -r config.backup config

# Rebuild from source
git pull origin main
.\scripts\build-all.ps1
.\scripts\bundle\prod.ps1
```

## Updates

### Update Process

```bash
# 1. Backup current installation
cp -r neuro-desktop neuro-desktop.backup

# 2. Download new version
# ... extract to temp directory ...

# 3. Stop running instance
# Ctrl+C or kill process

# 4. Replace binaries
cp neuro-desktop-new/* neuro-desktop/

# 5. Restart
cd neuro-desktop
.\neuro-desktop.exe
```

### Rolling Back

```bash
# Restore previous version
rm -rf neuro-desktop
mv neuro-desktop.backup neuro-desktop
cd neuro-desktop
.\neuro-desktop.exe
```

## Advanced Deployment

### Docker Container (Experimental)

```dockerfile
FROM ubuntu:22.04

# Install dependencies
RUN apt-get update && apt-get install -y \
    python3.10 python3-pip \
    x11-apps xvfb

# Copy application
COPY dist/neuro-desktop /opt/neuro-desktop

# Run with virtual display
CMD ["xvfb-run", "/opt/neuro-desktop/neuro-desktop"]
```

### Kubernetes Deployment (Future)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: neuro-desktop
spec:
  replicas: 1
  selector:
    matchLabels:
      app: neuro-desktop
  template:
    metadata:
      labels:
        app: neuro-desktop
    spec:
      containers:
      - name: neuro-desktop
        image: neuro-desktop:latest
        env:
        - name: NEURO_SDK_WS_URL
          value: "ws://neuro-api:8000"
```

## Conclusion

Neuro Desktop can be deployed in various environments from simple desktop installations to complex containerized deployments. Choose the method that best fits your use case.

For production use, always:
- ✅ Test thoroughly before deploying
- ✅ Keep backups of configurations
- ✅ Monitor system health
- ✅ Keep software updated
- ✅ Follow security best practices

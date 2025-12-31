Write-Host "=== Dev environment setup ==="

# ---------- Python ----------
if (-Not (Test-Path "backend/python/.venv")) {
    Write-Host "Creating Python venv..."
    python -m venv backend/python/.venv
}

Write-Host "Installing Python dependencies..."
& backend/python/.venv/Scripts/python.exe -m pip install --upgrade pip
& backend/python/.venv/Scripts/python.exe -m pip install -r backend/python/requirements.txt

# ---------- Node / Frontend ----------
if (-Not (Test-Path "frontend/node_modules")) {
    Write-Host "Installing frontend dependencies..."
    cd frontend
    npm install
    cd ..
}

# ---------- Rust ----------
Write-Host "Checking Rust..."
cargo --version | Out-Null

# ---------- Go (optional) ----------
if (Test-Path "native/go") {
    Write-Host "Setting up Go..."
    cd native/go
    go mod tidy
    cd ../..
}

Write-Host "=== Dev setup complete ==="

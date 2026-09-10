# ============================================================
# desktop/scripts/bundle/prod.ps1
# ============================================================

$ErrorActionPreference = "Stop"

Write-Host "=== Building Neuro Desktop Bundle ==="

$DIST = "dist/neuro-desktop"
$PY_DIST = "$DIST/python"

# ---------- Clean ----------
Remove-Item -Recurse -Force dist -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $DIST | Out-Null

# ---------- Build Rust ----------
Write-Host "[1/4] Building Rust application..."
Push-Location apps/neuro-desktop
cargo build --release
Pop-Location

Copy-Item `
  apps/neuro-desktop/target/release/neuro-desktop.exe `
  $DIST

Write-Host "      ??? Rust binary built"

# ---------- Build Process Handler ----------
Write-Host "[1.5/4] Building Process Handler..."
Push-Location apps/process-handler
if (!(Test-Path "build")) {
    New-Item -ItemType Directory -Path "build" | Out-Null
}
Push-Location build
cmake .. -DBUILD_TESTS=OFF
cmake --build . --config Release
Pop-Location
Pop-Location
Write-Host "      [ok] Process Handler built"

# ---------- Build Neuro Integration ----------
Write-Host "[2/4] Building Neuro integration..."

New-Item -ItemType Directory -Force -Path apps/neuro-integration/dist | Out-Null
Push-Location apps/neuro-integration

# Build for Windows
go build -o dist/neuro-integration.exe .

Pop-Location

Copy-Item `
  apps/neuro-integration/dist/neuro-integration.exe `
  $DIST/neuro-integration.exe

New-Item -ItemType Directory -Force -Path $DIST/integration-docs | Out-Null

Copy-Item `
  apps/neuro-integration/integration-docs/"Action Script Documentation.md" `
  $DIST/integration-docs/"Action Script Documentation.md"

Copy-Item `
  apps/neuro-integration/permissions.example.json `
  $DIST/permissions.json

Write-Host "      ??? Neuro Integration binary built"

# ---------- Copy Process Handler ----------
$processHandlerCandidates = @(
  "apps/process-handler/build/Release/process-handler.exe",
  "apps/process-handler/build/process-handler.exe"
)
$processHandlerSource = $processHandlerCandidates | Where-Object { Test-Path $_ } | Select-Object -First 1
if ($processHandlerSource) {
    Copy-Item $processHandlerSource "$DIST/process-handler.exe"
    Write-Host "      [ok] Process Handler binary bundled"
} else {
    throw "Process Handler binary not found after build step"
}

$relayBinarySource = $env:NEURO_RELAY_BINARY_SOURCE
if ($relayBinarySource -and (Test-Path $relayBinarySource)) {
    Copy-Item $relayBinarySource "$DIST/neuro-relay.exe"
    Write-Host "      [ok] Neuro Relay binary bundled"
} else {
    Write-Host "      [..] Neuro Relay binary not bundled (set NEURO_RELAY_BINARY_SOURCE)"
}

# ---------- Build frontend ----------
Write-Host "[3/4] Building frontend..."
Push-Location frontend
npm run build
Pop-Location

Copy-Item frontend/dist -Recurse $DIST/frontend

Write-Host "      ??? Frontend built"

# ---------- Copy Config ----------
Write-Host "Copying configuration files..."
Copy-Item config "$DIST/config" -Recurse
Write-Host "  ??? Config files copied"

Write-Host "Copying catalog files..."
Copy-Item catalog "$DIST/catalog" -Recurse
Write-Host "  [ok] Catalog files copied"

# ---------- Bundle Python (EMBEDDED) ----------
Write-Host "[4/4] Bundling Python runtime..."

New-Item -ItemType Directory -Force -Path $PY_DIST | Out-Null

Copy-Item backend/python/.venv/Lib $PY_DIST/Lib -Recurse

$sitePackagesPath = Join-Path $PY_DIST "Lib/site-packages"
if (Test-Path $sitePackagesPath) {
    # Bundle only runtime dependencies needed by controller drivers.
    # Local development environments sometimes contain optional scientific stacks
    # (numpy/scipy) that can crash embedded Python builds on Windows.
    $stripPatterns = @("numpy*", "scipy*")
    foreach ($pattern in $stripPatterns) {
        Get-ChildItem -Path $sitePackagesPath -Force -Filter $pattern -ErrorAction SilentlyContinue |
            Remove-Item -Recurse -Force -ErrorAction SilentlyContinue
    }

    # Remove cached wheel artifacts accidentally copied from development venvs.
    Get-ChildItem -Path $sitePackagesPath -Force -Filter "*.whl" -ErrorAction SilentlyContinue |
        Remove-Item -Force -ErrorAction SilentlyContinue
}

# ---------- Copy Controller Drivers ----------
Copy-Item `
  backend/python/controller `
  "$PY_DIST/controller" `
  -Recurse

Write-Host "      ??? Python runtime bundled"

# ---------- Metadata ----------
@"
Neuro Desktop Control System
=============================

This is a self-contained bundle of Neuro Desktop.

Contents:
  - neuro-desktop.exe         Main application (Rust)
  - neuro-integration.exe     Neuro API connector (Go)
  - neuro-relay.exe           Optional relay binary (if bundled)
  - process-handler.exe       Optional process supervisor
  - python/                   Python runtime and drivers
  - frontend/                 Web UI assets

To run:
  1. Double-click neuro-desktop.exe
  2. Or run from terminal: .\neuro-desktop.exe

Environment Variables (optional):
  - NEURO_SDK_WS_URL    WebSocket URL for Neuro API
                        Default: ws://localhost:8000
  
  - NEURO_IPC_FILE      Path to IPC file
                        Default: ./neuro_ipc.json

  - NEURO_PERMISSIONS_FILE  Path to permissions policy file
                            Default: ./permissions.json

  - NEURO_RELAY_ENABLED     Enable bundled Neuro Relay
                            Default: false

The Go integration binary will be started automatically
by the main Rust binary. You do not need to run it manually.

Press Ctrl+C to stop.

For more information, visit:
https://github.com/Nakashireyumi/neuro-desktop
"@ | Out-File "$DIST/README.txt"

# ---------- Create launcher script ----------
@" 
@echo off
echo Starting Neuro Desktop...
echo.
neuro-desktop.exe
pause
"@ | Out-File "$DIST/start.bat" -Encoding ASCII

@"
@echo off
echo Starting Neuro Desktop (supervised mode)...
echo.
process-handler.exe
pause
"@ | Out-File "$DIST/start-supervised.bat" -Encoding ASCII

Write-Host ""
Write-Host "=== Bundle complete ==="
Write-Host "Location: $DIST"
Write-Host ""
Write-Host "Files included:"
Get-ChildItem $DIST -Recurse -File | ForEach-Object {
    $relativePath = $_.FullName.Replace("$PWD\$DIST\", "")
    Write-Host "  - $relativePath"
}
Write-Host ""
Write-Host "To test: cd $DIST && .\neuro-desktop.exe"

# OLD CODE:
# $ErrorActionPreference = "Stop"

# Write-Host "=== Building Neuro Desktop Bundle ==="

# $DIST = "dist/neuro-desktop"
# $PY_DIST = "$DIST/python"

# # ---------- Clean ----------
# Remove-Item -Recurse -Force dist -ErrorAction SilentlyContinue
# New-Item -ItemType Directory -Force -Path $DIST | Out-Null

# # ---------- Build Rust ----------
# Write-Host "Building Rust app..."
# Push-Location apps/neuro-desktop
# cargo build --release
# Pop-Location

# Copy-Item `
#   apps/neuro-desktop/target/release/neuro-desktop.exe `
#   $DIST

# # ---------- Build frontend ----------
# Write-Host "Building frontend..."
# Push-Location frontend
# npm run build
# Pop-Location

# Copy-Item frontend/dist -Recurse $DIST/frontend

# # ---------- Build Go Integration ----------
# Write-Host "Building Go integration..."
# Push-Location native/go-neuro-integration
# go build -o go-neuro-integration.exe main.go
# Pop-Location

# Copy-Item `
#   native/go-neuro-integration/go-neuro-integration.exe `
#   $DIST

# # ---------- Bundle Python (EMBEDDED) ----------
# Write-Host "Bundling Python files and libraries..."

# New-Item -ItemType Directory -Force -Path $PY_DIST | Out-Null

# Copy-Item backend/python/.venv/Lib $PY_DIST/Lib -Recurse

# # ---------- Copy Controller Drivers ----------
# Copy-Item `
#   backend/python/controller `
#   "$PY_DIST/controller" `
#   -Recurse

# # ---------- Metadata ----------
# @"
# Neuro Desktop
# -------------
# This folder contains all runtime dependencies.
# Do not move files individually.
# "@ | Out-File "$DIST/README.txt"

# Write-Host "=== Bundle complete ==="

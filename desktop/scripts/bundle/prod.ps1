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

Write-Host "      ✓ Rust binary built"

# ---------- Build Neuro Integration ----------
Write-Host "[2/4] Building Neuro integration..."

New-Item -ItemType Directory -Force -Path native/neuro-integration/dist | Out-Null
Push-Location native/neuro-integration

# Build for Windows
go build -o dist/neuro-integration.exe main.go

Pop-Location

Copy-Item `
  native/neuro-integration/dist/neuro-integration.exe `
  $DIST/neuro-integration.exe

New-Item -ItemType Directory -Force -Path $DIST/integration-docs | Out-Null

Copy-Item `
  native/neuro-integration/integration-docs/"Action Script Documentation.md" `
  $DIST/integration-docs/"Action Script Documentation.md"

Write-Host "      ✓ Neuro Integration binary built"

# ---------- Build frontend ----------
Write-Host "[3/4] Building frontend..."
Push-Location frontend
npm run build
Pop-Location

Copy-Item frontend/dist -Recurse $DIST/frontend

Write-Host "      ✓ Frontend built"

# ---------- Copy Config ----------
Write-Host "Copying configuration files..."
Copy-Item config "$DIST/config" -Recurse
Write-Host "  ✓ Config files copied"

# ---------- Bundle Python (EMBEDDED) ----------
Write-Host "[4/4] Bundling Python runtime..."

New-Item -ItemType Directory -Force -Path $PY_DIST | Out-Null

Copy-Item backend/python/.venv/Lib $PY_DIST/Lib -Recurse

# ---------- Copy Controller Drivers ----------
Copy-Item `
  backend/python/controller `
  "$PY_DIST/controller" `
  -Recurse

Write-Host "      ✓ Python runtime bundled"

# ---------- Metadata ----------
@"
Neuro Desktop Control System
=============================

This is a self-contained bundle of Neuro Desktop.

Contents:
  - neuro-desktop.exe         Main application (Rust)
  - go-neuro-integration.exe  Neuro API connector (Go)
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

The Go integration binary will be started automatically
by the main Rust binary. You don't need to run it manually.

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
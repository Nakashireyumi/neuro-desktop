# ============================================================
# desktop/scripts/bundle/dev.ps1
# ============================================================
$ErrorActionPreference = "Stop"

Write-Host "=== Development Bundle ==="

$DIST = "apps/neuro-desktop/target/release"
$PY_DIST = "$DIST/python"

Remove-Item -Recurse -Force $DIST/python -ErrorAction SilentlyContinue
Remove-Item -Recurse -Force $DIST/frontend -ErrorAction SilentlyContinue
Remove-Item -Recurse -Force $DIST/config -ErrorAction SilentlyContinue

# ---------- Build Neuro Integration ----------
Write-Host "Building Neuro integration..."

New-Item -ItemType Directory -Force -Path native/neuro-integration/dist | Out-Null
Push-Location native/neuro-integration/

go build -o dist/neuro-integration.exe main.go
Pop-Location

Copy-Item `
  native/neuro-integration/dist/neuro-integration.exe `
  $DIST/neuro-integration.exe

New-Item -ItemType Directory -Force -Path $DIST/integration-docs | Out-Null

Copy-Item `
  native/neuro-integration/integration-docs/"Action Script Documentation.md" `
  $DIST/integration-docs/"Action Script Documentation.md"

Write-Host "  ✓ Neuro Integration binary copied to $DIST"

# ---------- Build frontend ----------
Write-Host "Building frontend..."
Push-Location frontend
npm run build
Pop-Location

Copy-Item frontend/dist -Recurse $DIST/frontend

# ---------- Copy Config ----------
Write-Host "Copying configuration files..."
Copy-Item config "$DIST/config" -Recurse
Write-Host "  ✓ Config files copied"

# ---------- Bundle Python (EMBEDDED) ----------
$PY_DIST = "$DIST/python"
Write-Host "Bundling Python files and libraries..."

New-Item -ItemType Directory -Force -Path $PY_DIST | Out-Null

Copy-Item backend/python/.venv/Lib $PY_DIST/Lib -Recurse

# ---------- Copy Controller Drivers ----------
Copy-Item backend/python/controller "$PY_DIST/controller" -Recurse

Write-Host ""
Write-Host "=== Dev bundle complete ==="
Write-Host "Run from: $DIST"
Write-Host "Execute:  .\neuro-desktop.exe"
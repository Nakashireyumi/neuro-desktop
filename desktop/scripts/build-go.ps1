# ============================================================
# desktop/scripts/bundle/build-go.ps1 (Standalone script)
# ============================================================

Write-Host "Building Go integration for multiple platforms..."

$OUTPUT_DIR = "native/go-neuro-integration/build"
Remove-Item -Recurse -Force $OUTPUT_DIR -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path $OUTPUT_DIR | Out-Null

Push-Location native/go-neuro-integration

# Windows
Write-Host "Building for Windows..."
$env:GOOS = "windows"
$env:GOARCH = "amd64"
go build -o "$OUTPUT_DIR/go-neuro-integration-windows-amd64.exe" main.go

# Linux
Write-Host "Building for Linux..."
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -o "$OUTPUT_DIR/go-neuro-integration-linux-amd64" main.go

# macOS (Intel)
Write-Host "Building for macOS (Intel)..."
$env:GOOS = "darwin"
$env:GOARCH = "amd64"
go build -o "$OUTPUT_DIR/go-neuro-integration-darwin-amd64" main.go

# macOS (Apple Silicon)
Write-Host "Building for macOS (ARM64)..."
$env:GOOS = "darwin"
$env:GOARCH = "arm64"
go build -o "$OUTPUT_DIR/go-neuro-integration-darwin-arm64" main.go

Pop-Location

Write-Host ""
Write-Host "=== Cross-platform builds complete ==="
Write-Host "Binaries in: $OUTPUT_DIR"
Get-ChildItem $OUTPUT_DIR
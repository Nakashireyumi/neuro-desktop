# ============================================================
# desktop/scripts/build-go.ps1
# Cross-compiles the server (Go) for every platform we ship.
#   .\scripts\build-go.ps1
# ============================================================
$ErrorActionPreference = 'Stop'

Write-Host 'Building the Neuro Desktop server for every platform...'

$OUTPUT_DIR = 'apps/neuro-integration/dist'
if (Test-Path $OUTPUT_DIR) { Remove-Item -Recurse -Force $OUTPUT_DIR }
New-Item -ItemType Directory -Force -Path $OUTPUT_DIR | Out-Null

Push-Location 'apps/neuro-integration'
try {
    $targets = @(
        @{ GOOS = 'windows'; GOARCH = 'amd64'; Name = 'windows-amd64.exe' },
        @{ GOOS = 'linux';   GOARCH = 'amd64'; Name = 'linux-amd64' },
        @{ GOOS = 'darwin';  GOARCH = 'amd64'; Name = 'darwin-amd64' },
        @{ GOOS = 'darwin';  GOARCH = 'arm64'; Name = 'darwin-arm64' }
    )

    foreach ($target in $targets) {
        Write-Host ("  -> {0}/{1}" -f $target.GOOS, $target.GOARCH)
        $env:GOOS = $target.GOOS
        $env:GOARCH = $target.GOARCH
        $env:CGO_ENABLED = '0'
        go build -trimpath -o "$OUTPUT_DIR/neuro-integration-$($target.Name)" .
        if ($LASTEXITCODE -ne 0) { throw "go build failed for $($target.GOOS)/$($target.GOARCH)" }
    }
} finally {
    Pop-Location
    Remove-Item Env:\GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:\GOARCH -ErrorAction SilentlyContinue
    Remove-Item Env:\CGO_ENABLED -ErrorAction SilentlyContinue
}

Write-Host ''
Write-Host '=== Cross-platform builds complete ==='
Write-Host "Binaries in: $OUTPUT_DIR"
Get-ChildItem $OUTPUT_DIR

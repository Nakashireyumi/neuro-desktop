# ============================================================
# desktop/scripts/build-all.ps1 — Windows build for the shipped product.
#
# The product is the Go server, the Go dashboard program, the Python agent and the
# dashboard page (see docs/ARCHITECTURE.md). Usage, from the desktop\ folder:
#   .\scripts\build-all.ps1              # build + test
#   .\scripts\build-all.ps1 -NoTest      # build only
# ============================================================
[CmdletBinding()]
param(
    [switch]$NoTest
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

function Invoke-Checked([string]$What, [scriptblock]$Block) {
    & $Block
    if ($LASTEXITCODE -ne 0) { throw "$What failed (exit $LASTEXITCODE)" }
}

Write-Host '=== neuro-desktop build (windows) ==='

Write-Host '[1/5] Server (../headless/apps/neuro-integration)'
Push-Location '../headless/apps/neuro-integration'
try {
    if (-not $NoTest) {
        Invoke-Checked 'go vet' { go vet ./... }
        Invoke-Checked 'go test' { go test -count=1 ./... }
    }
    New-Item -ItemType Directory -Force -Path 'dist' | Out-Null
    Invoke-Checked 'go build' { go build -o 'dist/neuro-integration.exe' . }
} finally {
    Pop-Location
}
Write-Host '      -> ../headless/apps/neuro-integration/dist/neuro-integration.exe'

Write-Host '[2/5] Dashboard program (../headless/apps/neuro-dashboard)'
Push-Location '../headless/apps/neuro-dashboard'
try {
    if (-not $NoTest) {
        Invoke-Checked 'go vet (dashboard)' { go vet ./... }
        Invoke-Checked 'go test (dashboard)' { go test -count=1 ./... }
    }
    New-Item -ItemType Directory -Force -Path 'dist' | Out-Null
    Invoke-Checked 'go build (dashboard)' { go build -o 'dist/neuro-dashboard.exe' . }
} finally {
    Pop-Location
}
Write-Host '      -> ../headless/apps/neuro-dashboard/dist/neuro-dashboard.exe'

Write-Host '[3/5] Dashboard (frontend)'
Push-Location 'frontend'
try {
    if (Test-Path 'package-lock.json') {
        Invoke-Checked 'npm ci' { npm ci }
    } else {
        Invoke-Checked 'npm install' { npm install }
    }
    if (-not $NoTest) { Invoke-Checked 'frontend tests' { npm test } }
    Invoke-Checked 'npm run build' { npm run build }
} finally {
    Pop-Location
}
Write-Host '      -> frontend/dist'

Write-Host '[4/5] Agent (python/controller)'
Push-Location 'python'
try {
    Invoke-Checked 'python compileall' { python -m compileall -q controller | Out-Null }
    if (-not $NoTest) {
        $PreviousHeadless = $env:NEURO_HEADLESS
        try {
            $env:NEURO_HEADLESS = '1'
            Invoke-Checked 'agent tests' { python -m unittest discover -s tests -t . | Out-Null }
        } finally { $env:NEURO_HEADLESS = $PreviousHeadless }
    }
} finally {
    Pop-Location
}
Write-Host '      -> python/controller'

Write-Host '[+] Go client, slice 1 (not the shipping desktop client)'
Push-Location '../headless/apps/neuro-client-go'
try {
    if (-not $NoTest) {
        Invoke-Checked 'go vet (Go client)' { go vet ./... }
        Invoke-Checked 'go test (Go client)' { go test -count=1 ./... }
    }
    New-Item -ItemType Directory -Force -Path 'dist' | Out-Null
    Invoke-Checked 'go build (Go client)' { go build -o 'dist/neuro-client-go.exe' . }
} finally { Pop-Location }

Write-Host '[5/5] Native manager (GTK 4)'
if (-not $NoTest) {
    $PreviousPath = $env:PYTHONPATH
    try {
        $env:PYTHONPATH = 'apps/neuro-manager'
        Invoke-Checked 'manager tests' { python -m unittest discover -s apps/neuro-manager/tests }
    } finally { $env:PYTHONPATH = $PreviousPath }
}
Invoke-Checked 'native manager staging' { python scripts/build-manager.py --out dist/manager-source }

Write-Host ''
Write-Host 'Build complete.'
Write-Host '  Release bundle:  .\scripts\bundle\prod.ps1'
Write-Host '  Run from source: .\scripts\bundle\dev.ps1'

# ============================================================
# desktop/scripts/bundle/dev.ps1 - development bundle + run
#   .\scripts\bundle\dev.ps1
#   $env:NEURO_BUNDLE_NO_LAUNCH = '1'; .\scripts\bundle\dev.ps1   # stage only
#
# Stages dist\dev and runs the server, local agent and native GTK manager.
# Set NEURO_NO_AGENT=1 or NEURO_NO_MANAGER=1 for split-machine/headless setups.
# The optional browser dashboard remains at http://127.0.0.1:8300/ui/.
# Not verified on Windows from the development environment: see docs/PRODUCTION_TODO.md.
# ============================================================
$ErrorActionPreference = 'Stop'

# Copy bundled defaults without replacing an operator's edited configuration or
# game profiles. Catalog metadata is refreshed separately below.
function Copy-SeedDirectory([string]$Source, [string]$Destination) {
    New-Item -ItemType Directory -Force -Path $Destination | Out-Null
    foreach ($Item in Get-ChildItem -LiteralPath $Source -Force) {
        $Target = Join-Path $Destination $Item.Name
        if ($Item.PSIsContainer) {
            Copy-SeedDirectory $Item.FullName $Target
        } elseif (!(Test-Path -LiteralPath $Target)) {
            Copy-Item -LiteralPath $Item.FullName -Destination $Target
        }
    }
}

$Root = Resolve-Path (Join-Path $PSScriptRoot '..\..')
Push-Location $Root
try {
    $Dist = 'dist/dev'
    $Server = 'neuro-integration.exe'

    Write-Host '=== Development Bundle ==='

    Write-Host "Updating $Dist (keeping credentials, policy, installed extensions and custom profiles)..."
    New-Item -ItemType Directory -Force -Path $Dist | Out-Null

    Write-Host 'Building the server...'
    Push-Location 'apps/neuro-integration'
    try {
        New-Item -ItemType Directory -Force -Path 'dist' | Out-Null
        # -tags neurodev: only development bundles accept NEURO_EXTENSIONS_ALLOW_UNSIGNED.
        & go build -tags neurodev -o "dist/$Server" .
        if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
    }
    finally {
        Pop-Location
    }
    Copy-Item "apps/neuro-integration/dist/$Server" "$Dist/$Server" -Force

    Write-Host 'Building the dashboard program (apps/neuro-dashboard)...'
    Push-Location 'apps/neuro-dashboard'
    try {
        New-Item -ItemType Directory -Force -Path 'dist' | Out-Null
        & go build -o 'dist/neuro-dashboard.exe' .
        if ($LASTEXITCODE -ne 0) { throw 'go build (neuro-dashboard) failed' }
    }
    finally {
        Pop-Location
    }
    Copy-Item 'apps/neuro-dashboard/dist/neuro-dashboard.exe' "$Dist/neuro-dashboard.exe" -Force

    Write-Host 'Building the dashboard client...'
    Push-Location 'frontend'
    try {
        & npm ci
        if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' }
        & npm run build
        if ($LASTEXITCODE -ne 0) { throw 'npm run build failed' }
    }
    finally {
        Pop-Location
    }
    if (Test-Path "$Dist/frontend") { Remove-Item -Recurse -Force "$Dist/frontend" }
    New-Item -ItemType Directory -Force -Path "$Dist/frontend" | Out-Null
    Copy-Item 'frontend/dist/*' "$Dist/frontend" -Recurse -Force

    Write-Host 'Staging the native GTK manager...'
    & python 'scripts/build-manager.py' --out "$Dist/manager"
    if ($LASTEXITCODE -ne 0) { throw 'native manager staging failed' }

    Write-Host 'Staging the agent, config and catalog...'
    if (Test-Path "$Dist/agent") { Remove-Item -Recurse -Force "$Dist/agent" }
    New-Item -ItemType Directory -Force -Path "$Dist/agent" | Out-Null
    Copy-Item 'backend/python/controller' "$Dist/agent/controller" -Recurse -Force
    Get-ChildItem -Path "$Dist/agent" -Recurse -Directory -Filter '__pycache__' |
        Remove-Item -Recurse -Force
    Copy-Item 'backend/python/requirements.txt' "$Dist/agent/requirements.txt" -Force
    Copy-Item 'backend/python/requirements-windows.txt' "$Dist/agent/requirements-windows.txt" -Force
    Copy-SeedDirectory 'config' "$Dist/config"
    New-Item -ItemType Directory -Force -Path "$Dist/catalog" | Out-Null
    foreach ($Item in Get-ChildItem -LiteralPath 'catalog' -Force) {
        if ($Item.Name -in @('extensions-state.json', 'extension-permissions.json', 'integrations.json')) { continue }
        if ($Item.PSIsContainer) {
            Copy-SeedDirectory $Item.FullName (Join-Path "$Dist/catalog" $Item.Name)
        } else {
            Copy-Item -LiteralPath $Item.FullName -Destination "$Dist/catalog" -Force
        }
    }
    if (!(Test-Path "$Dist/permissions.json")) {
        Copy-Item 'apps/neuro-integration/permissions.example.json' "$Dist/permissions.json"
    }
    $Launcher = Get-Content 'scripts/bundle/templates/start.bat' -Raw
    Set-Content -Path "$Dist/start.bat" -Value ($Launcher -replace '@SERVER@', $Server) -NoNewline

    $MarketplaceSource = Join-Path $Root 'catalog/marketplace.json'
    if ($env:NEURO_MARKETPLACE_SOURCE) { $MarketplaceSource = $env:NEURO_MARKETPLACE_SOURCE }
    if (Test-Path $MarketplaceSource) { Copy-Item $MarketplaceSource "$Dist/catalog/marketplace.json" -Force }

    Write-Host ''
    Write-Host "=== Dev bundle complete: $Dist ==="
    Write-Host ''
    Write-Host '1. Start the server, agent and native manager together:'
    Write-Host "     cd $Dist ; .\start.bat --ws-url ws://localhost:8000"
    Write-Host '   setup prints the dashboard token once: copy it. It is the sign-in token.' 
    Write-Host '2. Remote agent (set NEURO_NO_AGENT=1 on the server):'
    Write-Host '     cd dist\dev\agent ; python -m controller.agent --bridge 127.0.0.1:9876'
    Write-Host '3. The source manager needs GTK 4 + PyGObject (see apps/neuro-manager/README.md).'
    Write-Host '   Optional browser dashboard: http://127.0.0.1:8300/ui/'
    Write-Host ''

    if ($env:NEURO_BUNDLE_NO_LAUNCH -eq '1') {
        Write-Host 'Skipping launch (NEURO_BUNDLE_NO_LAUNCH=1)'
        return
    }

    Push-Location $Dist
    try {
        Write-Host 'Launching the server, local agent and native manager (Ctrl-C to stop)...'
        & '.\start.bat'
        if ($LASTEXITCODE -ne 0) { throw "launcher failed (exit $LASTEXITCODE)" }
    }
    finally {
        Pop-Location
    }
}
finally {
    Pop-Location
}

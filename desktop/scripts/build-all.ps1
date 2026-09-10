# ============================================================
# scripts/build-all.ps1 - Complete Build System
# ============================================================

param(
    [Parameter(Mandatory = $false)]
    [ValidateSet('Debug', 'Release')]
    [string]$Configuration = 'Release',

    [Parameter(Mandatory = $false)]
    [switch]$SkipTests,

    [Parameter(Mandatory = $false)]
    [switch]$Clean
)

$ErrorActionPreference = 'Stop'

$Green = [ConsoleColor]::Green
$Red = [ConsoleColor]::Red
$Yellow = [ConsoleColor]::Yellow
$Cyan = [ConsoleColor]::Cyan

function Write-Step {
    param([string]$Message)
    Write-Host "`n[*] $Message" -ForegroundColor $Cyan
}

function Write-Success {
    param([string]$Message)
    Write-Host "  [OK] $Message" -ForegroundColor $Green
}

function Write-ErrorLine {
    param([string]$Message)
    Write-Host "  [ERR] $Message" -ForegroundColor $Red
}

function Write-Info {
    param([string]$Message)
    Write-Host "  [..] $Message" -ForegroundColor $Yellow
}

$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot '..')
Push-Location $RepoRoot

try {
    Write-Host '=======================================================' -ForegroundColor $Cyan
    Write-Host '        Neuro Desktop Build System' -ForegroundColor $Cyan
    Write-Host '=======================================================' -ForegroundColor $Cyan
    Write-Host ''
    Write-Info "Configuration: $Configuration"
    Write-Info "Skip Tests: $SkipTests"
    Write-Info "Clean Build: $Clean"

    if ($Clean) {
        Write-Step 'Cleaning previous builds...'

        Remove-Item -Recurse -Force 'dist' -ErrorAction SilentlyContinue
        Remove-Item -Recurse -Force 'apps/neuro-desktop/target' -ErrorAction SilentlyContinue
        Remove-Item -Recurse -Force 'apps/process-handler/build' -ErrorAction SilentlyContinue
        Remove-Item -Recurse -Force 'apps/neuro-integration/dist' -ErrorAction SilentlyContinue
        Remove-Item -Recurse -Force 'frontend/dist' -ErrorAction SilentlyContinue

        Write-Success 'Clean completed'
    }

    Write-Step 'Building Process Handler (C++)...'
    Push-Location 'apps/process-handler'
    try {
        if (Test-Path 'build/CMakeCache.txt') {
            $cmakeCache = Get-Content 'build/CMakeCache.txt' -Raw
            if ($cmakeCache -match 'native/process-handler') {
                Write-Info 'Removing stale CMake cache from old native/ path...'
                Remove-Item -Recurse -Force 'build'
            }
        }

        if (!(Test-Path 'build')) {
            New-Item -ItemType Directory -Path 'build' | Out-Null
        }

        Push-Location 'build'
        try {
            $cmakeArgs = @('..', "-DCMAKE_BUILD_TYPE=$Configuration")
            if ($SkipTests) {
                $cmakeArgs += '-DBUILD_TESTS=OFF'
            }
            else {
                $cmakeArgs += '-DBUILD_TESTS=ON'
            }

            Write-Info 'Running CMake configure...'
            & cmake @cmakeArgs
            if ($LASTEXITCODE -ne 0) { throw 'CMake configure failed' }

            Write-Info 'Building C++ targets...'
            & cmake --build . --config $Configuration
            if ($LASTEXITCODE -ne 0) { throw 'CMake build failed' }

            if (!$SkipTests) {
                Write-Info 'Running C++ tests...'
                & ctest --output-on-failure -C $Configuration
                if ($LASTEXITCODE -ne 0) { throw 'C++ tests failed' }
            }
        }
        finally {
            Pop-Location
        }

        Write-Success 'Process Handler built successfully'
    }
    catch {
        Write-ErrorLine "Process Handler build failed: $_"
        exit 1
    }
    finally {
        Pop-Location
    }

    Write-Step 'Building Go Integration...'
    Push-Location 'apps/neuro-integration'
    try {
        New-Item -ItemType Directory -Force -Path 'dist' | Out-Null

        $previousGOOS = $env:GOOS
        $previousGOARCH = $env:GOARCH

        try {
            $env:GOOS = 'windows'
            $env:GOARCH = 'amd64'

            Write-Info 'Building neuro-integration.exe...'
            & go build -o 'dist/neuro-integration.exe' .
            if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }

            if (!$SkipTests) {
                Write-Info 'Running Go tests...'
                & go test -v ./...
                if ($LASTEXITCODE -ne 0) { throw 'Go tests failed' }
            }
        }
        finally {
            $env:GOOS = $previousGOOS
            $env:GOARCH = $previousGOARCH
        }

        Write-Success 'Go integration built successfully'
    }
    catch {
        Write-ErrorLine "Go integration build failed: $_"
        exit 1
    }
    finally {
        Pop-Location
    }

    $relaySourceDir = $env:NEURO_RELAY_SOURCE_DIR
    if ($relaySourceDir) {
        Write-Step 'Building Neuro Relay (optional)...'
        if (Test-Path (Join-Path $relaySourceDir 'src/entrypoint.go')) {
            Push-Location $relaySourceDir
            try {
                $relayOut = Join-Path $RepoRoot 'apps/neuro-integration/dist/neuro-relay.exe'
                Write-Info "Building relay from: $relaySourceDir"
                & go build -o $relayOut ./src/entrypoint.go
                if ($LASTEXITCODE -ne 0) { throw 'Relay build failed' }

                $env:NEURO_RELAY_BINARY_SOURCE = $relayOut
                Write-Success "Neuro Relay built at $relayOut"
            }
            catch {
                Write-ErrorLine "Neuro Relay build failed: $_"
                exit 1
            }
            finally {
                Pop-Location
            }
        }
        else {
            Write-Info "Skipping relay build: src/entrypoint.go not found at $relaySourceDir"
        }
    }

    Write-Step 'Building Rust Application...'
    Push-Location 'apps/neuro-desktop'
    try {
        $cargoBuildArgs = @('build')
        $cargoTestArgs = @('test')

        if ($Configuration -eq 'Release') {
            $cargoBuildArgs += '--release'
            $cargoTestArgs += '--release'
        }

        Write-Info 'Running cargo build...'
        & cargo @cargoBuildArgs
        if ($LASTEXITCODE -ne 0) { throw 'Cargo build failed' }

        if (!$SkipTests) {
            Write-Info 'Running cargo test...'
            & cargo @cargoTestArgs
            if ($LASTEXITCODE -ne 0) { throw 'Cargo tests failed' }
        }

        Write-Success 'Rust application built successfully'
    }
    catch {
        Write-ErrorLine "Rust build failed: $_"
        exit 1
    }
    finally {
        Pop-Location
    }

    Write-Step 'Building Frontend...'
    Push-Location 'frontend'
    try {
        if (!(Test-Path 'node_modules')) {
            Write-Info 'Installing npm dependencies...'
            & npm install
            if ($LASTEXITCODE -ne 0) { throw 'npm install failed' }
        }

        Write-Info 'Running npm build...'
        & npm run build
        if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed' }

        if (!$SkipTests) {
            Write-Info 'Running frontend tests (if present)...'
            & npm run test --if-present
            if ($LASTEXITCODE -ne 0) { throw 'Frontend tests failed' }
        }

        Write-Success 'Frontend built successfully'
    }
    catch {
        Write-ErrorLine "Frontend build failed: $_"
        exit 1
    }
    finally {
        Pop-Location
    }

    Write-Step 'Creating development bundle...'
    try {
        & .\scripts\bundle\dev.ps1
        if ($LASTEXITCODE -ne 0) { throw 'Bundle script failed' }
        Write-Success 'Development bundle created'
    }
    catch {
        Write-ErrorLine "Bundle creation failed: $_"
        exit 1
    }

    if (!$SkipTests -and (Test-Path 'tests/package.json')) {
        Write-Step 'Running integration tests...'
        Push-Location 'tests'
        try {
            if (!(Test-Path 'node_modules')) {
                Write-Info 'Installing integration test dependencies...'
                & npm install
                if ($LASTEXITCODE -ne 0) { throw 'Integration npm install failed' }
            }

            Write-Info 'Running integration tests...'
            & npm test
            if ($LASTEXITCODE -ne 0) { throw 'Integration tests failed' }

            Write-Success 'Integration tests passed'
        }
        catch {
            Write-ErrorLine "Integration tests failed: $_"
            exit 1
        }
        finally {
            Pop-Location
        }
    }

    $rustOutDir = if ($Configuration -eq 'Release') { 'release' } else { 'debug' }

    Write-Host ''
    Write-Host '=======================================================' -ForegroundColor $Green
    Write-Host '        Build Completed Successfully' -ForegroundColor $Green
    Write-Host '=======================================================' -ForegroundColor $Green
    Write-Host ''
    Write-Info 'Build artifacts:'
    Write-Host '  - Process Handler: apps/process-handler/build/process-handler.exe'
    Write-Host '  - Go Integration:  apps/neuro-integration/dist/neuro-integration.exe'
    Write-Host "  - Rust App:        apps/neuro-desktop/target/$rustOutDir/neuro-desktop.exe"
    Write-Host '  - Frontend:        frontend/dist/'
    Write-Host ''
    Write-Info 'To create a production bundle:'
    Write-Host '  .\scripts\bundle\prod.ps1'
}
finally {
    Pop-Location
}


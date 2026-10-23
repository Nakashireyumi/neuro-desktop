# Builds neuro-client.exe: the program that runs on the PC Neuro controls. It is the
# controller agent packaged as one executable, so the controlled PC needs no Python
# install and runs nothing else (no dashboard, no vision, no policy).
#
#   .\scripts\build-client.ps1          # -> desktop\dist\neuro-client\neuro-client.exe
#
# PyInstaller does not cross-compile: run this on Windows. Not verified on Windows
# from the development environment; see docs/PRODUCTION_TODO.md.
$ErrorActionPreference = 'Stop'

$Root = Resolve-Path (Join-Path $PSScriptRoot '..')
Set-Location $Root

$PyInstallerVersion = '6.22.3'
$Out = if ($env:NEURO_CLIENT_OUT) { $env:NEURO_CLIENT_OUT } else { Join-Path $Root 'dist\neuro-client' }
$Work = Join-Path $Root 'dist\.client-build'
$Venv = Join-Path $Work 'venv'

New-Item -ItemType Directory -Force -Path $Work | Out-Null
if (-not (Test-Path (Join-Path $Venv 'Scripts\python.exe'))) {
    python -m venv $Venv
    if ($LASTEXITCODE -ne 0) { throw 'Creating the client build environment failed' }
}
$Py = Join-Path $Venv 'Scripts\python.exe'
& $Py -m pip install --quiet --upgrade pip
if ($LASTEXITCODE -ne 0) { throw 'Updating pip in the client build environment failed' }
& $Py -m pip install --quiet -r python\requirements.txt -r python\requirements-windows.txt "pyinstaller==$PyInstallerVersion"
if ($LASTEXITCODE -ne 0) { throw 'Installing the client build dependencies failed' }

Remove-Item -Recurse -Force -ErrorAction SilentlyContinue (Join-Path $Work 'pyi'), $Out
New-Item -ItemType Directory -Force -Path (Join-Path $Work 'pyi'), $Out | Out-Null
# Input and screenshots use PyAutoGUI, MSS and Pillow; optional image matching
# must not collect ambient OpenCV/NumPy and their video/Qt/VTK libraries.
& (Join-Path $Venv 'Scripts\pyinstaller.exe') --noconfirm --onefile --clean `
    --name neuro-client `
    --paths python `
    --collect-submodules pynput `
    --exclude-module cv2 --exclude-module numpy `
    --distpath $Out `
    --workpath (Join-Path $Work 'pyi\build') `
    --specpath (Join-Path $Work 'pyi') `
    ..\headless\apps\neuro-client\neuro_client.py
if ($LASTEXITCODE -ne 0) { throw 'PyInstaller failed' }
Write-Host "built: $(Join-Path $Out 'neuro-client.exe')"
& (Join-Path $Out 'neuro-client.exe') --help
if ($LASTEXITCODE -ne 0) { throw 'The frozen client failed its --help smoke check' }
Write-Host 'smoke: --help runs'

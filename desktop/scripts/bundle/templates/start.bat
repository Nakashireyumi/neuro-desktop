@echo off
rem Neuro Desktop launcher (bundled). Installed as start.bat.
rem
rem Starts the server (dashboard at http://127.0.0.1:8300/ui/) and the agent on this
rem PC. The agent is neuro-client\neuro-client.exe when the bundle has it, else the
rem Python agent in agent\ (needs Python with agent\requirements.txt installed).
rem For a split-machine setup, set NEURO_NO_AGENT=1 and run the server with
rem start.bat. On the PC Neuro controls, copy the neuro-client folder and run:
rem   neuro-client.exe --bridge <ip>:9876 --token <executor token>
setlocal

cd /d "%~dp0"

set NEURO_UI_DIR=%~dp0frontend
set NEURO_IPC_FILE=%~dp0neuro_ipc.json
set NEURO_PERMISSIONS_FILE=%~dp0permissions.json
set NEURO_CATALOG_FILE=%~dp0catalog\index.json
set NEURO_GAME_PROFILES_DIR=%~dp0catalog\games
set NEURO_EXTENSIONS_STATE_FILE=%~dp0catalog\extensions-state.json
set PYTHONPATH=%~dp0agent;%~dp0agent\Lib;%PYTHONPATH%

if not exist "%~dp0@SERVER@" (
  echo Cannot find the server binary at %~dp0@SERVER@
  pause
  exit /b 1
)

rem First-run step. It is idempotent: it prints the dashboard token only the first
rem time, and a failed check stops the launch here with the reason printed.
"%~dp0@SERVER@" setup
if errorlevel 1 (
  echo Setup is not complete. Fix the FAIL lines above, then run start.bat again.
  pause
  exit /b 1
)

echo Dashboard: http://127.0.0.1:8300/ui/ ^(sign in with the dashboard token^)

if "%NEURO_NO_AGENT%"=="1" goto run_server

if exist "%~dp0neuro-client\neuro-client.exe" (
  start "Neuro Desktop agent" /min "%~dp0neuro-client\neuro-client.exe" --bridge 127.0.0.1:9876
  goto run_server
)

where python >nul 2>nul
if errorlevel 1 goto no_agent

start "Neuro Desktop agent" /min python -m controller.agent --bridge 127.0.0.1:9876
goto run_server

:no_agent
echo No agent started: this bundle has no neuro-client\ and Python was not found.
echo On the PC Neuro controls run:
echo   neuro-client.exe --bridge ^<server-ip^>:9876 --token ^<executor token^>

:run_server
if "%NEURO_NO_MANAGER%"=="1" goto server
if not defined NEURO_MANAGER_SERVER set NEURO_MANAGER_SERVER=http://127.0.0.1:8300
if exist "%~dp0manager\neuro-manager.exe" (
  start "Neuro Desktop Manager" "%~dp0manager\neuro-manager.exe" --server "%NEURO_MANAGER_SERVER%"
) else if exist "%~dp0manager\neuro-manager.cmd" (
  start "Neuro Desktop Manager" cmd /c ""%~dp0manager\neuro-manager.cmd" --server "%NEURO_MANAGER_SERVER%""
)
:server
"%~dp0@SERVER@" %*
pause

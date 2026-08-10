@echo off
REM Observer management wrapper for cmd.exe
REM Usage: observer.bat [command] [args...]
REM   commands: start | stop | restart | status | logs | config | doctor
REM   options:  -Port <port>  (dashboard port, default 8099)
REM Defaults to 'start' when no command is given (double-click to launch).

set SCRIPT_DIR=%~dp0
set REPO_ROOT=%SCRIPT_DIR%..
set PS_SCRIPT=%SCRIPT_DIR%observer.ps1

if not exist "%PS_SCRIPT%" (
    echo Error: observer.ps1 not found at %PS_SCRIPT%
    exit /b 1
)

if "%~1"=="" (
    set CMD_ARGS=start
) else (
    set CMD_ARGS=%*
)

REM Prefer PowerShell 7 (pwsh); the script uses PS7-only syntax.
where pwsh.exe >nul 2>nul
if %ERRORLEVEL%==0 (
    pwsh.exe -NoProfile -ExecutionPolicy Bypass -File "%PS_SCRIPT%" %CMD_ARGS%
) else (
    powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%PS_SCRIPT%" %CMD_ARGS%
)
exit /b %ERRORLEVEL%

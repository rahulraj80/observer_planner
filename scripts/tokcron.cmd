@echo off
rem scripts\tokcron.cmd - run the Tokenomics job's entry point from cmd.exe.
rem Arguments reach WSL as argv through `wsl.exe --exec` (no Linux shell in
rem between). SBO_WSL_DISTRO / SBO_TOKCRON_EXEC as in scripts/tokcron.
setlocal
if "%SBO_WSL_DISTRO%"=="" set "SBO_WSL_DISTRO=Ubuntu-20.04"
if "%SBO_TOKCRON_EXEC%"=="" set "SBO_TOKCRON_EXEC=./.local/bin/tokcron"
wsl.exe -d %SBO_WSL_DISTRO% --cd ~ --exec %SBO_TOKCRON_EXEC% %*
exit /b %ERRORLEVEL%

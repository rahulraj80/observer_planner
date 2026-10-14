# scripts/tokcron.ps1 - run the Tokenomics job's entry point from PowerShell.
# Arguments reach WSL as argv through `wsl.exe --exec` (no Linux shell in
# between). SBO_WSL_DISTRO / SBO_TOKCRON_EXEC as in scripts/tokcron.
$distro = if ($env:SBO_WSL_DISTRO) { $env:SBO_WSL_DISTRO } else { 'Ubuntu-20.04' }
$target = if ($env:SBO_TOKCRON_EXEC) { $env:SBO_TOKCRON_EXEC } else { './.local/bin/tokcron' }
& wsl.exe -d $distro --cd '~' --exec $target @args
exit $LASTEXITCODE

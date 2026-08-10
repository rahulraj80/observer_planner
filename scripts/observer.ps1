<#>
.SYNOPSIS
    Observer management script - start, stop, restart, status, logs

.DESCRIPTION
    A unified PowerShell script to manage the SuperBased Observer daemon and
    its associated services. Supports both the solo observer binary and the
    observer-org server.

    Start is single-instance: any existing observer.exe is stopped first, and
    the daemon's dashboard is served on 127.0.0.1:8099 (override with -Port).

.NOTES
    Works with the observer binary built from this repo (go build ./cmd/observer).
    Assumes default config at ~/.observer/config.toml or project-local .observer.toml
#>

param(
    [Parameter(Mandatory=$true, Position=0)]
    [ValidateSet('start','stop','restart','status','logs','config','doctor')]
    [string]$Command,

    [Parameter()]
    [int]$Port = 8099,

    [Parameter()]
    [string[]]$PassThru
)

$ErrorActionPreference = 'Stop'

# Configuration
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$RepoRoot = Resolve-Path (Join-Path $ScriptDir '..')
$BinDir = Join-Path $RepoRoot 'bin'
$ObserverBin = Join-Path $BinDir 'observer.exe'
$ObserverOrgBin = Join-Path $BinDir 'observer-org.exe'

# Default paths
$UserConfigDir = Join-Path $env:USERPROFILE '.observer'
$UserConfigFile = Join-Path $UserConfigDir 'config.toml'
$ProjectConfigFile = Join-Path $RepoRoot '.observer.toml'
$DataDir = Join-Path $UserConfigDir 'data'
$LogFile = Join-Path $DataDir 'observer.log'
$ErrLogFile = Join-Path $DataDir 'observer.err.log'
$ObserverPidFilePath = Join-Path $DataDir 'observer.pid'
$DashboardAddr = "127.0.0.1:$Port"

function Test-Binary {
    param([string]$Path)
    if (-not (Test-Path $Path)) {
        Write-Error "Binary not found: $Path"
        Write-Error "Run 'go build ./cmd/observer' or 'make build' first."
        exit 1
    }
}

function Get-ConfigPath {
    # Priority: explicit --config > project .observer.toml > ~/.observer/config.toml
    if ($global:ConfigPath) { return $global:ConfigPath }
    if (Test-Path $ProjectConfigFile) { return $ProjectConfigFile }
    if (Test-Path $UserConfigFile) { return $UserConfigFile }
    return $null
}

function Get-ConfigLabel {
    $config = Get-ConfigPath
    if ($config) { return $config }
    return '<defaults>'
}

function Get-ObserverPid {
    if (Test-Path $ObserverPidFilePath) {
        $p = (Get-Content $ObserverPidFilePath -Raw).Trim()
        if ($p -match '^\d+$') { return [int]$p }
    }
    return $null
}

function Is-Running {
    $p = Get-ObserverPid
    if ($null -eq $p) { return $false }
    try {
        $proc = Get-Process -Id $p -ErrorAction Stop
        return $proc.ProcessName -eq 'observer'
    }
    catch {
        return $false
    }
}

# Stop-StrayObservers kills every running observer.exe so start can never
# leave more than one instance behind (single-instance guarantee).
function Stop-StrayObservers {
    $procs = Get-Process -Name 'observer' -ErrorAction SilentlyContinue
    if ($procs) {
        $procs | ForEach-Object { Stop-Process -Id $_.Id -Force -ErrorAction SilentlyContinue }
        Start-Sleep -Milliseconds 500
        return $procs.Count
    }
    return 0
}

function Start-Observer {
    param(
        [string[]]$ExtraArgs = @(),
        [switch]$Org = $false
    )

    $killed = Stop-StrayObservers
    if ($killed -gt 0) {
        Write-Host "Stopped $killed existing observer instance(s)."
    }

    $bin = if ($Org) { $ObserverOrgBin } else { $ObserverBin }
    Test-Binary $bin

    $config = Get-ConfigPath
    $cmdArgs = @("start", "--dashboard-addr", $DashboardAddr)
    if ($config) { $cmdArgs += "--config", $config }
    $cmdArgs += $ExtraArgs

    Write-Host "Starting $(if ($Org) { 'observer-org' } else { 'observer' }) with config: $(Get-ConfigLabel)..."
    Write-Host "Dashboard: http://$DashboardAddr"

    # Ensure data dir exists
    New-Item -ItemType Directory -Force -Path $DataDir | Out-Null

    # Redirect stdout/stderr to files (not pipes) so the daemon can never
    # block on an unread pipe buffer while it runs detached.
    $stdout = [System.IO.File]::Open($LogFile, [System.IO.FileMode]::Append, [System.IO.FileAccess]::Write, [System.IO.FileShare]::Read)
    $stderr = [System.IO.File]::Open($ErrLogFile, [System.IO.FileMode]::Append, [System.IO.FileAccess]::Write, [System.IO.FileShare]::Read)

    try {
        $psi = New-Object System.Diagnostics.ProcessStartInfo
        $psi.FileName = $bin
        foreach ($arg in $cmdArgs) { $psi.ArgumentList.Add($arg) }
        $psi.UseShellExecute = $false
        $psi.RedirectStandardOutput = $true
        $psi.RedirectStandardError = $true
        $psi.CreateNoWindow = $true
        $psi.WorkingDirectory = $RepoRoot

        $proc = New-Object System.Diagnostics.Process
        $proc.StartInfo = $psi
        $proc.Start() | Out-Null
        $proc.Id | Out-File -FilePath $ObserverPidFilePath -Encoding ascii -NoNewline

        Write-Host "Started with PID $($proc.Id). Log: $LogFile"
    }
    finally {
        $stdout.Close()
        $stderr.Close()
    }
}

function Stop-Observer {
    $p = Get-ObserverPid

    $killed = Stop-StrayObservers
    if ($p -and (Is-Running)) {
        Write-Host "Stopping observer (PID $p)..."
    }
    elseif ($p) {
        Write-Warning "PID file exists but process not running. Cleaning up."
    }

    if ($killed -gt 0) {
        Write-Host "Stopped $killed observer process(es)."
    }
    elseif ($p) {
        Write-Host "Stopped."
    }
    else {
        Write-Host "No observer process found."
    }

    if (Test-Path $ObserverPidFilePath) { Remove-Item $ObserverPidFilePath -Force }
}

function Show-Status {
    $config = Get-ConfigLabel
    if (Is-Running) {
        $p = Get-ObserverPid
        $proc = Get-Process -Id $p
        Write-Host "Status: RUNNING (PID $p)" -ForegroundColor Green
        Write-Host "  Dashboard: http://$DashboardAddr"
        Write-Host "  CPU: $($proc.TotalProcessorTime.TotalSeconds.ToString('F1'))s  Mem: $($proc.WorkingSet64/1MB) MB"
        Write-Host "  Started: $($proc.StartTime)"
        Write-Host "  Config: $config"
        Write-Host "  Log: $LogFile"
        Write-Host "  Data: $DataDir"
    }
    else {
        Write-Host "Status: STOPPED" -ForegroundColor Red
        Write-Host "  Dashboard: http://$DashboardAddr"
        Write-Host "  Config: $config"
        Write-Host "  Data: $DataDir"
    }
}

function Show-Logs {
    param([int]$Lines = 100, [switch]$Follow)

    $path = if (Test-Path $LogFile) { $LogFile } else { $ErrLogFile }
    if (-not (Test-Path $path)) {
        Write-Warning "Log file not found: $LogFile"
        return
    }

    if ($Follow) {
        Write-Host "Following $path (Ctrl+C to stop)..."
        Get-Content $path -Wait -Tail $Lines
    }
    else {
        Get-Content $path -Tail $Lines
    }
}

function Show-Config {
    $config = Get-ConfigPath
    if ($config) {
        Write-Host "Using config: $config"
        Get-Content $config
    }
    else {
        Write-Warning "No config file found."
        Write-Host "Checked:"
        Write-Host "  $ProjectConfigFile"
        Write-Host "  $UserConfigFile"
    }
}

function Run-Doctor {
    Write-Host "=== Observer Doctor ==="
    Write-Host ""

    # Binary
    Write-Host "Binary check:"
    if (Test-Path $ObserverBin) {
        $ver = & $ObserverBin --version 2>&1 | Select-Object -First 1
        if (-not $ver) { $ver = "built (no --version flag)" }
        Write-Host "  observer: OK - $ver" -ForegroundColor Green
    }
    else {
        Write-Host "  observer: MISSING at $ObserverBin" -ForegroundColor Red
    }
    if (Test-Path $ObserverOrgBin) {
        $ver = & $ObserverOrgBin --version 2>&1 | Select-Object -First 1
        if (-not $ver) { $ver = "built (no --version flag)" }
        Write-Host "  observer-org: OK - $ver" -ForegroundColor Green
    }
    else {
        Write-Host "  observer-org: not built" -ForegroundColor Yellow
    }

    # Config
    Write-Host ""
    Write-Host "Config:"
    $config = Get-ConfigPath
    if ($config) {
        Write-Host "  Found: $config" -ForegroundColor Green
    }
    else {
        Write-Host "  Not found (using defaults)" -ForegroundColor Yellow
    }

    # Data dir
    Write-Host ""
    Write-Host "Data directory:"
    if (Test-Path $DataDir) {
        $db = Get-ChildItem $DataDir -Filter '*.db' | Select-Object -First 1
        Write-Host "  Path: $DataDir" -ForegroundColor Green
        if ($db) { Write-Host "  DB: $($db.Name) ($($db.Length/1MB) MB)" }
        else { Write-Host "  DB: none yet" }
    }
    else {
        Write-Host "  Not created yet" -ForegroundColor Yellow
    }

    # Process
    Write-Host ""
    Write-Host "Process:"
    $running = Is-Running
    $strays = @(Get-Process -Name 'observer' -ErrorAction SilentlyContinue)
    if ($running) {
        $p = Get-ObserverPid
        Write-Host "  Running (PID $p)" -ForegroundColor Green
        if ($strays.Count -gt 1) {
            Write-Host "  WARNING: $($strays.Count - 1) extra instance(s) running" -ForegroundColor Yellow
        }
    }
    elseif ($strays.Count -gt 0) {
        Write-Host "  Running but NOT tracked by pid file ($($strays.Count) instance(s))" -ForegroundColor Yellow
        $strays | ForEach-Object { Write-Host "    PID $($_.Id)" }
    }
    else {
        Write-Host "  Not running" -ForegroundColor Red
    }

    # Port check
    Write-Host ""
    Write-Host "Port check (localhost:$Port):"
    try {
        $listener = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction Stop
        if ($listener) {
            Write-Host "  Port ${Port}: LISTENING (PID $($listener.OwningProcess))" -ForegroundColor Green
        }
        else {
            Write-Host "  Port ${Port}: free" -ForegroundColor Yellow
        }
    }
    catch {
        # Fallback for systems without Get-NetTCPConnection
        $tcp = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, $Port)
        try {
            $tcp.Start()
            $tcp.Stop()
            Write-Host "  Port ${Port}: free" -ForegroundColor Yellow
        }
        catch {
            Write-Host "  Port ${Port}: IN USE" -ForegroundColor Green
        }
    }
}

# Main dispatch
switch ($Command) {
    'start' {
        Start-Observer -ExtraArgs $PassThru
    }
    'stop' {
        Stop-Observer
    }
    'restart' {
        Stop-Observer
        Start-Sleep -Seconds 1
        Start-Observer -ExtraArgs $PassThru
    }
    'status' {
        Show-Status
    }
    'logs' {
        $follow = $PassThru -contains '-f' -or $PassThru -contains '--follow'
        $lines = 100
        $idx = $PassThru.IndexOf('-n')
        if ($idx -ge 0 -and $idx+1 -lt $PassThru.Count) {
            $lines = [int]$PassThru[$idx+1]
        }
        Show-Logs -Lines $lines -Follow:$follow
    }
    'config' {
        Show-Config
    }
    'doctor' {
        Run-Doctor
    }
    default {
        Write-Error "Unknown command: $Command"
        exit 1
    }
}

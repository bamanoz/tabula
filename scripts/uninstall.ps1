# Tabula uninstaller — removes service, binary, skills, and optionally user data.
# Usage:
#   irm https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/uninstall.ps1 | iex
#   .\uninstall.ps1 -All

param([switch]$All)

$ErrorActionPreference = "Stop"
$TabulaHome = if ($env:TABULA_HOME) { $env:TABULA_HOME } else { Join-Path $HOME ".tabula" }

function Info($msg)  { Write-Host "==> $msg" -ForegroundColor Blue }
function Ok($msg)    { Write-Host "  ✓ $msg" -ForegroundColor Green }

# ── Stop and remove service ─────────────────────────────────────

$TaskName = "TabulaKernel"
$task = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if ($task) {
    if ($task.State -eq "Running") {
        Stop-ScheduledTask -TaskName $TaskName
        Ok "Service stopped"
    }
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
    Ok "Service removed (Task Scheduler)"
}

# ── Remove installed files ──────────────────────────────────────

if (Test-Path $TabulaHome) {
    Info "Removing $TabulaHome..."

    # Always remove: bin, distrib, skills, testing, service, venv, logs, boot scripts
    foreach ($item in @("bin", "distrib", "skills", "testing", "service", ".venv", "logs", "boot-cicd.py", "tabula.yaml")) {
        $path = Join-Path $TabulaHome $item
        if (Test-Path $path) { Remove-Item -Recurse -Force $path }
    }

    if ($All) {
        Remove-Item -Recurse -Force $TabulaHome
        Ok "Removed $TabulaHome (including user data)"
    } else {
        Ok "Removed installed files (kept data/, IDENTITY.md, etc.)"
        Write-Host "  To remove everything: Remove-Item -Recurse -Force '$TabulaHome'"
    }
}

# ── Clean up environment ────────────────────────────────────────

$BinDir = Join-Path $TabulaHome "bin"
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -like "*$BinDir*") {
    $NewPath = ($UserPath -split ";" | Where-Object { $_ -ne $BinDir }) -join ";"
    [Environment]::SetEnvironmentVariable("Path", $NewPath, "User")
    Ok "Removed from PATH"
}

$CurrentHome = [Environment]::GetEnvironmentVariable("TABULA_HOME", "User")
if ($CurrentHome) {
    [Environment]::SetEnvironmentVariable("TABULA_HOME", $null, "User")
    Ok "Removed TABULA_HOME"
}

Write-Host ""
Write-Host "Tabula uninstalled." -ForegroundColor Green

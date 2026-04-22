# Install Tabula from source to ~\.tabula\
# Run: powershell -ExecutionPolicy Bypass -File scripts/install-dev.ps1

$ErrorActionPreference = "Stop"

$TabulaHome = if ($env:TABULA_HOME) { $env:TABULA_HOME } else { Join-Path $HOME ".tabula" }
$BinDir = Join-Path $TabulaHome "bin"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot = Split-Path -Parent $ScriptDir
$Venv = Join-Path $TabulaHome ".venv"

function New-FlatRuntimeSurface {
    param(
        [string]$SourceDir,
        [string]$DestDir,
        [string]$TabulaHome,
        [string[]]$Preserve = @()
    )

    New-Item -ItemType Directory -Force -Path $DestDir | Out-Null
    Get-ChildItem -Force -Path $DestDir | ForEach-Object {
        if ($Preserve -contains $_.Name) {
            return
        }
        Remove-Item -Recurse -Force $_.FullName
    }
    if (-not (Test-Path $SourceDir)) {
        return
    }
    Get-ChildItem -Force -Path $SourceDir | ForEach-Object {
        $rel = $_.FullName.Substring($TabulaHome.Length + 1).Replace('\', '/')
        $target = Join-Path $DestDir $_.Name
        New-Item -ItemType Junction -Path $target -Target (Join-Path $TabulaHome $rel) | Out-Null
    }
}

Write-Host "Installing Tabula to $TabulaHome..."

New-Item -ItemType Directory -Force -Path $TabulaHome, $BinDir | Out-Null

# Remove legacy root-level runtime layout from earlier installs.
foreach ($legacy in @("boot.py", "templates", "skills", "testing", "distrib")) {
    $legacyPath = Join-Path $TabulaHome $legacy
    if (Test-Path $legacyPath) { Remove-Item -Recurse -Force $legacyPath }
}

Copy-Item (Join-Path $RepoRoot "examples" "boot-cicd.py") -Destination (Join-Path $TabulaHome "boot-cicd.py") -Force

# Shared skill library
$SkillsDest = Join-Path $TabulaHome "skills"
if (Test-Path $SkillsDest) { Remove-Item -Recurse -Force $SkillsDest }
New-Item -ItemType Directory -Force -Path $SkillsDest | Out-Null
Copy-Item (Join-Path $RepoRoot "skills" "_lib") -Destination (Join-Path $SkillsDest "_lib") -Recurse -Force
Get-ChildItem -Path $SkillsDest -Recurse -Directory -Filter "__pycache__" | Remove-Item -Recurse -Force

# Test/dev runtime skills
$TestingDest = Join-Path $TabulaHome "testing"
if (Test-Path $TestingDest) { Remove-Item -Recurse -Force $TestingDest }
Copy-Item (Join-Path $RepoRoot "testing") -Destination $TestingDest -Recurse -Force
Get-ChildItem -Path $TestingDest -Recurse -Directory -Filter "__pycache__" | Remove-Item -Recurse -Force

# Service units
$ServiceDest = Join-Path $TabulaHome "service"
if (Test-Path $ServiceDest) { Remove-Item -Recurse -Force $ServiceDest }
Copy-Item (Join-Path $RepoRoot "service") -Destination $ServiceDest -Recurse -Force

# Python venv with dependencies
if (-not (Test-Path $Venv)) {
    Write-Host "Creating Python venv..."
    python -m venv $Venv
}
$Pip = Join-Path $Venv "Scripts" "pip.exe"
& $Pip install -q --upgrade pip
& $Pip install -q -r (Join-Path $ScriptDir "requirements-dev.txt")
Write-Host "Python dependencies installed"

# Go binary
Write-Host "Building Go binary..."
$BinPath = Join-Path $BinDir "tabula.exe"
$VersionStr = (Get-Content (Join-Path $RepoRoot "VERSION") -Raw).Trim()
try { $CommitStr = (& git -C $RepoRoot rev-parse --short HEAD).Trim() } catch { $CommitStr = "unknown" }
$DateStr = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$LdFlags = "-X main.version=$VersionStr -X main.commit=$CommitStr -X main.date=$DateStr"
Push-Location $RepoRoot
go build -ldflags $LdFlags -o $BinPath ./cmd/tabula/
Pop-Location

# Record installed kernel version for tabula-distro compatibility checks.
Set-Content -Path (Join-Path $TabulaHome "VERSION") -Value $VersionStr -NoNewline

# Launch scripts
foreach ($script in @("tabula-server.ps1", "tabula-cli.ps1", "tabula-api.ps1", "tabula-install-distro.ps1")) {
    Copy-Item (Join-Path $RepoRoot "bin" $script) -Destination (Join-Path $BinDir $script) -Force
}
Copy-Item (Join-Path $RepoRoot "scripts" "install-distro.py") -Destination (Join-Path $BinDir "install-distro.py") -Force

$PythonRuntime = Join-Path $Venv "Scripts" "python.exe"
& $PythonRuntime (Join-Path $BinDir "install-distro.py") --home $TabulaHome (Join-Path $RepoRoot "distrib" "assistant")

# Add to PATH
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -notlike "*$BinDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$BinDir;$UserPath", "User")
    Write-Host "Added $BinDir to user PATH"
} else {
    Write-Host "Already in PATH"
}

# Set TABULA_HOME
$CurrentHome = [Environment]::GetEnvironmentVariable("TABULA_HOME", "User")
if ($CurrentHome -ne $TabulaHome) {
    [Environment]::SetEnvironmentVariable("TABULA_HOME", $TabulaHome, "User")
    Write-Host "Set TABULA_HOME=$TabulaHome"
}

# Apply in current session
$env:TABULA_HOME = $TabulaHome
$env:Path = "$BinDir;$env:Path"
Write-Host "Environment updated for current session"

Write-Host ""
Write-Host "Installed."

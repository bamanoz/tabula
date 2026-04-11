# Install Tabula to ~\.tabula\
# Run: powershell -ExecutionPolicy Bypass -File install.ps1

$ErrorActionPreference = "Stop"

$TabulaHome = if ($env:TABULA_HOME) { $env:TABULA_HOME } else { Join-Path $HOME ".tabula" }
$BinDir = Join-Path $TabulaHome "bin"

Write-Host "Installing Tabula to $TabulaHome..."

New-Item -ItemType Directory -Force -Path $TabulaHome, $BinDir | Out-Null

# Config
Copy-Item "tabula.yaml" -Destination $TabulaHome -Force
Copy-Item "boot.py" -Destination $TabulaHome -Force

# Skills (mirror directory, exclude test/mock skills)
$SkillsDest = Join-Path $TabulaHome "skills"
if (Test-Path $SkillsDest) { Remove-Item -Recurse -Force $SkillsDest }
Copy-Item "skills" -Destination $SkillsDest -Recurse -Force
# Remove mock skills
Remove-Item -Recurse -Force (Join-Path $SkillsDest "driver-mock") -ErrorAction SilentlyContinue
Remove-Item -Recurse -Force (Join-Path $SkillsDest "subagent-mock") -ErrorAction SilentlyContinue
# Clean pycache
Get-ChildItem -Path $SkillsDest -Recurse -Directory -Filter "__pycache__" | Remove-Item -Recurse -Force

# Memory directory (don't overwrite existing data)
New-Item -ItemType Directory -Force -Path (Join-Path $TabulaHome "memory") | Out-Null

# Python venv with dependencies
$Venv = Join-Path $TabulaHome ".venv"
if (-not (Test-Path $Venv)) {
    Write-Host "Creating Python venv..."
    python -m venv $Venv
}
$Pip = Join-Path $Venv "Scripts" "pip.exe"
& $Pip install -q websocket-client pytest
Write-Host "Python dependencies installed"

# Go binary
Write-Host "Building Go binary..."
$BinPath = Join-Path $BinDir "tabula.exe"
go build -o $BinPath ./cmd/tabula/

# Launch scripts
foreach ($script in @("tabula-headless.ps1", "tabula-cli.ps1", "tabula-api.ps1")) {
    Copy-Item (Join-Path "bin" $script) -Destination (Join-Path $BinDir $script) -Force
}

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
Write-Host "Installed. Ready to assist!"

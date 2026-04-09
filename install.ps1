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
& $Pip install -q websocket-client rich prompt_toolkit pytest
Write-Host "Python dependencies installed"

# Go binary
Write-Host "Building Go binary..."
$BinPath = Join-Path $BinDir "tabula.exe"
go build -o $BinPath ./cmd/tabula/

# Launch scripts (PowerShell wrappers)
@"
# Launch Tabula (kernel + interactive CLI)
`$env:TABULA_HOME = if (`$env:TABULA_HOME) { `$env:TABULA_HOME } else { Join-Path `$HOME ".tabula" }
& "`$env:TABULA_HOME\bin\tabula.exe"
"@ | Set-Content (Join-Path $BinDir "tabula-main.ps1")

@"
# Launch Tabula kernel only (headless)
`$env:TABULA_HOME = if (`$env:TABULA_HOME) { `$env:TABULA_HOME } else { Join-Path `$HOME ".tabula" }
`$env:TABULA_HEADLESS = "1"
& "`$env:TABULA_HOME\bin\tabula.exe"
"@ | Set-Content (Join-Path $BinDir "tabula-headless.ps1")

@"
# Launch Tabula API gateway (connects to running kernel)
`$env:TABULA_HOME = if (`$env:TABULA_HOME) { `$env:TABULA_HOME } else { Join-Path `$HOME ".tabula" }
`$Venv = Join-Path `$env:TABULA_HOME ".venv" "Scripts" "python.exe"
`$Port = if (`$env:TABULA_API_PORT) { `$env:TABULA_API_PORT } else { "8090" }
& `$Venv (Join-Path `$env:TABULA_HOME "skills" "gateway-api" "run.py") --port `$Port
"@ | Set-Content (Join-Path $BinDir "tabula-api.ps1")

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
Write-Host "Installed. Run: tabula-main"

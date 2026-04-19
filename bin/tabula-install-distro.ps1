$ErrorActionPreference = "Stop"

if (-not $env:TABULA_HOME) {
    $env:TABULA_HOME = Join-Path $HOME ".tabula"
}

$Python = Join-Path $env:TABULA_HOME ".venv" "Scripts" "python.exe"
if (-not (Test-Path $Python)) {
    $Python = "python"
}

& $Python (Join-Path $env:TABULA_HOME "bin" "install-distro.py") --home $env:TABULA_HOME @args

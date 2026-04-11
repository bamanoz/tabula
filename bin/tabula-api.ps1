# Launch Tabula API gateway (connects to a running kernel).
# Start the kernel first with tabula-headless.
$env:TABULA_HOME = if ($env:TABULA_HOME) { $env:TABULA_HOME } else { Join-Path $HOME ".tabula" }
$VenvPython = Join-Path $env:TABULA_HOME ".venv" "Scripts" "python.exe"

if (-not (Test-Path $VenvPython)) {
    Write-Error "venv not found at $env:TABULA_HOME\.venv — run install.ps1 first."
    exit 1
}

$Port = if ($env:TABULA_API_PORT) { $env:TABULA_API_PORT } else { "8090" }
$Gateway = Join-Path $env:TABULA_HOME "skills" "gateway-api" "run.py"

& $VenvPython $Gateway --port $Port

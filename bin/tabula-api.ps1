# Launch Tabula API gateway (connects to a running kernel).
# Start the kernel first with tabula-server.
$env:TABULA_HOME = if ($env:TABULA_HOME) { $env:TABULA_HOME } else { Join-Path $HOME ".tabula" }

function Load-TabulaEnv {
    $envFile = Join-Path $env:TABULA_HOME ".env"
    if (-not (Test-Path $envFile)) {
        return
    }
    foreach ($line in Get-Content $envFile) {
        $trimmed = $line.Trim()
        if (-not $trimmed -or $trimmed.StartsWith("#")) {
            continue
        }
        $parts = $trimmed -split '=', 2
        if ($parts.Count -ne 2) {
            continue
        }
        $key = $parts[0].Trim()
        $value = $parts[1].Trim()
        if (-not $key) {
            continue
        }
        if (-not (Test-Path "Env:$key")) {
            Set-Item -Path "Env:$key" -Value $value
        }
    }
}

Load-TabulaEnv

$VenvPython = Join-Path $env:TABULA_HOME ".venv" "Scripts" "python.exe"

if (-not (Test-Path $VenvPython)) {
    Write-Error "venv not found at $env:TABULA_HOME\.venv — run install.ps1 first."
    exit 1
}

$Port = if ($env:TABULA_API_PORT) { $env:TABULA_API_PORT } else { "8090" }
$Gateway = Join-Path $env:TABULA_HOME "skills" "gateway-api" "run.py"

& $VenvPython $Gateway --port $Port

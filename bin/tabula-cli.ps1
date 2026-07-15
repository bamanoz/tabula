# Launch Tabula CLI gateway (connects to a running kernel).
# Usage: tabula-cli.ps1 [--resume SESSION_ID]
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

$Venv = if ($env:TABULA_VENV) { $env:TABULA_VENV } else { Join-Path $env:TABULA_HOME ".venv" }
$VenvPython = Join-Path $Venv "Scripts" "python.exe"

if (-not (Test-Path $VenvPython)) {
    Write-Error "venv not found at $Venv — run install.ps1 first."
    exit 1
}

$Gateway = Join-Path $env:TABULA_HOME "apps" "gateway-cli" "run.py"

& $VenvPython $Gateway @args

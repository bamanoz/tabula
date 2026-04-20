# Launch the Tabula kernel with the default local boot script.
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

if (-not $env:TABULA_BOOT) {
    $VenvPython = Join-Path $env:TABULA_HOME ".venv" "Scripts" "python.exe"
    $BootScript = Join-Path $env:TABULA_HOME "boot.py"
    $env:TABULA_BOOT = "`"$VenvPython`" `"$BootScript`""
}
& (Join-Path $env:TABULA_HOME "bin" "tabula.exe") serve @args

# Launch the Tabula kernel and local runtime as separate processes.
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

function Wait-ForPath {
    param(
        [string]$Path,
        [string]$Label,
        [int]$TimeoutSeconds,
        $KernelProcess
    )
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        if (Test-Path $Path) {
            return
        }
        if ($KernelProcess.HasExited) {
            throw "kernel exited before $Label became ready"
        }
        Start-Sleep -Milliseconds 200
    }
    throw "timed out waiting for $Label at $Path"
}

function Runtime-SocketPath {
    param([string]$ConfigPath)
    $line = Get-Content $ConfigPath | Where-Object { $_ -match '^url\s*=\s*"unix://(.+)"\s*$' } | Select-Object -First 1
    if (-not $line) {
        throw "runtime url not found in $ConfigPath"
    }
    return ($Matches[1])
}

function Load-AppEnv {
    if ($env:TABULA_APP_ID) {
        return
    }
    $runtimeConfig = Join-Path $env:TABULA_HOME "config" "runtime.toml"
    if (-not (Test-Path $runtimeConfig)) {
        return
    }
    $tenantIDs = New-Object System.Collections.Generic.List[string]
    $inTenant = $false
    foreach ($line in Get-Content $runtimeConfig) {
        $trimmed = $line.Trim()
        if ($trimmed -eq "[[tenant]]") {
            $inTenant = $true
            continue
        }
        if ($trimmed.StartsWith("[[") -or ($trimmed.StartsWith("[") -and $trimmed -ne "[[tenant]]")) {
            $inTenant = $false
            continue
        }
        if ($inTenant -and $trimmed -match '^id\s*=\s*"([^"]+)"\s*$') {
            $tenantIDs.Add($Matches[1])
        }
    }
    if ($tenantIDs.Count -eq 1) {
        $env:TABULA_APP_ID = $tenantIDs[0]
        if (-not $env:TABULA_TENANT_ID) {
            $env:TABULA_TENANT_ID = $tenantIDs[0]
        }
        if (-not $env:TABULA_TENANT_DIR) {
            $env:TABULA_TENANT_DIR = Join-Path (Join-Path $env:TABULA_HOME "tenants") $tenantIDs[0]
        }
    }
}

Load-TabulaEnv
Load-AppEnv

$binDir = Join-Path $env:TABULA_HOME "bin"
$tabulaBin = Join-Path $binDir "tabula.exe"
$runtimeBin = Join-Path $binDir "tabula-runtime.exe"
$venv = if ($env:TABULA_VENV) { $env:TABULA_VENV } else { Join-Path $env:TABULA_HOME ".venv" }
if (-not $env:TABULA_PATH) {
    $env:TABULA_PATH = "$(Join-Path $venv 'Scripts');$binDir;$env:Path"
}
$env:Path = $env:TABULA_PATH
if ($env:PYTHONPATH) {
    $env:PYTHONPATH = "$(Join-Path $env:TABULA_HOME 'packages' 'python' 'src');$env:PYTHONPATH"
} else {
    $env:PYTHONPATH = "$(Join-Path $env:TABULA_HOME 'packages' 'python' 'src')"
}

$runtimeMode = "external"
$forwardArgs = New-Object System.Collections.Generic.List[string]
for ($i = 0; $i -lt $args.Count; $i++) {
    $arg = [string]$args[$i]
    if ($arg -eq "--runtime-mode") {
        if ($i + 1 -ge $args.Count) {
            throw "--runtime-mode requires a value"
        }
        $runtimeMode = [string]$args[$i + 1]
        $i++
        continue
    }
    if ($arg.StartsWith("--runtime-mode=")) {
        $runtimeMode = $arg.Substring(15)
        continue
    }
    $forwardArgs.Add($arg)
}

if ($runtimeMode -notin @("external", "disabled")) {
    throw "tabula-runner supports only --runtime-mode external|disabled"
}

if ($runtimeMode -eq "disabled") {
    & $tabulaBin serve --runtime-mode disabled @forwardArgs
    exit $LASTEXITCODE
}

$startupTimeout = if ($env:TABULA_RUNNER_STARTUP_TIMEOUT) { [int]$env:TABULA_RUNNER_STARTUP_TIMEOUT } else { 30 }
$runtimeConfig = Join-Path $env:TABULA_HOME "config" "runtime.toml"
$runtimeToken = Join-Path $env:TABULA_HOME "run" "runtime-token"

$kernelArgs = @("serve", "--runtime-mode", "external") + $forwardArgs
$kernel = Start-Process -FilePath $tabulaBin -ArgumentList $kernelArgs -PassThru

try {
    Wait-ForPath -Path $runtimeConfig -Label "runtime config" -TimeoutSeconds $startupTimeout -KernelProcess $kernel
    Wait-ForPath -Path $runtimeToken -Label "runtime token" -TimeoutSeconds $startupTimeout -KernelProcess $kernel
    $runtimeSocket = Runtime-SocketPath -ConfigPath $runtimeConfig
    Wait-ForPath -Path $runtimeSocket -Label "runtime socket" -TimeoutSeconds $startupTimeout -KernelProcess $kernel

    $runtime = Start-Process -FilePath $runtimeBin -ArgumentList @("start", "--config", $runtimeConfig, "--runtime-id", "local") -PassThru
    try {
        while ($true) {
            if ($kernel.HasExited) {
                if (-not $runtime.HasExited) {
                    Stop-Process -Id $runtime.Id -Force -ErrorAction SilentlyContinue
                }
                exit $kernel.ExitCode
            }
            if ($runtime.HasExited) {
                if (-not $kernel.HasExited) {
                    Stop-Process -Id $kernel.Id -Force -ErrorAction SilentlyContinue
                }
                exit $runtime.ExitCode
            }
            Start-Sleep -Milliseconds 200
        }
    }
    finally {
        if (-not $runtime.HasExited) {
            Stop-Process -Id $runtime.Id -Force -ErrorAction SilentlyContinue
        }
    }
}
finally {
    if (-not $kernel.HasExited) {
        Stop-Process -Id $kernel.Id -Force -ErrorAction SilentlyContinue
    }
}

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

Load-TabulaEnv

$binDir = Join-Path $env:TABULA_HOME "bin"
$tabulaBin = Join-Path $binDir "tabula.exe"
$runtimeBin = Join-Path $binDir "tabula-runtime.exe"
if (-not $env:TABULA_PATH) {
    $env:TABULA_PATH = "$(Join-Path $env:TABULA_HOME '.venv' 'Scripts');$binDir;$env:Path"
}
$env:Path = "$binDir;$env:Path"

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

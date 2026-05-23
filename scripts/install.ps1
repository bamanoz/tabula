# Tabula installer — downloads pre-built kernel/runtime binaries from GitHub Releases.
#
# Installs the kernel layer only. After this script finishes, install a
# distro separately:
#
#   tabula-distro install 'git+https://github.com/bamanoz/tabula-distrib.git@main#path=claw'
#   tabula-distro install C:\path\to\local\distro
#
# Usage:
#   irm https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.ps1 | iex
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.ps1))) app run
#   $env:VERSION = "v1.0.0"; irm ... | iex

$ErrorActionPreference = "Stop"

$Repo = "bamanoz/tabula"
$TabulaHome = if ($env:TABULA_HOME) { $env:TABULA_HOME } else { Join-Path $HOME ".tabula" }
$BinDir = Join-Path $TabulaHome "bin"
$Venv = Join-Path $TabulaHome ".venv"
$PostInstallArgs = $args

# ── Helpers ──────────────────────────────────────────────────────

function Info($msg)  { Write-Host "==> $msg" -ForegroundColor Blue }
function Ok($msg)    { Write-Host "  ✓ $msg" -ForegroundColor Green }
function Die($msg)   { Write-Host "error: $msg" -ForegroundColor Red; exit 1 }

# ── Resolve version ─────────────────────────────────────────────

function Resolve-Version {
    if ($env:VERSION) {
        Info "Using version: $env:VERSION"
        return $env:VERSION
    }
    Info "Fetching latest release..."
    try {
        $release = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest" `
            -Headers @{ Accept = "application/vnd.github+json" }
    } catch {
        Die "could not fetch latest release for $Repo; set GITHUB_TOKEN for a private repo, set VERSION=vX.Y.Z, or publish a GitHub release"
    }
    if (-not $release.tag_name) { Die "Could not determine latest version" }
    Info "Latest version: $($release.tag_name)"
    return $release.tag_name
}

# ── Check Python ────────────────────────────────────────────────

function Check-Python {
    $py = $null
    foreach ($candidate in @("python3", "python")) {
        try {
            $null = & $candidate --version 2>&1
            $py = $candidate
            break
        } catch {}
    }
    if (-not $py) { Die "Python 3.11+ is required. Install from https://python.org/downloads/" }

    $ver = & $py -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')"
    $parts = $ver -split '\.'
    $major = [int]$parts[0]; $minor = [int]$parts[1]
    if ($major -lt 3 -or ($major -eq 3 -and $minor -lt 11)) {
        Die "Python 3.11+ required, found $ver"
    }
    Ok "Python $ver"
    return $py
}

function Install-PythonDeps {
    $RequirementsUrl = "https://raw.githubusercontent.com/$Repo/$Version/scripts/requirements-runtime.txt"
    $RequirementsPath = Join-Path $TmpDir "requirements-runtime.txt"

    Info "Installing Python dependencies..."
    $Pip = Join-Path $Venv "Scripts" "pip.exe"
    & $Pip install -q --upgrade pip
    try {
        Invoke-WebRequest $RequirementsUrl -OutFile $RequirementsPath
        & $Pip install -q -r $RequirementsPath
    } catch {
        & $Pip install -q websocket-client prompt_toolkit rich
    }
    Ok "Python dependencies installed"
}

function New-FlatRuntimeSurface {
    param(
        [string]$SourceDir,
        [string]$DestDir,
        [string]$TabulaHome,
        [string[]]$Preserve = @()
    )

    # Legacy helper retained for backwards compatibility; no longer invoked
    # because distros are installed via tabula-distro into $TABULA_HOME/distrib.
    New-Item -ItemType Directory -Force -Path $DestDir | Out-Null
}

function Verify-Launchers {
    foreach ($launcher in @("tabula-runner.ps1", "tabula-cli.ps1")) {
        $path = Join-Path $BinDir $launcher
        if (-not (Test-Path $path)) {
            Die "release payload is missing required launcher: bin/$launcher"
        }
    }
    Ok "Launchers installed"
}

# ── Service install ─────────────────────────────────────────────

function Install-Service {
    $LogDir = Join-Path $TabulaHome "logs"
    New-Item -ItemType Directory -Force -Path $LogDir | Out-Null

    $TaskName = "TabulaKernel"

    # Remove existing task if present
    $existing = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    if ($existing) {
        Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
    }

    $HeadlessScript = Join-Path $BinDir "tabula-runner.ps1"
    $OutLog = Join-Path $LogDir "kernel.out.log"
    $ErrLog = Join-Path $LogDir "kernel.err.log"

    $Action = New-ScheduledTaskAction `
        -Execute "powershell.exe" `
        -Argument "-NoProfile -ExecutionPolicy Bypass -File `"$HeadlessScript`" > `"$OutLog`" 2> `"$ErrLog`"" `
        -WorkingDirectory $TabulaHome

    $Trigger = New-ScheduledTaskTrigger -AtLogOn

    $Settings = New-ScheduledTaskSettingsSet `
        -AllowStartIfOnBatteries `
        -DontStopIfGoingOnBatteries `
        -RestartInterval (New-TimeSpan -Seconds 5) `
        -RestartCount 999 `
        -ExecutionTimeLimit 0

    Register-ScheduledTask `
        -TaskName $TaskName `
        -Action $Action `
        -Trigger $Trigger `
        -Settings $Settings `
        -Description "Tabula kernel" | Out-Null

    Start-ScheduledTask -TaskName $TaskName
    Ok "Kernel service installed (Task Scheduler)"
}

# ── Main ────────────────────────────────────────────────────────

$Version = Resolve-Version
$VerBare = $Version.TrimStart("v")

$BinaryArchive = "tabula_${VerBare}_windows_amd64.zip"
$SkillsArchive = "tabula-skills-${Version}.tar.gz"
$BaseUrl = "https://github.com/$Repo/releases/download/$Version"

$TmpDir = Join-Path ([IO.Path]::GetTempPath()) "tabula-install-$(Get-Random)"
New-Item -ItemType Directory -Force -Path $TmpDir | Out-Null

try {
    # Download
    Info "Downloading binary..."
    try {
        Invoke-WebRequest "$BaseUrl/$BinaryArchive" -OutFile (Join-Path $TmpDir $BinaryArchive)
    } catch {
        Die "could not download $BinaryArchive from $BaseUrl; set VERSION to a published release"
    }

    Info "Downloading skills..."
    try {
        Invoke-WebRequest "$BaseUrl/$SkillsArchive" -OutFile (Join-Path $TmpDir $SkillsArchive)
    } catch {
        Die "could not download $SkillsArchive from $BaseUrl; the release payload is incomplete"
    }

    # Install
    Info "Installing to $TabulaHome..."
    New-Item -ItemType Directory -Force -Path $TabulaHome, $BinDir | Out-Null

    # Remove legacy root-level runtime layout from older installs.
    foreach ($legacy in @("boot.py", "templates", "distrib", "skills", "testing")) {
        $legacyPath = Join-Path $TabulaHome $legacy
        if (Test-Path $legacyPath) { Remove-Item -Recurse -Force $legacyPath }
    }

    # Extract binaries from zip
    Expand-Archive -Path (Join-Path $TmpDir $BinaryArchive) -DestinationPath $TmpDir -Force
    Copy-Item (Join-Path $TmpDir "tabula.exe") -Destination (Join-Path $BinDir "tabula.exe") -Force
    $RuntimeExe = Join-Path $TmpDir "tabula-runtime.exe"
    if (Test-Path $RuntimeExe) {
        Copy-Item $RuntimeExe -Destination (Join-Path $BinDir "tabula-runtime.exe") -Force
    }
    Ok "Binary installed"

    # Back up user config before tar overwrites it
    $UserConfig = Join-Path $TabulaHome "tabula.yaml"
    $ConfigBackup = Join-Path $TmpDir "tabula.yaml.bak"
    $HadConfig = Test-Path $UserConfig
    if ($HadConfig) {
        Copy-Item $UserConfig $ConfigBackup
    }

    # Extract skills tarball
    tar -xzf (Join-Path $TmpDir $SkillsArchive) -C $TabulaHome
    # Record installed kernel version for tabula-distro compatibility checks.
    Set-Content -Path (Join-Path $TabulaHome "VERSION") -Value $VerBare -NoNewline
    $InstalledBinDir = Join-Path $TabulaHome "bin"
    Ok "Skills and config installed"

    # Restore user config if it existed
    if ($HadConfig) {
        Copy-Item $ConfigBackup $UserConfig
    }

    # Python
    $Python = Check-Python

    if (-not (Test-Path $Venv)) {
        Info "Creating Python venv..."
        & $Python -m venv $Venv
    }

    Install-PythonDeps

    # Install tabula-distro from the bundled tools/ directory if available.
    $DistroToolDir = Join-Path $TabulaHome "tools" "tabula-distro"
    if (Test-Path $DistroToolDir) {
        $Pip = Join-Path $Venv "Scripts" "pip.exe"
        & $Pip install -q -e $DistroToolDir
    }
    # Expose installer entrypoints on PATH alongside the rest of the launchers.
    $TabulaInstallSrc = Join-Path $Venv "Scripts" "tabula-install.exe"
    if (Test-Path $TabulaInstallSrc) {
        Copy-Item $TabulaInstallSrc -Destination (Join-Path $BinDir "tabula-install.exe") -Force
    }
    $TabulaDistroSrc = Join-Path $Venv "Scripts" "tabula-distro.exe"
    if (Test-Path $TabulaDistroSrc) {
        Copy-Item $TabulaDistroSrc -Destination (Join-Path $BinDir "tabula-distro.exe") -Force
    }

    # Copy PowerShell launch scripts
    foreach ($script in @("tabula-runner.ps1", "tabula-cli.ps1")) {
        $src = Join-Path $TabulaHome "bin" $script
        if (Test-Path $src) {
            Copy-Item $src -Destination (Join-Path $BinDir $script) -Force
        }
    }
    Verify-Launchers

    # Add to PATH
    $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($UserPath -notlike "*$BinDir*") {
        [Environment]::SetEnvironmentVariable("Path", "$BinDir;$UserPath", "User")
        Ok "Added $BinDir to user PATH"
    } else {
        Ok "Already in PATH"
    }

    # Set TABULA_HOME
    $CurrentHome = [Environment]::GetEnvironmentVariable("TABULA_HOME", "User")
    if ($CurrentHome -ne $TabulaHome) {
        [Environment]::SetEnvironmentVariable("TABULA_HOME", $TabulaHome, "User")
        Ok "Set TABULA_HOME=$TabulaHome"
    }

    $env:TABULA_HOME = $TabulaHome
    $env:Path = "$BinDir;$env:Path"

    if ($PostInstallArgs.Count -ge 1 -and $PostInstallArgs[0] -eq "app") {
        Info "Skipping default kernel service; app command will start/reuse its configured kernel when needed"
    } else {
        Install-Service
    }

    # Env file for API keys
    $EnvFile = Join-Path $TabulaHome ".env"
    if (-not (Test-Path $EnvFile)) {
        Set-Content -Path $EnvFile -Value "ANTHROPIC_API_KEY=`n# OPENAI_API_KEY=`n# TABULA_PROVIDER=anthropic"
    }

    Write-Host ""
    Write-Host "Tabula $Version kernel installed!" -ForegroundColor Green
    Write-Host ""

    if ($PostInstallArgs.Count -gt 0) {
        $TabulaInstall = Join-Path $BinDir "tabula-install.exe"
        Info "Running: tabula-install $($PostInstallArgs -join ' ')"
        & $TabulaInstall @PostInstallArgs
        exit $LASTEXITCODE
    }

    Write-Host "Add your API key to $EnvFile :"
    Write-Host "  echo ANTHROPIC_API_KEY=sk-... >> $EnvFile"
    Write-Host ""
    Write-Host "Install a distro (required before the kernel can do anything useful):"
    Write-Host "  tabula-install distro install 'git+https://github.com/bamanoz/tabula-distrib.git@main#path=claw'"
    Write-Host "  tabula-install distro install C:\path\to\local\distro"
    Write-Host ""
    Write-Host "Or install and run an app manifest in one command:"
    Write-Host "  & ([scriptblock]::Create((irm https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.ps1))) app run"
    Write-Host ""
    Write-Host "Then connect:"
    Write-Host "  tabula-cli"
    Write-Host ""

} finally {
    Remove-Item -Recurse -Force $TmpDir -ErrorAction SilentlyContinue
}

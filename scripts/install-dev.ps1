# Install Tabula kernel + runtime daemon from source.
#
# Installs the local runtime layer:
#   * Go binaries (tabula.exe + tabula-runtime.exe, built from this repo)
#   * launch scripts (tabula-runner.ps1, tabula-cli.ps1)
#   * Python venv with runtime + dev dependencies
#   * tabula-distro installer (editable, from tools/tabula-distro)

$ErrorActionPreference = "Stop"

foreach ($arg in $args) {
    switch ($arg) {
        { $_ -in @("-h", "--help") } {
            Get-Content $MyInvocation.MyCommand.Path | Select-Object -First 12
            exit 0
        }
        default { throw "unknown arg: $arg" }
    }
}

$TabulaHome = if ($env:TABULA_HOME) { $env:TABULA_HOME } else { Join-Path $HOME ".tabula" }
$BinDir = Join-Path $TabulaHome "bin"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot = Split-Path -Parent $ScriptDir
$Arch = if ($env:PROCESSOR_ARCHITECTURE) { $env:PROCESSOR_ARCHITECTURE } else { "unknown" }
$Venv = if ($env:TABULA_VENV) { $env:TABULA_VENV } else { Join-Path $TabulaHome ".venv-Windows-$Arch" }

function Stop-ExistingTabula {
    Write-Host "==> Stopping any running tabula kernel/runtime"
    try {
        Get-CimInstance Win32_Process |
            Where-Object { $_.CommandLine -like "*$TabulaHome*tabula*serve*" -or $_.CommandLine -like "*$TabulaHome*tabula-runtime*start*" } |
            ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
    } catch {
        Write-Host "warning: could not inspect running tabula processes: $_"
    }
}

function Append-EnvLineIfMissing {
    param([string]$Path, [string]$Prefix, [string]$Line)
    if (-not (Test-Path $Path)) {
        Set-Content -Path $Path -Value $Line
        return
    }
    if (-not (Select-String -Path $Path -Pattern "^$([regex]::Escape($Prefix))" -Quiet)) {
        Add-Content -Path $Path -Value $Line
    }
}

function Write-BundlesPth {
    param([string]$Python, [string]$BundlesRoot)
    if (-not (Test-Path $BundlesRoot)) {
        Write-Host "warning: tabula-bundles checkout not found at $BundlesRoot"
        Write-Host "         tabula-install app materializers may require tabula_plugin_sdk; set TABULA_BUNDLES_ROOT to your checkout"
        return
    }
    $code = @'
import site
import sys
from pathlib import Path

root = Path(sys.argv[1]).resolve()
paths = [
    root / "base" / "plugin-sdk" / "sdk" / "python" / "src",
    root / "base" / "skills" / "sdk" / "python" / "src",
    root / "base" / "tool-result-store" / "sdk" / "python" / "src",
    root / "base" / "sessions" / "sdk" / "python" / "src",
    root / "base" / "deferred-tools" / "sdk" / "python" / "src",
    root / "drivers" / "driver" / "sdk" / "python" / "src",
    root / "mempalace" / "mempalace-common" / "sdk" / "python" / "src",
]
existing = [path for path in paths if path.is_dir()]
if not existing:
    raise SystemExit(f"no component-owned Python SDK roots found under {root}")
site_packages = Path(site.getsitepackages()[0])
site_packages.mkdir(parents=True, exist_ok=True)
(site_packages / "tabula-bundles-sdk-roots.pth").write_text("\n".join(str(path) for path in existing) + "\n", encoding="utf-8")
'@
    $code | & $Python - $BundlesRoot
}

Stop-ExistingTabula

Write-Host "==> Installing Tabula kernel/runtime to $TabulaHome"
New-Item -ItemType Directory -Force -Path $TabulaHome, $BinDir, (Join-Path $TabulaHome "config") | Out-Null

$GlobalConfig = Join-Path $TabulaHome "config" "global.toml"
if (-not (Test-Path $GlobalConfig)) {
    Copy-Item (Join-Path $RepoRoot "config" "global.toml") -Destination $GlobalConfig -Force
}

$ServiceSource = Join-Path $RepoRoot "service"
if (Test-Path $ServiceSource) {
    $ServiceDest = Join-Path $TabulaHome "service"
    if (Test-Path $ServiceDest) { Remove-Item -Recurse -Force $ServiceDest }
    Copy-Item $ServiceSource -Destination $ServiceDest -Recurse -Force
}

if ((Test-Path $Venv) -and -not (Test-Path (Join-Path $Venv "Scripts" "pip.exe"))) {
    Write-Host "==> Recreating invalid Python venv"
    Remove-Item -Recurse -Force $Venv
}
if (-not (Test-Path $Venv)) {
    Write-Host "==> Creating Python venv"
    python -m venv $Venv
}
$Python = Join-Path $Venv "Scripts" "python.exe"
$Pip = Join-Path $Venv "Scripts" "pip.exe"
& $Pip install -q --upgrade pip
& $Pip install -q -r (Join-Path $ScriptDir "requirements-dev.txt")
$BundlesRoot = if ($env:TABULA_BUNDLES_ROOT) { $env:TABULA_BUNDLES_ROOT } else { Join-Path (Split-Path -Parent $RepoRoot) "tabula-bundles" }
Write-BundlesPth -Python $Python -BundlesRoot $BundlesRoot
& $Pip install -q -e (Join-Path $RepoRoot "tools" "tabula-distro")
Write-Host "    Python dependencies installed"

$EnvFile = Join-Path $TabulaHome ".env"
Append-EnvLineIfMissing -Path $EnvFile -Prefix "TABULA_VENV=" -Line "TABULA_VENV=$Venv"

Write-Host "==> Building Go binaries"
$VersionStr = (Get-Content (Join-Path $RepoRoot "VERSION") -Raw).Trim()
try { $CommitStr = (& git -C $RepoRoot rev-parse --short HEAD).Trim() } catch { $CommitStr = "unknown" }
$DateStr = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$LdFlags = "-X main.version=$VersionStr -X main.commit=$CommitStr -X main.date=$DateStr"
Push-Location $RepoRoot
try {
    go build -ldflags $LdFlags -o (Join-Path $BinDir "tabula.exe") ./cmd/tabula/
    go build -ldflags $LdFlags -o (Join-Path $BinDir "tabula-runtime.exe") ./cmd/tabula-runtime/
} finally {
    Pop-Location
}

Set-Content -Path (Join-Path $TabulaHome "VERSION") -Value $VersionStr -NoNewline
& (Join-Path $BinDir "tabula.exe") --protocol | Set-Content -Path (Join-Path $TabulaHome "PROTOCOL")

foreach ($script in @("tabula-runner.ps1", "tabula-cli.ps1")) {
    Copy-Item (Join-Path $RepoRoot "bin" $script) -Destination (Join-Path $BinDir $script) -Force
}
foreach ($exe in @("tabula-install.exe", "tabula-distro.exe")) {
    $src = Join-Path $Venv "Scripts" $exe
    if (Test-Path $src) {
        Copy-Item $src -Destination (Join-Path $BinDir $exe) -Force
    }
}

$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -notlike "*$BinDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$BinDir;$UserPath", "User")
    Write-Host "    Added to user PATH"
} else {
    Write-Host "    Already configured in user PATH"
}
$CurrentHome = [Environment]::GetEnvironmentVariable("TABULA_HOME", "User")
if ($CurrentHome -ne $TabulaHome) {
    [Environment]::SetEnvironmentVariable("TABULA_HOME", $TabulaHome, "User")
    Write-Host "    Set TABULA_HOME=$TabulaHome"
}

$env:TABULA_HOME = $TabulaHome
$env:TABULA_VENV = $Venv
$env:TABULA_PATH = "$(Join-Path $Venv 'Scripts');$BinDir;$env:Path"
$env:Path = "$BinDir;$env:Path"

Write-Host ""
Write-Host "Tabula kernel/runtime installed at $TabulaHome."
Write-Host ""
Write-Host "Next: install a distro with tabula-install. Examples:"
Write-Host ""
Write-Host "  tabula-install distro install ../tabula-distrib/claw"
Write-Host "  tabula-install distro install 'git+https://github.com/bamanoz/tabula-distrib.git@main#path=guardian'"
Write-Host ""
Write-Host "Then start the kernel:"
Write-Host ""
Write-Host "  tabula-runner.ps1"

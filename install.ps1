# Tabula installer — downloads pre-built binary and skills from GitHub Releases.
# Usage:
#   irm https://raw.githubusercontent.com/bamanoz/tabula/main/install.ps1 | iex
#   $env:VERSION = "v1.0.0"; irm ... | iex

$ErrorActionPreference = "Stop"

$Repo = "bamanoz/tabula"
$TabulaHome = if ($env:TABULA_HOME) { $env:TABULA_HOME } else { Join-Path $HOME ".tabula" }
$BinDir = Join-Path $TabulaHome "bin"
$Venv = Join-Path $TabulaHome ".venv"

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
    $release = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest" `
        -Headers @{ Accept = "application/vnd.github+json" }
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
    Invoke-WebRequest "$BaseUrl/$BinaryArchive" -OutFile (Join-Path $TmpDir $BinaryArchive)

    Info "Downloading skills..."
    Invoke-WebRequest "$BaseUrl/$SkillsArchive" -OutFile (Join-Path $TmpDir $SkillsArchive)

    # Install
    Info "Installing to $TabulaHome..."
    New-Item -ItemType Directory -Force -Path $TabulaHome, $BinDir, (Join-Path $TabulaHome "memory") | Out-Null

    # Extract binary from zip
    Expand-Archive -Path (Join-Path $TmpDir $BinaryArchive) -DestinationPath $TmpDir -Force
    Copy-Item (Join-Path $TmpDir "tabula.exe") -Destination (Join-Path $BinDir "tabula.exe") -Force
    Ok "Binary installed"

    # Extract skills tarball
    tar -xzf (Join-Path $TmpDir $SkillsArchive) -C $TabulaHome
    Ok "Skills and config installed"

    # Python
    $Python = Check-Python

    if (-not (Test-Path $Venv)) {
        Info "Creating Python venv..."
        & $Python -m venv $Venv
    }

    Info "Installing Python dependencies..."
    $Pip = Join-Path $Venv "Scripts" "pip.exe"
    & $Pip install -q --upgrade pip
    & $Pip install -q websocket-client prompt_toolkit rich
    Ok "Python dependencies installed"

    # Copy PowerShell launch scripts
    foreach ($script in @("tabula-headless.ps1", "tabula-cli.ps1", "tabula-api.ps1")) {
        $src = Join-Path $TabulaHome "bin" $script
        if (Test-Path $src) {
            Copy-Item $src -Destination (Join-Path $BinDir $script) -Force
        }
    }

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

    Write-Host ""
    Write-Host "Tabula $Version installed!" -ForegroundColor Green
    Write-Host ""
    Write-Host '  $env:ANTHROPIC_API_KEY = "sk-..."'
    Write-Host "  tabula-headless    # start kernel"
    Write-Host "  tabula-cli         # connect CLI"
    Write-Host ""

} finally {
    Remove-Item -Recurse -Force $TmpDir -ErrorAction SilentlyContinue
}

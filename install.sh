#!/bin/bash
# Tabula installer — downloads pre-built binary and skills from GitHub Releases.
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/install.sh | bash
#   VERSION=v1.0.0 curl -fsSL ... | bash
set -euo pipefail

REPO="bamanoz/tabula"
TABULA_HOME="${TABULA_HOME:-$HOME/.tabula}"
BIN_DIR="$TABULA_HOME/bin"
VENV="$TABULA_HOME/.venv"

# ── helpers ──────────────────────────────────────────────────────

info() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
ok()   { printf '\033[1;32m  ✓\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

need() {
  command -v "$1" &>/dev/null || die "required tool not found: $1"
}

# ── detect platform ─────────────────────────────────────────────

detect_platform() {
  case "$(uname -s)" in
    Darwin) PLATFORM_OS="darwin" ;;
    Linux)  PLATFORM_OS="linux"  ;;
    *)      die "Unsupported OS: $(uname -s). Windows users: see install.ps1" ;;
  esac

  case "$(uname -m)" in
    x86_64|amd64)  PLATFORM_ARCH="amd64" ;;
    arm64|aarch64) PLATFORM_ARCH="arm64" ;;
    *)             die "Unsupported architecture: $(uname -m)" ;;
  esac
}

# ── resolve version ─────────────────────────────────────────────

resolve_version() {
  if [ -n "${VERSION:-}" ]; then
    info "Using version: $VERSION"
    return
  fi

  info "Fetching latest release..."
  VERSION=$(curl -fsSL \
    -H "Accept: application/vnd.github+json" \
    "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' | head -1 \
    | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')

  [ -n "$VERSION" ] || die "Could not determine latest version"
  info "Latest version: $VERSION"
}

# ── check python ─────────────────────────────────────────────────

check_python() {
  local py=""
  for candidate in python3.13 python3.12 python3.11 python3; do
    if command -v "$candidate" &>/dev/null; then
      py="$candidate"
      break
    fi
  done

  [ -n "$py" ] || die "Python 3.11+ is required. Install from https://python.org/downloads/"

  local major minor
  major=$("$py" -c 'import sys; print(sys.version_info.major)')
  minor=$("$py" -c 'import sys; print(sys.version_info.minor)')

  if [ "$major" -lt 3 ] || { [ "$major" -eq 3 ] && [ "$minor" -lt 11 ]; }; then
    die "Python 3.11+ required, found ${major}.${minor} at $(command -v "$py")"
  fi

  PYTHON_BIN="$py"
  ok "Python ${major}.${minor}"
}

# ── shell config ─────────────────────────────────────────────────

configure_shell() {
  local shell_rc=""
  if [ -n "${ZSH_VERSION:-}" ] || [ -f "$HOME/.zshrc" ]; then
    shell_rc="$HOME/.zshrc"
  elif [ -f "$HOME/.bashrc" ]; then
    shell_rc="$HOME/.bashrc"
  elif [ -f "$HOME/.bash_profile" ]; then
    shell_rc="$HOME/.bash_profile"
  fi

  if [ -n "$shell_rc" ]; then
    if ! grep -qF 'TABULA_HOME' "$shell_rc"; then
      printf '\n# Tabula\nexport TABULA_HOME="%s"\nexport PATH="$TABULA_HOME/bin:$PATH"\n' \
        "$TABULA_HOME" >> "$shell_rc"
      ok "Added to $shell_rc"
    else
      ok "Already in $shell_rc"
    fi
  else
    printf 'Add to your shell rc:\n  export TABULA_HOME="%s"\n  export PATH="$TABULA_HOME/bin:$PATH"\n' \
      "$TABULA_HOME"
  fi

  export TABULA_HOME="$TABULA_HOME"
  export PATH="$BIN_DIR:$PATH"
}

# ── main ─────────────────────────────────────────────────────────

main() {
  need curl
  need tar

  detect_platform
  resolve_version

  local ver_bare="${VERSION#v}"
  local binary_archive="tabula_${ver_bare}_${PLATFORM_OS}_${PLATFORM_ARCH}.tar.gz"
  local skills_archive="tabula-skills-${VERSION}.tar.gz"
  local base_url="https://github.com/${REPO}/releases/download/${VERSION}"

  local tmp
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT

  # Download
  info "Downloading binary..."
  curl -fsSL --progress-bar -o "$tmp/$binary_archive" "$base_url/$binary_archive"

  info "Downloading skills..."
  curl -fsSL --progress-bar -o "$tmp/$skills_archive" "$base_url/$skills_archive"

  # Install
  info "Installing to $TABULA_HOME..."
  mkdir -p "$BIN_DIR" "$TABULA_HOME/memory"

  tar -xzf "$tmp/$binary_archive" -C "$tmp"
  install -m 755 "$tmp/tabula" "$BIN_DIR/tabula"
  if [ "$PLATFORM_OS" = "darwin" ]; then
    xattr -d com.apple.quarantine "$BIN_DIR/tabula" 2>/dev/null || true
  fi
  ok "Binary installed"

  tar -xzf "$tmp/$skills_archive" -C "$TABULA_HOME"
  chmod +x "$BIN_DIR/tabula-headless" "$BIN_DIR/tabula-cli" "$BIN_DIR/tabula-api" 2>/dev/null || true
  ok "Skills and config installed"

  # Python
  check_python

  if [ ! -d "$VENV" ]; then
    info "Creating Python venv..."
    "$PYTHON_BIN" -m venv "$VENV"
  fi

  info "Installing Python dependencies..."
  "$VENV/bin/pip" install -q --upgrade pip
  "$VENV/bin/pip" install -q websocket-client prompt_toolkit rich
  ok "Python dependencies installed"

  # Shell
  configure_shell

  printf '\n\033[1;32mTabula %s installed!\033[0m\n\n' "$VERSION"
  printf '  export ANTHROPIC_API_KEY=sk-...\n'
  printf '  tabula-headless    # start kernel\n'
  printf '  tabula-cli         # connect CLI\n\n'
}

main "$@"

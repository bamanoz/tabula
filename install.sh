#!/bin/bash
# Tabula installer — downloads pre-built binary and skills from GitHub Releases.
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/install.sh | bash
#   VERSION=v1.0.0 curl -fsSL ... | bash
#   BUNDLES=caveman curl -fsSL ... | bash
#   BUNDLES=all curl -fsSL ... | bash
set -euo pipefail

REPO="bamanoz/tabula"
TABULA_HOME="${TABULA_HOME:-$HOME/.tabula}"
BIN_DIR="$TABULA_HOME/bin"
VENV="$TABULA_HOME/.venv"

# Auth header for private repos (optional)
AUTH_HEADER=()
if [ -n "${GITHUB_TOKEN:-}" ]; then
  AUTH_HEADER=(-H "Authorization: token $GITHUB_TOKEN")
fi

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
    "${AUTH_HEADER[@]}" \
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

# ── save PATH ────────────────────────────────────────────────────

save_path_to_env() {
  local env_file="$1"
  # Include venv bin so kernel children can find skill Python
  local full_path="$VENV/bin:$PATH"
  local path_line="TABULA_PATH=$full_path"

  if [ -f "$env_file" ]; then
    # Remove old TABULA_PATH line, then append new one
    local tmp="${env_file}.tmp"
    grep -v '^TABULA_PATH=' "$env_file" > "$tmp" || true
    printf '%s\n' "$path_line" >> "$tmp"
    mv "$tmp" "$env_file"
  else
    printf '%s\n' "$path_line" > "$env_file"
  fi
  ok "Saved login PATH to .env"
}

# ── bundles ──────────────────────────────────────────────────────

install_bundles() {
  local bundles_dir="$TABULA_HOME/bundles"
  local requested="${BUNDLES:-}"

  if [ -z "$requested" ]; then
    # No bundles requested — remove any that were unpacked from tar
    rm -rf "$bundles_dir"
    return
  fi

  if [ "$requested" = "all" ]; then
    ok "All bundles installed"
    return
  fi

  # Keep only requested bundles, remove the rest
  IFS=',' read -ra wanted <<< "$requested"
  for dir in "$bundles_dir"/*/; do
    [ -d "$dir" ] || continue
    local name
    name=$(basename "$dir")
    local keep=false
    for w in "${wanted[@]}"; do
      if [ "$w" = "$name" ]; then
        keep=true
        break
      fi
    done
    if [ "$keep" = false ]; then
      rm -rf "$dir"
    fi
  done

  # Remove bundles dir if empty
  rmdir "$bundles_dir" 2>/dev/null || true

  # List what was installed
  if [ -d "$bundles_dir" ]; then
    local installed=()
    for dir in "$bundles_dir"/*/; do
      [ -d "$dir" ] && installed+=("$(basename "$dir")")
    done
    if [ ${#installed[@]} -gt 0 ]; then
      ok "Bundles installed: ${installed[*]}"
    fi
  fi
}

# ── link bundles into skills/ ────────────────────────────────────

link_bundles() {
  local bundles_dir="$TABULA_HOME/bundles"
  local skills_dir="$TABULA_HOME/skills"

  # Remove stale bundle symlinks from skills/
  for link in "$skills_dir"/*/; do
    [ -L "${link%/}" ] && rm -f "${link%/}"
  done

  # Create symlinks for each installed bundle
  if [ -d "$bundles_dir" ]; then
    for dir in "$bundles_dir"/*/; do
      [ -d "$dir" ] || continue
      local name
      name=$(basename "$dir")
      ln -sfn "../bundles/$name" "$skills_dir/$name"
    done
  fi
}

# ── service install ──────────────────────────────────────────────

install_service() {
  mkdir -p "$TABULA_HOME/logs"

  if [ "$PLATFORM_OS" = "darwin" ]; then
    install_launchd
  else
    install_systemd
  fi
}

install_launchd() {
  local plist_src="$TABULA_HOME/service/com.tabula.kernel.plist"
  local plist_dest="$HOME/Library/LaunchAgents/com.tabula.kernel.plist"

  if [ ! -f "$plist_src" ]; then
    info "Skipping service install (plist template not found)"
    return
  fi

  # Stop existing service if loaded, wait for it to fully unload
  if launchctl print "gui/$(id -u)/com.tabula.kernel" &>/dev/null; then
    launchctl bootout "gui/$(id -u)/com.tabula.kernel" 2>/dev/null || true
    for _ in 1 2 3 4 5; do
      launchctl print "gui/$(id -u)/com.tabula.kernel" &>/dev/null || break
      sleep 1
    done
  fi

  # Replace placeholders and install
  sed "s|__TABULA_HOME__|${TABULA_HOME}|g" "$plist_src" > "$plist_dest"

  launchctl bootstrap "gui/$(id -u)" "$plist_dest"
  ok "Kernel service installed (launchd)"
}

install_systemd() {
  local unit_src="$TABULA_HOME/service/tabula.service"
  local unit_dir="$HOME/.config/systemd/user"
  local unit_dest="$unit_dir/tabula.service"

  if [ ! -f "$unit_src" ]; then
    info "Skipping service install (systemd unit not found)"
    return
  fi

  mkdir -p "$unit_dir"

  # Replace placeholders and install
  sed "s|__TABULA_HOME__|${TABULA_HOME}|g" "$unit_src" > "$unit_dest"

  systemctl --user daemon-reload
  systemctl --user enable --now tabula.service
  ok "Kernel service installed (systemd)"

  # Enable lingering so service runs without active login session
  if command -v loginctl &>/dev/null; then
    loginctl enable-linger "$(whoami)" 2>/dev/null || true
  fi
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
  trap 'rm -rf "${tmp:-}"' EXIT

  # Download
  if [ ${#AUTH_HEADER[@]} -gt 0 ]; then
    # Private repo: download via GitHub API
    local api_url="https://api.github.com/repos/${REPO}/releases/tags/${VERSION}"
    local release_json
    release_json=$(curl -fsSL "${AUTH_HEADER[@]}" -H "Accept: application/vnd.github+json" "$api_url")

    download_asset() {
      local name="$1" dest="$2"
      # Extract asset API URL by finding the name line, then reading the url line before it
      local asset_id
      asset_id=$(printf '%s' "$release_json" | grep -B5 "\"name\": \"${name}\"" | grep '"url":' | tail -1 \
        | sed 's/.*"url": *"\([^"]*\)".*/\1/')
      [ -n "$asset_id" ] || die "Asset $name not found in release"
      info "Downloading $name..."
      curl -fsSL "${AUTH_HEADER[@]}" -H "Accept: application/octet-stream" -L -o "$dest" "$asset_id"
    }

    download_asset "$binary_archive" "$tmp/$binary_archive"
    download_asset "$skills_archive" "$tmp/$skills_archive"
  else
    # Public repo: direct download
    info "Downloading binary..."
    curl -fsSL -L --progress-bar -o "$tmp/$binary_archive" "$base_url/$binary_archive"

    info "Downloading skills..."
    curl -fsSL -L --progress-bar -o "$tmp/$skills_archive" "$base_url/$skills_archive"
  fi

  # Install
  info "Installing to $TABULA_HOME..."
  mkdir -p "$BIN_DIR" "$TABULA_HOME/memory"

  tar -xzf "$tmp/$binary_archive" -C "$tmp"
  install -m 755 "$tmp/tabula" "$BIN_DIR/tabula"
  if [ "$PLATFORM_OS" = "darwin" ]; then
    xattr -d com.apple.quarantine "$BIN_DIR/tabula" 2>/dev/null || true
  fi
  ok "Binary installed"

  # Back up user config before tar overwrites it
  local had_config=false
  if [ -f "$TABULA_HOME/tabula.yaml" ]; then
    cp "$TABULA_HOME/tabula.yaml" "$tmp/tabula.yaml.bak"
    had_config=true
  fi

  tar -xzf "$tmp/$skills_archive" -C "$TABULA_HOME"
  chmod +x "$BIN_DIR/tabula-headless" "$BIN_DIR/tabula-cli" "$BIN_DIR/tabula-api" 2>/dev/null || true
  ok "Skills and config installed"

  # Install bundles (optional) and symlink into skills/
  install_bundles
  link_bundles

  # Restore user config if it existed
  if [ "$had_config" = true ]; then
    cp "$tmp/tabula.yaml.bak" "$TABULA_HOME/tabula.yaml"
  fi

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

  # Service
  install_service

  # Save full login shell PATH for the kernel service
  # (launchd/systemd start with minimal PATH like /usr/bin:/bin)
  local env_file="$TABULA_HOME/.env"
  save_path_to_env "$env_file"

  # Env file for API keys (skills read it at startup, no kernel restart needed)
  if [ ! -f "$env_file" ]; then
    printf '# API keys — loaded by skills at startup.\nANTHROPIC_API_KEY=\n# OPENAI_API_KEY=\n# TABULA_PROVIDER=anthropic\n' >> "$env_file"
    chmod 600 "$env_file"
  fi

  printf '\n\033[1;32mTabula %s installed!\033[0m\n\n' "$VERSION"
  printf 'Add your API key:\n'
  printf '  echo "ANTHROPIC_API_KEY=sk-..." >> %s\n\n' "$env_file"
  if [ -z "${BUNDLES:-}" ]; then
    printf 'Optional bundles (caveman, etc.):\n'
    printf '  BUNDLES=caveman %s\n\n' "$0"
  fi
  printf 'Then connect:\n'
  printf '  tabula-cli\n\n'
}

main "$@"

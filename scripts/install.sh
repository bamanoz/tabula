#!/bin/bash
# Tabula installer — downloads pre-built kernel/runtime binaries from GitHub Releases.
#
# This installs the local runtime layer (tabula, tabula-runtime, launchers, venv, tabula-distro).
# After it finishes, install a distro separately:
#
#   tabula-distro install 'git+https://github.com/bamanoz/tabula-distrib.git@main#path=claw'
#   tabula-distro install /path/to/local/distro
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash -s -- app run
#   VERSION=v1.0.0 curl -fsSL ... | bash
set -euo pipefail

REPO="bamanoz/tabula"
TABULA_HOME="${TABULA_HOME:-$HOME/.tabula}"
BIN_DIR="$TABULA_HOME/bin"
VENV="$TABULA_HOME/.venv"
POST_INSTALL_ARGS=("$@")

# Auth header for private repos (optional)
AUTH_HEADER=()

use_gh() {
  command -v gh &>/dev/null && [ -n "${GH_RELEASE_TOKEN:-}" ]
}

github_token() {
  if command -v gh &>/dev/null; then
    local token
    token="$(env -u GITHUB_TOKEN -u GH_TOKEN gh auth token 2>/dev/null || true)"
    if [ -n "$token" ]; then
      printf '%s' "$token"
      return
    fi
  fi
  if [ -n "${GITHUB_TOKEN:-}" ]; then
    printf '%s' "$GITHUB_TOKEN"
    return
  fi
  if [ -n "${GH_TOKEN:-}" ]; then
    printf '%s' "$GH_TOKEN"
  fi
}

configure_auth_header() {
  local token
  token="$(github_token)"
  if [ -n "$token" ]; then
    GH_RELEASE_TOKEN="$token"
    GITHUB_TOKEN="$token"
    GH_TOKEN="$token"
    export GH_RELEASE_TOKEN
    export GITHUB_TOKEN
    export GH_TOKEN
    AUTH_HEADER=(-H "Authorization: Bearer $token")
  fi
}

configure_auth_header

# ── helpers ──────────────────────────────────────────────────────

info() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
ok()   { printf '\033[1;32m  ✓\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

need() {
  command -v "$1" &>/dev/null || die "required tool not found: $1"
}

curl_retry() {
  curl --retry 5 --retry-delay 2 --connect-timeout 30 "$@"
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
  if use_gh; then
    VERSION=$(gh release view --repo "${REPO}" --json tagName --jq .tagName 2>/dev/null || true)
    if [ -n "$VERSION" ]; then
      info "Latest version: $VERSION"
      return
    fi
  fi

  local curl_args=(curl_retry -fsSL)
  if [ ${#AUTH_HEADER[@]} -gt 0 ]; then
    curl_args+=("${AUTH_HEADER[@]}")
  fi
  curl_args+=(-H "Accept: application/vnd.github+json" "https://api.github.com/repos/${REPO}/releases/latest")
  local release_json
  if ! release_json=$("${curl_args[@]}"); then
    die "could not fetch latest release for ${REPO}; set GITHUB_TOKEN for a private repo, set VERSION=vX.Y.Z, or publish a GitHub release"
  fi
  VERSION=$(printf '%s' "$release_json" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')

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

install_python_deps() {
  local requirements_url="https://raw.githubusercontent.com/${REPO}/${VERSION}/scripts/requirements-runtime.txt"

  info "Installing Python dependencies..."
  "$VENV/bin/pip" install -q --upgrade pip
  local curl_args=(curl_retry -fsSL -o "$tmp/requirements-runtime.txt")
  if [ ${#AUTH_HEADER[@]} -gt 0 ]; then
    curl_args+=("${AUTH_HEADER[@]}")
  fi
  curl_args+=("$requirements_url")
  if "${curl_args[@]}"; then
    "$VENV/bin/pip" install -q -r "$tmp/requirements-runtime.txt"
  else
    "$VENV/bin/pip" install -q websocket-client prompt_toolkit rich
  fi
  ok "Python dependencies installed"
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

# ── service install ──────────────────────────────────────────────

verify_launchers() {
  for launcher in tabula-runner tabula-cli; do
    local path="$BIN_DIR/$launcher"
    [ -f "$path" ] || die "release payload is missing required launcher: bin/$launcher"
    chmod +x "$path" || die "could not mark launcher executable: $path"
  done
  ok "Launchers installed"
}

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

  # Replace placeholders and install plist
  sed "s|__TABULA_HOME__|${TABULA_HOME}|g" "$plist_src" > "$plist_dest"

  local domain="gui/$(id -u)"
  local label="com.tabula.kernel"

  if launchctl print "$domain/$label" &>/dev/null; then
    # Already loaded — unload, reload with new plist, then force-start
    launchctl bootout "$domain/$label" 2>/dev/null || true
    for _ in 1 2 3 4 5; do
      launchctl print "$domain/$label" &>/dev/null || break
      sleep 1
    done
  fi

  launchctl bootstrap "$domain" "$plist_dest"
  launchctl kickstart -k "$domain/$label" 2>/dev/null || true
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
  systemctl --user restart tabula.service
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
  if use_gh; then
    info "Downloading assets with gh..."
    gh release download "$VERSION" --repo "${REPO}" --pattern "$binary_archive" --dir "$tmp" --clobber >/dev/null || \
      die "could not download $binary_archive from GitHub release $VERSION"
    gh release download "$VERSION" --repo "${REPO}" --pattern "$skills_archive" --dir "$tmp" --clobber >/dev/null || \
      die "could not download $skills_archive from GitHub release $VERSION"
  elif [ ${#AUTH_HEADER[@]} -gt 0 ]; then
    # Private repo: download via GitHub API
    local api_url="https://api.github.com/repos/${REPO}/releases/tags/${VERSION}"
    local release_json
    local curl_args=(curl_retry -fsSL)
    if [ ${#AUTH_HEADER[@]} -gt 0 ]; then
      curl_args+=("${AUTH_HEADER[@]}")
    fi
    curl_args+=(-H "Accept: application/vnd.github+json" "$api_url")
    if ! release_json=$("${curl_args[@]}"); then
      die "could not fetch release ${VERSION} for ${REPO}; check GITHUB_TOKEN or publish the release"
    fi

    download_asset() {
      local name="$1" dest="$2"
      # Use python3 to reliably extract asset URL from JSON
      local asset_url
      asset_url=$(printf '%s' "$release_json" | python3 -c "
import sys, json
data = json.load(sys.stdin)
for a in data.get('assets', []):
    if a['name'] == '$name':
        print(a['url'])
        break
")
      [ -n "$asset_url" ] || die "Asset $name not found in release"
      info "Downloading $name..."
      local curl_args=(curl_retry -fsSL)
      if [ ${#AUTH_HEADER[@]} -gt 0 ]; then
        curl_args+=("${AUTH_HEADER[@]}")
      fi
      curl_args+=(-H "Accept: application/octet-stream" -L -o "$dest" "$asset_url")
      "${curl_args[@]}"
    }

    download_asset "$binary_archive" "$tmp/$binary_archive"
    download_asset "$skills_archive" "$tmp/$skills_archive"
  else
    # Public repo: direct download
    info "Downloading binary..."
    curl_retry -fsSL -L --progress-bar -o "$tmp/$binary_archive" "$base_url/$binary_archive" || \
      die "could not download $binary_archive from $base_url; set VERSION to a published release"

    info "Downloading skills..."
    curl_retry -fsSL -L --progress-bar -o "$tmp/$skills_archive" "$base_url/$skills_archive" || \
      die "could not download $skills_archive from $base_url; the release payload is incomplete"
  fi

  # Install
  info "Installing to $TABULA_HOME..."
  mkdir -p "$BIN_DIR"

  # Remove legacy root-level runtime layout from older installs.
  rm -rf \
    "$TABULA_HOME/templates" \
    "$TABULA_HOME/distrib" \
    "$TABULA_HOME/skills" \
    "$TABULA_HOME/testing"

  tar -xzf "$tmp/$binary_archive" -C "$tmp"
  [ -x "$tmp/tabula-runtime" ] || die "release archive is missing executable tabula-runtime sidecar"
  install -m 755 "$tmp/tabula" "$BIN_DIR/tabula"
  install -m 755 "$tmp/tabula-runtime" "$BIN_DIR/tabula-runtime"
  if [ "$PLATFORM_OS" = "darwin" ]; then
    xattr -d com.apple.quarantine "$BIN_DIR/tabula" 2>/dev/null || true
    xattr -d com.apple.quarantine "$BIN_DIR/tabula-runtime" 2>/dev/null || true
  fi
  ok "Binaries installed"

  local skills_payload="$tmp/skills-payload"
  mkdir -p "$skills_payload"
  tar -xzf "$tmp/$skills_archive" -C "$skills_payload"
  if [ -f "$skills_payload/config/global.toml" ]; then
    mkdir -p "$skills_payload/config"
    mv "$skills_payload/config/global.toml" "$skills_payload/config/global.toml.example"
  fi
  (cd "$skills_payload" && tar -cf - .) | tar -xf - -C "$TABULA_HOME"
  if [ ! -f "$TABULA_HOME/config/global.toml" ] && [ -f "$TABULA_HOME/config/global.toml.example" ]; then
    cp "$TABULA_HOME/config/global.toml.example" "$TABULA_HOME/config/global.toml"
  fi
  # Record installed Tabula version for tabula-distro compatibility checks.
  printf '%s\n' "${VERSION#v}" > "$TABULA_HOME/VERSION"
  # Record the supported runtime plugin compatibility range so the distro tool
  # can enforce `requires.protocol_version` offline.
  "$BIN_DIR/tabula" --protocol > "$TABULA_HOME/PROTOCOL" 2>/dev/null || \
    printf '{"plugin_protocol_min": 1, "plugin_protocol_max": 1}\n' > "$TABULA_HOME/PROTOCOL"
  verify_launchers
  ok "Skills and config installed"

  # Python
  check_python

  if [ ! -d "$VENV" ]; then
    info "Creating Python venv..."
    "$PYTHON_BIN" -m venv "$VENV"
  fi

  install_python_deps
  if [ -d "$TABULA_HOME/tools/tabula-distro" ]; then
    "$VENV/bin/pip" install -q -e "$TABULA_HOME/tools/tabula-distro"
  fi
  # Expose installer entrypoints on PATH alongside the rest of the launchers.
  ln -sf "$VENV/bin/tabula-install" "$BIN_DIR/tabula-install" 2>/dev/null || true
  ln -sf "$VENV/bin/tabula-distro" "$BIN_DIR/tabula-distro" 2>/dev/null || true

  # Shell
  configure_shell

  if [ ${#POST_INSTALL_ARGS[@]} -ge 1 ] && [ "${POST_INSTALL_ARGS[0]}" = "app" ]; then
    info "Skipping default kernel service; app command will start/reuse its configured kernel when needed"
  else
    # Service
    install_service
  fi

  # Save full login shell PATH for the kernel service
  # (launchd/systemd start with minimal PATH like /usr/bin:/bin)
  local env_file="$TABULA_HOME/.env"
  save_path_to_env "$env_file"

  # Env file for API keys (plugins read it at startup, no kernel restart needed)
  if ! grep -q '^ANTHROPIC_API_KEY=' "$env_file" 2>/dev/null; then
    printf '\n# API keys — loaded by plugins at startup.\nANTHROPIC_API_KEY=\n# OPENAI_API_KEY=\n# TABULA_PROVIDER=anthropic\n' >> "$env_file"
    chmod 600 "$env_file"
  fi

  printf '\n\033[1;32mTabula %s kernel/runtime installed!\033[0m\n\n' "$VERSION"

  if [ ${#POST_INSTALL_ARGS[@]} -gt 0 ]; then
    info "Running: tabula-install ${POST_INSTALL_ARGS[*]}"
    exec "$BIN_DIR/tabula-install" --home "$TABULA_HOME" "${POST_INSTALL_ARGS[@]}"
  fi

  printf 'Add your API key:\n'
  printf '  echo "ANTHROPIC_API_KEY=sk-..." >> %s\n\n' "$env_file"
  printf 'Install a distro (this is required before the kernel can do anything useful):\n'
  printf '  tabula-install distro install '\''git+https://github.com/bamanoz/tabula-distrib.git@main#path=claw'\''\n'
  printf '  tabula-install distro install /path/to/local/distro\n\n'
  printf 'Or install and run an app manifest in one command:\n'
  printf '  curl -fsSL https://raw.githubusercontent.com/bamanoz/tabula/main/scripts/install.sh | bash -s -- app run\n\n'
  printf 'Then connect:\n'
  printf '  tabula-cli\n\n'
}

main "$@"

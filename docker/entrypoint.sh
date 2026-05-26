#!/usr/bin/env bash
set -euo pipefail

TABULA_WORKSPACE="${TABULA_WORKSPACE:-/workspace}"
TABULA_HOME="${TABULA_HOME:-$TABULA_WORKSPACE/.tabula}"
TABULA_APP_MANIFEST="${TABULA_APP_MANIFEST:-$TABULA_WORKSPACE/tabula.app.toml}"
TABULA_UID="${TABULA_UID:-1000}"
TABULA_GID="${TABULA_GID:-1000}"

if [ "$(id -u)" = "0" ]; then
  if [ "$(id -g tabula)" != "$TABULA_GID" ] && ! getent group "$TABULA_GID" >/dev/null; then
    groupmod -g "$TABULA_GID" tabula
  fi
  if [ "$(id -u tabula)" != "$TABULA_UID" ]; then
    usermod -u "$TABULA_UID" -g "$TABULA_GID" tabula
  elif [ "$(id -g tabula)" != "$TABULA_GID" ]; then
    usermod -g "$TABULA_GID" tabula
  fi
  mkdir -p "$TABULA_HOME" "$TABULA_WORKSPACE" /tmp/tabula-home
  chown tabula:tabula "$TABULA_HOME" "$TABULA_WORKSPACE" /tmp/tabula-home 2>/dev/null || true
  exec gosu tabula "$0" "$@"
fi

export TABULA_HOME TABULA_WORKSPACE TABULA_APP_MANIFEST
export PATH="$TABULA_HOME/bin:$TABULA_HOME/.venv/bin:$PATH"
export HOME="/tmp/tabula-home"
export GIT_SSH_COMMAND="${GIT_SSH_COMMAND:-ssh -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=/tmp/tabula-known-hosts}"
mkdir -p "$HOME"

configure_git_auth() {
  if [ -n "${GITHUB_TOKEN:-}" ]; then
    git config --global url."https://x-access-token:${GITHUB_TOKEN}@github.com/".insteadOf "https://github.com/" || true
    return
  fi
  if [ -d /host-ssh ]; then
    mkdir -p "$HOME/.ssh"
    ln -sf /host-ssh/config "$HOME/.ssh/config" 2>/dev/null || true
    ln -sf /host-ssh/id_rsa "$HOME/.ssh/id_rsa" 2>/dev/null || true
    ln -sf /host-ssh/id_rsa.pub "$HOME/.ssh/id_rsa.pub" 2>/dev/null || true
  fi
  git config --global url."ssh://git@github.com/".insteadOf "https://github.com/" || true
}

app_id_from_manifest() {
  "$TABULA_HOME/.venv/bin/python3" - "$TABULA_APP_MANIFEST" <<'PY'
import sys, tomllib
from pathlib import Path
with Path(sys.argv[1]).open('rb') as f:
    data = tomllib.load(f)
print((data.get('application') or {}).get('id') or 'default')
PY
}

install_tabula() {
  echo "==> Installing Tabula Docker runtime to $TABULA_HOME"
  mkdir -p "$TABULA_HOME/bin" "$TABULA_HOME/config" "$TABULA_HOME/service"
  if [ ! -f "$TABULA_HOME/config/global.toml" ]; then
    cp /opt/src/tabula/config/global.toml "$TABULA_HOME/config/global.toml"
  fi
  cp -R /opt/src/tabula/service/. "$TABULA_HOME/service/" 2>/dev/null || true
  if [ -d "$TABULA_HOME/.venv" ] && { ! "$TABULA_HOME/.venv/bin/python" -c 'import sys; print(sys.version)' >/dev/null 2>&1 || ! "$TABULA_HOME/.venv/bin/pip" --version >/dev/null 2>&1; }; then
    rm -rf "$TABULA_HOME/.venv"
  fi
  if [ ! -d "$TABULA_HOME/.venv" ]; then
    python -m venv "$TABULA_HOME/.venv"
  fi
  "$TABULA_HOME/.venv/bin/pip" install -q --upgrade pip
  "$TABULA_HOME/.venv/bin/pip" install -q --upgrade setuptools wheel
  "$TABULA_HOME/.venv/bin/pip" install -q -r /opt/src/tabula/scripts/requirements-dev.txt
  rm -rf /tmp/tabula-distro-src
  cp -R /opt/src/tabula/tools/tabula-distro /tmp/tabula-distro-src
  "$TABULA_HOME/.venv/bin/pip" install -q --no-build-isolation /tmp/tabula-distro-src
  cp /usr/local/bin/tabula "$TABULA_HOME/bin/tabula"
  cp /usr/local/bin/tabula-runtime "$TABULA_HOME/bin/tabula-runtime"
  cp /opt/src/tabula/bin/tabula-runner "$TABULA_HOME/bin/tabula-runner"
  cp /opt/src/tabula/bin/tabula-cli "$TABULA_HOME/bin/tabula-cli"
  chmod +x "$TABULA_HOME/bin/tabula" "$TABULA_HOME/bin/tabula-runtime" "$TABULA_HOME/bin/tabula-runner" "$TABULA_HOME/bin/tabula-cli"
  ln -sf "$TABULA_HOME/.venv/bin/tabula-install" "$TABULA_HOME/bin/tabula-install"
  ln -sf "$TABULA_HOME/.venv/bin/tabula-distro" "$TABULA_HOME/bin/tabula-distro"
  cat /opt/src/tabula/VERSION > "$TABULA_HOME/VERSION"
  "$TABULA_HOME/bin/tabula" --protocol > "$TABULA_HOME/PROTOCOL"
}

needs_install() {
  if [ "${TABULA_DOCKER_REINSTALL:-0}" = "1" ]; then
    return 0
  fi
  if [ ! -x "$TABULA_HOME/bin/tabula-install" ] || [ ! -x "$TABULA_HOME/bin/tabula-runner" ]; then
    return 0
  fi
  "$TABULA_HOME/bin/tabula-runner" --version >/dev/null 2>&1 || return 0
  return 1
}

ensure_user_files() {
  mkdir -p "$TABULA_HOME/config"
  if [ ! -f "$TABULA_HOME/secrets.json" ]; then
    printf '{}\n' > "$TABULA_HOME/secrets.json"
    chmod 600 "$TABULA_HOME/secrets.json" 2>/dev/null || true
  fi
  if [ ! -f "$TABULA_HOME/config/global.toml" ]; then
    cp /opt/src/tabula/config/global.toml "$TABULA_HOME/config/global.toml"
  fi
}

write_docker_gateway_config() {
  local app_id
  app_id="$(app_id_from_manifest)"
  local cfg_dir="$TABULA_HOME/tenants/$app_id/config/plugins/gateway-web"
  mkdir -p "$cfg_dir"
  cat > "$cfg_dir/config.toml" <<'EOF'
host = "0.0.0.0"
port = 8765
allow_remote = true
open_browser = false
allowed_origins = ["*"]
EOF
}

write_docker_runtime_config() {
  local app_id runtime_dir
  app_id="$(app_id_from_manifest)"
  runtime_dir="/tmp/tabula-runtime-$app_id"
  mkdir -p "$runtime_dir" "$TABULA_HOME/config"
  export TABULA_RUNTIME_SOCKET_PATH="$runtime_dir/runtime.sock"
  cat > "$TABULA_HOME/config/runtime.toml" <<EOF
plugin_dirs = []
skill_dirs = []

[[tenant]]
id = "$app_id"
plugin_dirs = ["$TABULA_HOME/tenants/$app_id/plugins"]
skill_dirs = ["$TABULA_HOME/tenants/$app_id/skills"]

[[kernel]]
id = "main"
url = "unix://$runtime_dir/runtime.sock"
token_file = "$TABULA_HOME/run/runtime-token"
tenants = ["$app_id"]
EOF
  export TABULA_PRESERVE_RUNTIME_CONFIG=1
}

prepare_runtime() {
  configure_git_auth
  if [ ! -f "$TABULA_APP_MANIFEST" ]; then
    echo "error: app manifest not found: $TABULA_APP_MANIFEST" >&2
    echo "mount a repository with tabula.app.toml at $TABULA_WORKSPACE or set TABULA_APP_MANIFEST" >&2
    exit 2
  fi
  mkdir -p "$TABULA_HOME" "$TABULA_WORKSPACE"
  if needs_install; then
    install_tabula
  fi
  ensure_user_files
  "$TABULA_HOME/bin/tabula-install" app prepare "$TABULA_APP_MANIFEST" --update
  write_docker_runtime_config
  write_docker_gateway_config
  git config --global --add safe.directory "$TABULA_WORKSPACE" || true
}

case "${1:-run}" in
  run)
    prepare_runtime
    exec "$TABULA_HOME/bin/tabula-runner"
    ;;
  prepare)
    prepare_runtime
    ;;
  shell)
    prepare_runtime
    exec bash
    ;;
  *)
    exec "$@"
    ;;
esac

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
export TABULA_VENV="${TABULA_VENV:-$TABULA_HOME/.venv}"
if [ ! -x "$TABULA_VENV/bin/python3" ]; then
  TABULA_VENV="$TABULA_HOME/.venv"
fi
if [ ! -x "$TABULA_VENV/bin/python3" ] && [ -x /opt/tabula-venv/bin/python3 ]; then
  TABULA_VENV="/opt/tabula-venv"
fi
export PATH="$TABULA_HOME/bin:$TABULA_VENV/bin:$PATH"
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
    for item in /host-ssh/*; do
      [ -e "$item" ] || continue
      ln -sf "$item" "$HOME/.ssh/$(basename "$item")" 2>/dev/null || true
    done
  fi
  git config --global url."ssh://git@github.com/".insteadOf "https://github.com/" || true
}

app_id_from_manifest() {
  "$TABULA_VENV/bin/python3" - "$TABULA_APP_MANIFEST" <<'PY'
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
  if [ -d "$TABULA_VENV" ] && { ! "$TABULA_VENV/bin/python" -c 'import sys; print(sys.version)' >/dev/null 2>&1 || ! "$TABULA_VENV/bin/pip" --version >/dev/null 2>&1; }; then
    rm -rf "$TABULA_VENV"
  fi
  if [ ! -d "$TABULA_VENV" ]; then
    if [ -d /opt/tabula-venv ]; then
      cp -a /opt/tabula-venv "$TABULA_VENV"
    else
      python -m venv "$TABULA_VENV"
      "$TABULA_VENV/bin/pip" install -q --upgrade pip
      "$TABULA_VENV/bin/pip" install -q --upgrade setuptools wheel
      "$TABULA_VENV/bin/pip" install -q -r /opt/src/tabula/scripts/requirements-dev.txt
      rm -rf /tmp/tabula-distro-src
      cp -R /opt/src/tabula/tools/tabula-distro /tmp/tabula-distro-src
      "$TABULA_VENV/bin/pip" install -q --no-build-isolation /tmp/tabula-distro-src
    fi
  fi
  cp /usr/local/bin/tabula "$TABULA_HOME/bin/tabula"
  cp /usr/local/bin/tabula-runtime "$TABULA_HOME/bin/tabula-runtime"
  cp /opt/src/tabula/bin/tabula-runner "$TABULA_HOME/bin/tabula-runner"
  cp /opt/src/tabula/bin/tabula-cli "$TABULA_HOME/bin/tabula-cli"
  chmod +x "$TABULA_HOME/bin/tabula" "$TABULA_HOME/bin/tabula-runtime" "$TABULA_HOME/bin/tabula-runner" "$TABULA_HOME/bin/tabula-cli"
  ln -sf "$TABULA_VENV/bin/tabula-install" "$TABULA_HOME/bin/tabula-install"
  ln -sf "$TABULA_VENV/bin/tabula-distro" "$TABULA_HOME/bin/tabula-distro"
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
  "$TABULA_HOME/bin/tabula" --version >/dev/null 2>&1 || return 0
  "$TABULA_HOME/bin/tabula-runtime" --version >/dev/null 2>&1 || return 0
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

configure_docker_runtime_socket() {
  local app_id runtime_dir
  app_id="$(app_id_from_manifest)"
  runtime_dir="/tmp/tabula-runtime-$app_id"
  mkdir -p "$runtime_dir"
  export TABULA_RUNTIME_SOCKET_PATH="$runtime_dir/runtime.sock"
}

write_docker_runtime_config() {
  local runtime_config
  mkdir -p "$TABULA_HOME/config"
  runtime_config="$TABULA_HOME/config/runtime.toml"
  "$TABULA_VENV/bin/python3" - "$runtime_config" "$TABULA_RUNTIME_SOCKET_PATH" <<'PY'
import sys
from pathlib import Path

from tabula_distro import toml_io

path = Path(sys.argv[1])
socket_path = sys.argv[2]
doc = toml_io.load(path)
kernels = doc.get("kernel")
if not kernels:
    raise SystemExit(f"runtime config has no [[kernel]] entry: {path}")

for kernel in kernels:
    if str(kernel.get("id", "")).strip() == "main":
        kernel["url"] = "unix://" + socket_path
        break
else:
    raise SystemExit(f"runtime config has no main kernel entry: {path}")

toml_io.dump(path, doc)
PY
  export TABULA_PRESERVE_RUNTIME_CONFIG=1
}

export_app_env() {
  local app_id
  app_id="$(app_id_from_manifest)"
  export TABULA_APP_ID="$app_id"
  export TABULA_TENANT_ID="$app_id"
  export TABULA_TENANT_DIR="$TABULA_HOME/tenants/$app_id"
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
  configure_docker_runtime_socket
  "$TABULA_HOME/bin/tabula-install" app install "$TABULA_APP_MANIFEST" --workspace "$TABULA_WORKSPACE" --update
  write_docker_runtime_config
  write_docker_gateway_config
  export_app_env
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

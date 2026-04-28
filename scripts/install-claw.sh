#!/bin/bash
# Convenience wrapper: install the Tabula kernel, then install the claw
# distro via tabula-distro.
#
# Prefers a sibling ../tabula-distrib/claw checkout; otherwise falls back
# to the public git+ source.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TABULA_HOME="${TABULA_HOME:-$HOME/.tabula}"

save_env_value() {
  local key="$1"
  local value="$2"
  local env_file="$TABULA_HOME/.env"
  [ -n "$value" ] || return 0
  mkdir -p "$TABULA_HOME"
  if [ -f "$env_file" ]; then
    local tmp="${env_file}.tmp"
    grep -v "^${key}=" "$env_file" > "$tmp" || true
    printf '%s=%s\n' "$key" "$value" >> "$tmp"
    mv "$tmp" "$env_file"
  else
    printf '%s=%s\n' "$key" "$value" > "$env_file"
  fi
}

wait_for_kernel() {
  local url="${TABULA_URL:-ws://localhost:8089/ws}"
  "$TABULA_HOME/.venv/bin/python3" - "$url" <<'PY'
import json
import sys
import time

import websocket

url = sys.argv[1]
deadline = time.time() + 20
last = None
while time.time() < deadline:
    try:
        ws = websocket.create_connection(url, timeout=1)
        ws.send(json.dumps({"type": "connect", "name": "install-claw-ready", "sends": [], "receives": [], "version": 1}))
        msg = json.loads(ws.recv())
        ws.close()
        if msg.get("type") == "connected":
            sys.exit(0)
        last = RuntimeError(str(msg))
    except Exception as exc:
        last = exc
        time.sleep(0.5)
print(f"kernel did not become ready at {url}: {last}", file=sys.stderr)
sys.exit(1)
PY
}

install_service() {
  mkdir -p "$TABULA_HOME/logs"
  case "$(uname -s)" in
    Darwin)
      local plist_src="$TABULA_HOME/service/com.tabula.kernel.plist"
      local plist_dest="$HOME/Library/LaunchAgents/com.tabula.kernel.plist"
      if [ ! -f "$plist_src" ]; then
        echo "warning: plist template not found; skipping service install" >&2
        return 0
      fi
      sed "s|__TABULA_HOME__|${TABULA_HOME}|g" "$plist_src" > "$plist_dest"
      local domain="gui/$(id -u)"
      local label="com.tabula.kernel"
      if launchctl print "$domain/$label" >/dev/null 2>&1; then
        launchctl bootout "$domain/$label" 2>/dev/null || true
        for _ in 1 2 3 4 5; do
          launchctl print "$domain/$label" >/dev/null 2>&1 || break
          sleep 1
        done
      fi
      launchctl bootstrap "$domain" "$plist_dest"
      launchctl kickstart -k "$domain/$label" 2>/dev/null || true
      echo "==> Kernel service installed (launchd)"
      ;;
    Linux)
      local unit_src="$TABULA_HOME/service/tabula.service"
      local unit_dir="$HOME/.config/systemd/user"
      local unit_dest="$unit_dir/tabula.service"
      if [ ! -f "$unit_src" ]; then
        echo "warning: systemd unit template not found; skipping service install" >&2
        return 0
      fi
      mkdir -p "$unit_dir"
      sed "s|__TABULA_HOME__|${TABULA_HOME}|g" "$unit_src" > "$unit_dest"
      systemctl --user daemon-reload
      systemctl --user enable --now tabula.service
      systemctl --user restart tabula.service
      if command -v loginctl >/dev/null 2>&1; then
        loginctl enable-linger "$(whoami)" 2>/dev/null || true
      fi
      echo "==> Kernel service installed (systemd)"
      ;;
    *)
      echo "warning: unsupported service platform; start with tabula-server" >&2
      ;;
  esac
}

# 1) Kernel
bash "$SCRIPT_DIR/install-dev.sh"

# 2) Distro
if [ -d "$REPO_ROOT/../tabula-distrib/claw" ]; then
  DISTRO_SOURCE="$(cd "$REPO_ROOT/../tabula-distrib/claw" && pwd)"
else
  DISTRO_SOURCE='git+https://github.com/bamanoz/tabula-distrib.git@main#path=claw'
fi

echo "==> Installing claw distro from $DISTRO_SOURCE"
"$TABULA_HOME/.venv/bin/tabula-distro" --home "$TABULA_HOME" install "$DISTRO_SOURCE"

# launchd/systemd do not inherit inline env assignments from this script.
# Persist the provider if the caller supplied it for this install invocation.
save_env_value TABULA_PROVIDER "${TABULA_PROVIDER:-}"
save_env_value TABULA_URL "${TABULA_URL:-}"

echo "==> Installing/restarting kernel service"
install_service
echo "==> Waiting for kernel to accept connections"
wait_for_kernel

cat <<EOF

Claw distro installed.

Run the CLI gateway with:

  tabula-claw

Kernel logs:

  $TABULA_HOME/logs/kernel.out.log
  $TABULA_HOME/logs/kernel.err.log

EOF

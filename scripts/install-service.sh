#!/bin/sh
set -eu

SERVICE_NAME="tabula-kernel"
LAUNCHD_LABEL="ai.tabula.kernel"
MODE="user"
DRY_RUN="0"
ACTION="install"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --system) MODE="system" ;;
    --user) MODE="user" ;;
    --dry-run) DRY_RUN="1" ;;
    --render) ACTION="render" ;;
    -h|--help)
      echo "usage: install-service.sh [--user|--system] [--dry-run|--render]"
      exit 0
      ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
  shift
done

TABULA_HOME="${TABULA_HOME:-$HOME/.tabula}"
TABULA_BIN="${TABULA_BIN:-$TABULA_HOME/bin/tabula}"
LOG_PATH="$TABULA_HOME/logs/kernel.log"

escape_xml() {
  printf '%s' "$1" | sed 's/&/\&amp;/g; s/</\&lt;/g; s/>/\&gt;/g; s/"/\&quot;/g'
}

render_launchd() {
  cat <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$LAUNCHD_LABEL</string>
  <key>ProgramArguments</key>
  <array>
    <string>$(escape_xml "$TABULA_BIN")</string>
    <string>serve</string>
    <string>--runtime-mode</string>
    <string>managed</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>TABULA_HOME</key>
    <string>$(escape_xml "$TABULA_HOME")</string>
  </dict>
  <key>WorkingDirectory</key>
  <string>$(escape_xml "$TABULA_HOME")</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>StandardOutPath</key>
  <string>$(escape_xml "$LOG_PATH")</string>
  <key>StandardErrorPath</key>
  <string>$(escape_xml "$LOG_PATH")</string>
</dict>
</plist>
EOF
}

render_systemd() {
  cat <<EOF
[Unit]
Description=Tabula kernel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$TABULA_HOME
Environment=TABULA_HOME=$TABULA_HOME
ExecStart=$TABULA_BIN serve --runtime-mode managed
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=default.target
EOF
}

platform="$(uname -s)"
if [ "$platform" = "Darwin" ]; then
  unit_path="$HOME/Library/LaunchAgents/$LAUNCHD_LABEL.plist"
  render_cmd="render_launchd"
elif [ "$platform" = "Linux" ]; then
  render_cmd="render_systemd"
  if [ "$MODE" = "system" ]; then
    if [ "$(id -u)" -ne 0 ]; then
      echo "--system mode requires root; rerun as root or use --user" >&2
      exit 1
    fi
    unit_path="/etc/systemd/system/$SERVICE_NAME.service"
  else
    unit_path="$HOME/.config/systemd/user/$SERVICE_NAME.service"
  fi
else
  echo "unsupported OS: $platform" >&2
  exit 1
fi

if [ "$ACTION" = "render" ]; then
  $render_cmd
  exit 0
fi

if [ "$DRY_RUN" = "1" ]; then
  echo "$unit_path"
  $render_cmd
  exit 0
fi

mkdir -p "$TABULA_HOME/logs" "$(dirname "$unit_path")"
$render_cmd > "$unit_path"

if [ "$platform" = "Darwin" ]; then
  launchctl unload "$unit_path" >/dev/null 2>&1 || true
  launchctl load "$unit_path"
else
  if [ "$MODE" = "system" ]; then
    systemctl daemon-reload
    systemctl enable --now "$SERVICE_NAME.service"
  else
    systemctl --user daemon-reload
    systemctl --user enable --now "$SERVICE_NAME.service"
  fi
fi

deadline=$(( $(date +%s) + 30 ))
while [ "$(date +%s)" -le "$deadline" ]; do
  if "$TABULA_BIN" status --json 2>/dev/null | grep -q '"running": true'; then
    echo "Tabula service installed: $unit_path"
    echo "Logs: $LOG_PATH"
    exit 0
  fi
  sleep 1
done

echo "service installed but kernel did not report running within 30s" >&2
echo "Unit: $unit_path" >&2
echo "Logs: $LOG_PATH" >&2
exit 1

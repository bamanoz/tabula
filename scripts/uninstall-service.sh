#!/bin/sh
set -eu

SERVICE_NAME="tabula-kernel"
LAUNCHD_LABEL="ai.tabula.kernel"
MODE="user"
DRY_RUN="0"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --system) MODE="system" ;;
    --user) MODE="user" ;;
    --dry-run) DRY_RUN="1" ;;
    -h|--help)
      echo "usage: uninstall-service.sh [--user|--system] [--dry-run]"
      exit 0
      ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
  shift
done

platform="$(uname -s)"
if [ "$platform" = "Darwin" ]; then
  unit_path="$HOME/Library/LaunchAgents/$LAUNCHD_LABEL.plist"
elif [ "$platform" = "Linux" ]; then
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

if [ "$DRY_RUN" = "1" ]; then
  echo "$unit_path"
  exit 0
fi

if [ "$platform" = "Darwin" ]; then
  if [ -f "$unit_path" ]; then
    launchctl unload "$unit_path" >/dev/null 2>&1 || true
    rm -f "$unit_path"
  fi
else
  if [ "$MODE" = "system" ]; then
    systemctl disable --now "$SERVICE_NAME.service" >/dev/null 2>&1 || true
    rm -f "$unit_path"
    systemctl daemon-reload
  else
    systemctl --user disable --now "$SERVICE_NAME.service" >/dev/null 2>&1 || true
    rm -f "$unit_path"
    systemctl --user daemon-reload
  fi
fi

echo "Tabula service uninstalled: $unit_path"

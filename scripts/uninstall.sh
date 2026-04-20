#!/bin/bash
# Tabula uninstaller — removes service, binary, skills, and optionally user data.
# Usage: bash uninstall.sh [--all]
#   --all  also removes data/ (memory palace etc.), IDENTITY.md, SOUL.md, USER.md, AGENTS.md
set -e

TABULA_HOME="${TABULA_HOME:-$HOME/.tabula}"

info() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
ok()   { printf '\033[1;32m  ✓\033[0m %s\n' "$*"; }

remove_all=false
if [ "${1:-}" = "--all" ]; then
  remove_all=true
fi

# ── Stop and remove service ─────────────────────────────────────

case "$(uname -s)" in
  Darwin)
    if launchctl print "gui/$(id -u)/com.tabula.kernel" &>/dev/null; then
      launchctl bootout "gui/$(id -u)/com.tabula.kernel" 2>/dev/null || true
      ok "Service stopped (launchd)"
    fi
    rm -f "$HOME/Library/LaunchAgents/com.tabula.kernel.plist"
    ok "Service removed (launchd)"
    ;;
  Linux)
    if systemctl --user is-active tabula.service &>/dev/null; then
      systemctl --user stop tabula.service
      ok "Service stopped (systemd)"
    fi
    systemctl --user disable tabula.service 2>/dev/null || true
    rm -f "$HOME/.config/systemd/user/tabula.service"
    systemctl --user daemon-reload 2>/dev/null || true
    ok "Service removed (systemd)"
    ;;
esac

# ── Remove installed files ──────────────────────────────────────

if [ -d "$TABULA_HOME" ]; then
  info "Removing $TABULA_HOME..."

  # Always remove: runtime layout, bin, distrib, skills, bundles, testing,
  # service, venv, logs, runtime state/data caches, top-level boot wrappers.
  rm -rf \
    "$TABULA_HOME/bin" \
    "$TABULA_HOME/distrib" \
    "$TABULA_HOME/skills" \
    "$TABULA_HOME/templates" \
    "$TABULA_HOME/testing" \
    "$TABULA_HOME/bundles" \
    "$TABULA_HOME/service" \
    "$TABULA_HOME/state" \
    "$TABULA_HOME/run" \
    "$TABULA_HOME/.venv" \
    "$TABULA_HOME/logs" \
    "$TABULA_HOME/boot.py" \
    "$TABULA_HOME/boot-cicd.py"

  if [ "$remove_all" = true ]; then
    rm -rf "$TABULA_HOME"
    ok "Removed $TABULA_HOME (including user data)"
  else
    ok "Removed installed files (kept data/, config/, secrets.json, IDENTITY.md, etc.)"
    printf '  To remove everything: rm -rf %s\n' "$TABULA_HOME"
  fi
fi

# ── Clean up shell rc ───────────────────────────────────────────

for rc in "$HOME/.zshrc" "$HOME/.bashrc" "$HOME/.bash_profile"; do
  if [ -f "$rc" ] && grep -qF 'TABULA_HOME' "$rc"; then
    sed -i.bak '/# Tabula/d;/TABULA_HOME/d' "$rc"
    rm -f "${rc}.bak"
    ok "Cleaned $rc"
  fi
done

printf '\n\033[1;32mTabula uninstalled.\033[0m\n'

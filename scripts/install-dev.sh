#!/bin/bash
# Install Tabula from source into ~/.tabula/.
#
# Usage:
#   bash scripts/install-dev.sh                     # install assistant distro
#   bash scripts/install-dev.sh --distro guardian   # install another distro
#
# The selected distro is activated via install-distro.py, which manages all
# symlink fan-out under ~/.tabula/{boot.py,templates,skills}. After the distro
# is installed, an optional distro-specific post-install hook
# (distrib/<name>/install.sh) is executed if present.
set -euo pipefail

DISTRO="assistant"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --distro) DISTRO="$2"; shift 2 ;;
    --distro=*) DISTRO="${1#*=}"; shift ;;
    -h|--help)
      sed -n '2,8p' "$0"
      exit 0
      ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

TABULA_HOME="${TABULA_HOME:-$HOME/.tabula}"
BIN_DIR="$TABULA_HOME/bin"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
VENV="$TABULA_HOME/.venv"
DISTRO_SRC="$REPO_ROOT/distrib/$DISTRO"

if [ ! -d "$DISTRO_SRC" ]; then
  echo "error: distro $DISTRO not found at $DISTRO_SRC" >&2
  exit 1
fi

echo "==> Stopping any running tabula kernel"
pkill -f "$TABULA_HOME/bin/tabula serve" 2>/dev/null || true
sleep 0.3

echo "==> Installing Tabula to $TABULA_HOME (distro: $DISTRO)"
mkdir -p "$TABULA_HOME" "$BIN_DIR"

# Wipe legacy root-level runtime layout from older installs. install-distro.py
# rebuilds the symlink fan-out from distrib/active/ on every run.
rm -rf \
  "$TABULA_HOME/boot.py" \
  "$TABULA_HOME/templates" \
  "$TABULA_HOME/skills" \
  "$TABULA_HOME/testing" \
  "$TABULA_HOME/distrib"

cp "$REPO_ROOT/examples/boot-cicd.py" "$TABULA_HOME/"

# Shared skill library (preserved by install-distro.py during distro swaps)
mkdir -p "$TABULA_HOME/skills"
rsync -a --delete --exclude '__pycache__' --exclude '*.pyc' \
  "$REPO_ROOT/skills/lib/" "$TABULA_HOME/skills/lib/"

# Test/dev runtime skills
mkdir -p "$TABULA_HOME/testing"
rsync -a --delete --exclude '__pycache__' --exclude '*.pyc' \
  "$REPO_ROOT/testing/skills/" "$TABULA_HOME/testing/skills/"

# Global config (don't overwrite user edits)
mkdir -p "$TABULA_HOME/config"
if [ ! -f "$TABULA_HOME/config/global.toml" ]; then
  cp "$REPO_ROOT/config/global.toml" "$TABULA_HOME/config/global.toml"
fi

# Service units
rsync -a --delete "$REPO_ROOT/service/" "$TABULA_HOME/service/"

# Python venv with dependencies
if [ ! -d "$VENV" ]; then
  echo "==> Creating Python venv"
  python3 -m venv "$VENV"
fi
"$VENV/bin/pip" install -q --upgrade pip
"$VENV/bin/pip" install -q -r "$SCRIPT_DIR/requirements-dev.txt"
echo "    Python dependencies installed"

# Go binary
echo "==> Building Go binary"
( cd "$REPO_ROOT" && go build -o "$BIN_DIR/tabula" ./cmd/tabula/ )
if [ "$(uname)" = "Darwin" ]; then
  codesign --force --sign - "$BIN_DIR/tabula" 2>/dev/null || true
fi

# Launch scripts
for script in tabula-server tabula-api tabula-cli tabula-install-distro; do
  cp "$REPO_ROOT/bin/$script" "$BIN_DIR/$script"
  chmod +x "$BIN_DIR/$script"
done
cp "$REPO_ROOT/scripts/install-distro.py" "$BIN_DIR/install-distro.py"

# Install + activate the chosen distro
echo "==> Installing distro: $DISTRO"
"$VENV/bin/python3" "$BIN_DIR/install-distro.py" --home "$TABULA_HOME" "$DISTRO_SRC"

# Optional distro-specific post-install hook (e.g. guardian builds a sandbox image).
POST_INSTALL="$DISTRO_SRC/install.sh"
if [ -f "$POST_INSTALL" ]; then
  echo "==> Running post-install hook: $DISTRO"
  TABULA_HOME="$TABULA_HOME" REPO_ROOT="$REPO_ROOT" bash "$POST_INSTALL"
fi

# PATH config
SHELL_RC=""
if [ -n "${ZSH_VERSION:-}" ] || [ -f "$HOME/.zshrc" ]; then
  SHELL_RC="$HOME/.zshrc"
elif [ -f "$HOME/.bashrc" ]; then
  SHELL_RC="$HOME/.bashrc"
elif [ -f "$HOME/.bash_profile" ]; then
  SHELL_RC="$HOME/.bash_profile"
fi

PATH_LINE="export PATH=\"$TABULA_HOME/bin:\$PATH\""
HOME_LINE="export TABULA_HOME=\"$TABULA_HOME\""
TABULA_PATH_VALUE="$TABULA_HOME/.venv/bin:$TABULA_HOME/bin:$PATH"
TABULA_PATH_LINE="export TABULA_PATH=\"$TABULA_PATH_VALUE\""

if [ -n "$SHELL_RC" ]; then
  if ! grep -qF 'TABULA_HOME' "$SHELL_RC"; then
    {
      echo ""
      echo "# Tabula"
      echo "$HOME_LINE"
      echo "$PATH_LINE"
      echo "$TABULA_PATH_LINE"
    } >> "$SHELL_RC"
    echo "    Added to $SHELL_RC"
  else
    echo "    Already configured in $SHELL_RC"
  fi
  export TABULA_HOME="$TABULA_HOME"
  export TABULA_PATH="$TABULA_PATH_VALUE"
  export PATH="$TABULA_HOME/bin:$PATH"
else
  echo "Could not detect shell rc file. Add manually:"
  printf '  %s\n  %s\n  %s\n' "$HOME_LINE" "$PATH_LINE" "$TABULA_PATH_LINE"
fi

echo
echo "Installed. Active distro: $DISTRO"
echo "  Active link: $TABULA_HOME/distrib/active -> $(readlink "$TABULA_HOME/distrib/active" 2>/dev/null || echo '?')"

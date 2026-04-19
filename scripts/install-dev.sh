#!/bin/bash
# Install Tabula from source into ~/.tabula/.
set -euo pipefail

TABULA_HOME="${TABULA_HOME:-$HOME/.tabula}"
BIN_DIR="$TABULA_HOME/bin"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
VENV="$TABULA_HOME/.venv"

link_runtime_surface() {
  local src_dir="$1"
  local dst_dir="$2"
  shift 2
  local preserve=("$@")
  mkdir -p "$dst_dir"
  for existing in "$dst_dir"/*; do
    [ -e "$existing" ] || continue
    local keep=false
    for name in "${preserve[@]}"; do
      if [ "$(basename "$existing")" = "$name" ]; then
        keep=true
        break
      fi
    done
    [ "$keep" = true ] && continue
    rm -rf "$existing"
  done
  if [ -d "$src_dir" ]; then
    for entry in "$src_dir"/*; do
      [ -e "$entry" ] || continue
      ln -sfn "../${entry#"$TABULA_HOME/"}" "$dst_dir/$(basename "$entry")"
    done
  fi
}

echo "Installing Tabula to $TABULA_HOME..."

mkdir -p "$TABULA_HOME" "$BIN_DIR"

# Remove legacy root-level runtime layout from earlier installs.
rm -rf \
  "$TABULA_HOME/boot.py" \
  "$TABULA_HOME/templates" \
  "$TABULA_HOME/skills" \
  "$TABULA_HOME/testing" \
  "$TABULA_HOME/distrib"

cp "$REPO_ROOT/examples/boot-cicd.py" "$TABULA_HOME/"

# Shared skill library
mkdir -p "$TABULA_HOME/skills"
rsync -a --delete \
  --exclude '__pycache__' \
  --exclude '*.pyc' \
  "$REPO_ROOT/skills/lib/" "$TABULA_HOME/skills/lib/"

# Test/dev runtime skills
mkdir -p "$TABULA_HOME/testing"
rsync -a --delete \
  --exclude '__pycache__' \
  --exclude '*.pyc' \
  "$REPO_ROOT/testing/skills/" "$TABULA_HOME/testing/skills/"

# Global config
mkdir -p "$TABULA_HOME/config"
if [ ! -f "$TABULA_HOME/config/global.toml" ]; then
  cp "$REPO_ROOT/config/global.toml" "$TABULA_HOME/config/global.toml"
fi

# Service units
rsync -a --delete "$REPO_ROOT/service/" "$TABULA_HOME/service/"

# Memory directory (don't overwrite existing data)
mkdir -p "$TABULA_HOME/memory"

# Python venv with dependencies
if [ ! -d "$VENV" ]; then
  echo "Creating Python venv..."
  python3 -m venv "$VENV"
fi
"$VENV/bin/pip" install -q --upgrade pip
"$VENV/bin/pip" install -q -r "$SCRIPT_DIR/requirements-dev.txt"
echo "Python dependencies installed"

# Go binary
echo "Building Go binary..."
(
  cd "$REPO_ROOT"
  go build -o "$BIN_DIR/tabula" ./cmd/tabula/
)
if [ "$(uname)" = "Darwin" ]; then
  codesign --force --sign - "$BIN_DIR/tabula" 2>/dev/null || true
fi

# Launch scripts
for script in tabula-server tabula-api tabula-cli tabula-install-distro; do
  cp "$REPO_ROOT/bin/$script" "$BIN_DIR/$script"
  chmod +x "$BIN_DIR/$script"
done
cp "$REPO_ROOT/scripts/install-distro.py" "$BIN_DIR/install-distro.py"

"$VENV/bin/python3" "$BIN_DIR/install-distro.py" --home "$TABULA_HOME" "$REPO_ROOT/distrib/assistant"

# Add to PATH
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
    echo "" >> "$SHELL_RC"
    echo "# Tabula" >> "$SHELL_RC"
    echo "$HOME_LINE" >> "$SHELL_RC"
    echo "$PATH_LINE" >> "$SHELL_RC"
    echo "$TABULA_PATH_LINE" >> "$SHELL_RC"
    echo "Added to $SHELL_RC"
  else
    echo "Already configured in $SHELL_RC"
  fi

  # Apply in current shell
  export TABULA_HOME="$TABULA_HOME"
  export TABULA_PATH="$TABULA_PATH_VALUE"
  export PATH="$TABULA_HOME/bin:$PATH"
  echo "Environment updated for current session"
else
  echo "Could not detect shell rc file. Add manually:"
  echo "  $HOME_LINE"
  echo "  $PATH_LINE"
  echo "  $TABULA_PATH_LINE"
fi

echo ""
echo "Installed."

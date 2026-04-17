#!/bin/bash
# Install Tabula from source into ~/.tabula/.
set -euo pipefail

TABULA_HOME="${TABULA_HOME:-$HOME/.tabula}"
BIN_DIR="$TABULA_HOME/bin"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
VENV="$TABULA_HOME/.venv"

echo "Installing Tabula to $TABULA_HOME..."

mkdir -p "$TABULA_HOME" "$BIN_DIR"

# Boot scripts
cp "$REPO_ROOT/boot.py" "$TABULA_HOME/"
cp "$REPO_ROOT/examples/boot-cicd.py" "$TABULA_HOME/"

# Templates
rsync -a --delete "$REPO_ROOT/templates/" "$TABULA_HOME/templates/"

# Service units
rsync -a --delete "$REPO_ROOT/service/" "$TABULA_HOME/service/"

# Skills
rsync -a --delete \
  --exclude '__pycache__' \
  --exclude '*.pyc' \
  --exclude '.venv' \
  --exclude 'driver-mock' \
  --exclude 'subagent-mock' \
  "$REPO_ROOT/skills/" "$TABULA_HOME/skills/"

# Bundles (optional thematic skill collections)
# BUNDLES=all for everything, BUNDLES=caveman,foo for specific ones, empty = skip
BUNDLES="${BUNDLES:-}"
if [ -n "$BUNDLES" ]; then
  if [ "$BUNDLES" = "all" ]; then
    rsync -a --delete \
      --exclude '__pycache__' \
      --exclude '*.pyc' \
      "$REPO_ROOT/bundles/" "$TABULA_HOME/bundles/"
    echo "All bundles installed"
  else
    mkdir -p "$TABULA_HOME/bundles"
    IFS=',' read -ra wanted <<< "$BUNDLES"
    for name in "${wanted[@]}"; do
      if [ -d "$REPO_ROOT/bundles/$name" ]; then
        rsync -a --delete \
          --exclude '__pycache__' \
          --exclude '*.pyc' \
          "$REPO_ROOT/bundles/$name/" "$TABULA_HOME/bundles/$name/"
        echo "Bundle installed: $name"
      else
        echo "warning: bundle '$name' not found, skipping"
      fi
    done
  fi
fi

# Symlink bundle skills into skills/ flat (remove stale symlinks first)
for link in "$TABULA_HOME/skills"/*/; do
  [ -L "${link%/}" ] && rm -f "${link%/}"
done
if [ -d "$TABULA_HOME/bundles" ]; then
  for bundle in "$TABULA_HOME/bundles"/*/; do
    [ -d "$bundle" ] || continue
    bundle_name=$(basename "$bundle")
    for skill in "$bundle"/*/; do
      [ -d "$skill" ] || continue
      skill_name=$(basename "$skill")
      ln -sfn "../bundles/$bundle_name/$skill_name" "$TABULA_HOME/skills/$skill_name"
    done
  done
fi

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
for script in tabula-server tabula-api tabula-cli; do
  cp "$REPO_ROOT/bin/$script" "$BIN_DIR/$script"
  chmod +x "$BIN_DIR/$script"
done

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

if [ -n "$SHELL_RC" ]; then
  if ! grep -qF 'TABULA_HOME' "$SHELL_RC"; then
    echo "" >> "$SHELL_RC"
    echo "# Tabula" >> "$SHELL_RC"
    echo "$HOME_LINE" >> "$SHELL_RC"
    echo "$PATH_LINE" >> "$SHELL_RC"
    echo "Added to $SHELL_RC"
  else
    echo "Already configured in $SHELL_RC"
  fi

  # Apply in current shell
  export TABULA_HOME="$TABULA_HOME"
  export PATH="$TABULA_HOME/bin:$PATH"
  echo "Environment updated for current session"
else
  echo "Could not detect shell rc file. Add manually:"
  echo "  $HOME_LINE"
  echo "  $PATH_LINE"
fi

echo ""
echo "Installed. Ready to assist!"

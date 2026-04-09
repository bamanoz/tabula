#!/bin/bash
# Install Tabula to ~/.tabula/
set -e

TABULA_HOME="${TABULA_HOME:-$HOME/.tabula}"
BIN_DIR="$TABULA_HOME/bin"

echo "Installing Tabula to $TABULA_HOME..."

mkdir -p "$TABULA_HOME" "$BIN_DIR"

# Config
cp tabula.yaml "$TABULA_HOME/"
cp boot.py "$TABULA_HOME/"

# Skills
rsync -a --delete \
  --exclude '__pycache__' \
  --exclude '*.pyc' \
  --exclude '.venv' \
  --exclude 'driver-mock' \
  --exclude 'subagent-mock' \
  skills/ "$TABULA_HOME/skills/"

# Memory directory (don't overwrite existing data)
mkdir -p "$TABULA_HOME/memory"

# Python venv with dependencies
VENV="$TABULA_HOME/.venv"
if [ ! -d "$VENV" ]; then
  echo "Creating Python venv..."
  python3 -m venv "$VENV"
fi
"$VENV/bin/pip" install -q websocket-client rich prompt_toolkit pytest
echo "Python dependencies installed"

# Go binary
echo "Building Go binary..."
go build -o "$BIN_DIR/tabula" ./cmd/tabula/
if [ "$(uname)" = "Darwin" ]; then
  codesign --force --sign - "$BIN_DIR/tabula" 2>/dev/null || true
fi

# CLI launcher script
cp bin/tabula-cli "$BIN_DIR/tabula-cli"
chmod +x "$BIN_DIR/tabula-cli"

# Add to PATH
SHELL_RC=""
if [ -n "$ZSH_VERSION" ] || [ -f "$HOME/.zshrc" ]; then
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
echo "Installed. Run: tabula"

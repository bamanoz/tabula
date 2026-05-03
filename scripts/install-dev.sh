#!/bin/bash
# Install Tabula kernel + runtime daemon from source.
#
# Installs the local runtime layer:
#   * Go binaries (`tabula` + `tabula-runtime`, built from this repo)
#   * launch scripts (tabula-server, tabula-cli, tabula-api, tabula-install-distro)
#   * Python venv with runtime + dev dependencies
#   * tabula-distro installer (editable, from tools/tabula-distro)
#   * service unit templates
#
# After this script finishes, install a distro separately:
#
#   tabula-distro install <path-or-uri>
#
# Examples:
#   tabula-distro install ../tabula-distrib/claw
#   tabula-distro install local:/abs/path/to/distro
#   tabula-distro install 'git+https://github.com/bamanoz/tabula-distrib.git@main#path=guardian'
set -euo pipefail

while [ "$#" -gt 0 ]; do
  case "$1" in
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

TABULA_HOME="${TABULA_HOME:-$HOME/.tabula}"
BIN_DIR="$TABULA_HOME/bin"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
VENV="$TABULA_HOME/.venv"

echo "==> Stopping any running tabula kernel/runtime"
pkill -f "$TABULA_HOME/bin/tabula serve" 2>/dev/null || true
pkill -f "$TABULA_HOME/bin/tabula-runtime start" 2>/dev/null || true
sleep 0.3

echo "==> Installing Tabula kernel to $TABULA_HOME"
mkdir -p "$TABULA_HOME" "$BIN_DIR"

cp "$REPO_ROOT/examples/boot-cicd.py" "$TABULA_HOME/"

# Global config (don't overwrite user edits)
mkdir -p "$TABULA_HOME/config"
if [ ! -f "$TABULA_HOME/config/global.toml" ]; then
  cp "$REPO_ROOT/config/global.toml" "$TABULA_HOME/config/global.toml"
fi

# Service unit templates
rsync -a --delete "$REPO_ROOT/service/" "$TABULA_HOME/service/"

# Python venv with dependencies
if [ ! -d "$VENV" ]; then
  echo "==> Creating Python venv"
  python3 -m venv "$VENV"
fi
"$VENV/bin/pip" install -q --upgrade pip
"$VENV/bin/pip" install -q -r "$SCRIPT_DIR/requirements-dev.txt"
"$VENV/bin/pip" install -q -e "$REPO_ROOT/tools/tabula-distro"
echo "    Python dependencies installed"

# Go binaries
echo "==> Building Go binaries"
VERSION_STR="$(cat "$REPO_ROOT/VERSION")"
COMMIT_STR="$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
DATE_STR="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-X main.version=$VERSION_STR -X main.commit=$COMMIT_STR -X main.date=$DATE_STR"
( cd "$REPO_ROOT" && go build -ldflags "$LDFLAGS" -o "$BIN_DIR/tabula" ./cmd/tabula/ )
( cd "$REPO_ROOT" && go build -ldflags "$LDFLAGS" -o "$BIN_DIR/tabula-runtime" ./cmd/tabula-runtime/ )
# Record installed kernel version for tabula-distro compatibility checks.
echo "$VERSION_STR" > "$TABULA_HOME/VERSION"
# Record kernel's supported plugin protocol version range so the distro tool
# can enforce `requires.protocol_version` on plugin manifests offline.
"$BIN_DIR/tabula" --protocol > "$TABULA_HOME/PROTOCOL"
if [ "$(uname)" = "Darwin" ]; then
  codesign --force --sign - "$BIN_DIR/tabula" 2>/dev/null || true
  codesign --force --sign - "$BIN_DIR/tabula-runtime" 2>/dev/null || true
fi

# Launch scripts
for script in tabula-server tabula-api tabula-cli tabula-install-distro tabula-coder tabula-claw; do
  cp "$REPO_ROOT/bin/$script" "$BIN_DIR/$script"
  chmod +x "$BIN_DIR/$script"
done
cp "$REPO_ROOT/scripts/install-distro.py" "$BIN_DIR/install-distro.py"

# Symlink tabula-distro from venv into bin/ so it's on PATH alongside the rest.
ln -sf "$VENV/bin/tabula-distro" "$BIN_DIR/tabula-distro"

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

cat <<EOF

Tabula kernel installed at $TABULA_HOME.

Next: install a distro with tabula-distro. Examples:

  tabula-distro install ../tabula-distrib/claw
  tabula-distro install 'git+https://github.com/bamanoz/tabula-distrib.git@main#path=guardian'

Then start the kernel:

  tabula-server

EOF

#!/bin/bash
# Install Tabula kernel + runtime library from source, then materialize a distro
# via ``tabula-distro install``.
#
# Usage:
#   bash scripts/install-dev.sh                                  # familiar from ../tabula-distrib
#   bash scripts/install-dev.sh --distro guardian                # named distro from --distrib-root
#   bash scripts/install-dev.sh --distro /abs/path/to/distro     # local absolute path
#   bash scripts/install-dev.sh --distro ./relative/distro       # local relative path
#   bash scripts/install-dev.sh --distro local:/abs/path         # explicit local: URI
#   bash scripts/install-dev.sh --distro 'git+https://github.com/bamanoz/tabula-distrib.git@main#path=guardian'
#   bash scripts/install-dev.sh --distrib-root ~/src/tabula-distrib --distro familiar
#
# --distro accepts:
#   * a name        -> resolved against --distrib-root (default: ../tabula-distrib)
#   * an absolute or ./../ relative path to a local distro directory
#   * a 'local:<path>' URI passed through to tabula-distro
#   * a 'git+<url>@<ref>#path=<subpath>' URI passed through to tabula-distro
set -euo pipefail

DISTRO="familiar"
DISTRIB_ROOT=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    --distro) DISTRO="$2"; shift 2 ;;
    --distro=*) DISTRO="${1#*=}"; shift ;;
    --distrib-root) DISTRIB_ROOT="$2"; shift 2 ;;
    --distrib-root=*) DISTRIB_ROOT="${1#*=}"; shift ;;
    -h|--help)
      sed -n '2,20p' "$0"
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

# Resolve the --distro argument into:
#   DISTRO_SOURCE — what gets passed to `tabula-distro install` (path or URI)
#   DISTRO_NAME   — short label for logging / post-install hook lookup
#   POST_INSTALL  — optional install.sh path (only when source is a local dir)
DISTRO_SOURCE=""
DISTRO_NAME=""
POST_INSTALL=""

case "$DISTRO" in
  git+*|local:*)
    # Pass URI through; derive a name from #path= or last URI segment.
    DISTRO_SOURCE="$DISTRO"
    if [[ "$DISTRO" == *"#path="* ]]; then
      DISTRO_NAME="${DISTRO##*#path=}"
      DISTRO_NAME="${DISTRO_NAME%%[?&]*}"
      DISTRO_NAME="${DISTRO_NAME##*/}"
    else
      DISTRO_NAME="${DISTRO##*/}"
      DISTRO_NAME="${DISTRO_NAME%.git*}"
    fi
    ;;
  /*|./*|../*)
    DISTRO_SOURCE="$(cd "$DISTRO" && pwd)"
    DISTRO_NAME="$(basename "$DISTRO_SOURCE")"
    POST_INSTALL="$DISTRO_SOURCE/install.sh"
    ;;
  *)
    # Bare name — look up in DISTRIB_ROOT (defaulting to sibling tabula-distrib).
    if [ -z "$DISTRIB_ROOT" ] && [ -d "$REPO_ROOT/../tabula-distrib" ]; then
      DISTRIB_ROOT="$(cd "$REPO_ROOT/../tabula-distrib" && pwd)"
    fi
    if [ -z "$DISTRIB_ROOT" ]; then
      echo "error: --distro '$DISTRO' is a bare name but no tabula-distrib checkout found" >&2
      echo "  hint: clone https://github.com/bamanoz/tabula-distrib next to this repo," >&2
      echo "        pass --distrib-root, or pass a full path / git+ URI to --distro" >&2
      exit 1
    fi
    DISTRO_SOURCE="$DISTRIB_ROOT/$DISTRO"
    DISTRO_NAME="$DISTRO"
    POST_INSTALL="$DISTRO_SOURCE/install.sh"
    ;;
esac

# For local-path sources, sanity-check the directory exists up front so we fail
# early rather than inside tabula-distro.
case "$DISTRO_SOURCE" in
  /*|./*|../*)
    if [ ! -d "$DISTRO_SOURCE" ]; then
      echo "error: distro source not found: $DISTRO_SOURCE" >&2
      exit 1
    fi
    ;;
esac

echo "==> Stopping any running tabula kernel"
pkill -f "$TABULA_HOME/bin/tabula serve" 2>/dev/null || true
sleep 0.3

echo "==> Installing Tabula to $TABULA_HOME (distro: $DISTRO_NAME)"
echo "    distro source: $DISTRO_SOURCE"
mkdir -p "$TABULA_HOME" "$BIN_DIR"

# Wipe any previous runtime layout — tabula-distro rebuilds it from scratch.
rm -rf \
  "$TABULA_HOME/boot.py" \
  "$TABULA_HOME/templates" \
  "$TABULA_HOME/skills" \
  "$TABULA_HOME/distrib"

cp "$REPO_ROOT/examples/boot-cicd.py" "$TABULA_HOME/"

# Shared skill library (preserved across distro swaps by tabula-distro).
mkdir -p "$TABULA_HOME/skills"
rsync -a --delete --exclude '__pycache__' --exclude '*.pyc' \
  "$REPO_ROOT/skills/lib/" "$TABULA_HOME/skills/lib/"

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
"$VENV/bin/pip" install -q -e "$REPO_ROOT/tools/tabula-distro"
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
echo "==> Installing distro from $DISTRO_SOURCE"
"$VENV/bin/tabula-distro" --home "$TABULA_HOME" install "$DISTRO_SOURCE"

# Optional distro-specific post-install hook (only for local sources where we
# can see the source tree; for git+ sources the hook is expected to live inside
# the materialized generation under $TABULA_HOME/distrib/<name>/current).
if [ -n "$POST_INSTALL" ] && [ -f "$POST_INSTALL" ]; then
  echo "==> Running post-install hook: $DISTRO_NAME"
  TABULA_HOME="$TABULA_HOME" REPO_ROOT="$REPO_ROOT" bash "$POST_INSTALL"
else
  GENERATION_HOOK="$TABULA_HOME/distrib/$DISTRO_NAME/current/install.sh"
  if [ -f "$GENERATION_HOOK" ]; then
    echo "==> Running post-install hook: $DISTRO_NAME (from generation)"
    TABULA_HOME="$TABULA_HOME" REPO_ROOT="$REPO_ROOT" bash "$GENERATION_HOOK"
  fi
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
echo "Installed. Active distro: $DISTRO_NAME"
echo "  Active link: $TABULA_HOME/distrib/active -> $(readlink "$TABULA_HOME/distrib/active" 2>/dev/null || echo '?')"

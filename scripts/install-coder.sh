#!/bin/bash
# Convenience wrapper: install the Tabula kernel, then install the coder
# distro via tabula-distro.
#
# Prefers a sibling ../tabula-distrib/coder checkout; otherwise falls back
# to the public git+ source.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TABULA_HOME="${TABULA_HOME:-$HOME/.tabula}"

# 1) Kernel
bash "$SCRIPT_DIR/install-dev.sh"

# 2) Distro
if [ -d "$REPO_ROOT/../tabula-distrib/coder" ]; then
  DISTRO_SOURCE="$(cd "$REPO_ROOT/../tabula-distrib/coder" && pwd)"
else
  DISTRO_SOURCE='git+https://github.com/bamanoz/tabula-distrib.git@main#path=coder'
fi

echo "==> Installing coder distro from $DISTRO_SOURCE"
"$TABULA_HOME/.venv/bin/tabula-distro" --home "$TABULA_HOME" install "$DISTRO_SOURCE"

GATEWAY_DIR="$TABULA_HOME/skills/gateway-tui"
if command -v bun >/dev/null 2>&1; then
  if [ -d "$TABULA_HOME/skills/_tslib" ]; then
    echo "==> Installing TypeScript skill SDK dependencies"
    (cd "$TABULA_HOME/skills/_tslib" && bun install)
    mkdir -p "$TABULA_HOME/skills/_tslib/node_modules"
    touch "$TABULA_HOME/skills/_tslib/node_modules/.tabula-sdk-installed"
  fi
  echo "==> Installing coder TUI dependencies"
  (cd "$GATEWAY_DIR" && bun install)
  mkdir -p "$GATEWAY_DIR/node_modules/@tabula"
  rm -rf "$GATEWAY_DIR/node_modules/@tabula/skill-sdk"
  ln -s "$TABULA_HOME/skills/_tslib" "$GATEWAY_DIR/node_modules/@tabula/skill-sdk"
  touch "$GATEWAY_DIR/node_modules/.tabula-coder-installed"
else
  echo "warning: Bun is required to run coder TUI. Install it from https://bun.sh" >&2
fi

cat <<EOF

Coder distro installed.

Run:

  tabula-coder

EOF

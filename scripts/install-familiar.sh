#!/bin/bash
# Convenience wrapper: install the Tabula kernel, then install the familiar
# distro via tabula-distro.
#
# Prefers a sibling ../tabula-distrib/familiar checkout; otherwise falls back
# to the public git+ source.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TABULA_HOME="${TABULA_HOME:-$HOME/.tabula}"

# 1) Kernel
bash "$SCRIPT_DIR/install-dev.sh"

# 2) Distro
if [ -d "$REPO_ROOT/../tabula-distrib/familiar" ]; then
  DISTRO_SOURCE="$(cd "$REPO_ROOT/../tabula-distrib/familiar" && pwd)"
else
  DISTRO_SOURCE='git+https://github.com/bamanoz/tabula-distrib.git@main#path=familiar'
fi

echo "==> Installing familiar distro from $DISTRO_SOURCE"
"$TABULA_HOME/.venv/bin/tabula-distro" --home "$TABULA_HOME" install "$DISTRO_SOURCE"

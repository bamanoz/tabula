#!/bin/bash
# Convenience wrapper: install the Tabula kernel, then install the guardian
# distro via tabula-distro.
#
# Prefers a sibling ../tabula-distrib/guardian checkout; otherwise falls back
# to the public git+ source.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TABULA_HOME="${TABULA_HOME:-$HOME/.tabula}"

# 1) Kernel
bash "$SCRIPT_DIR/install-dev.sh"

# 2) Distro
if [ -d "$REPO_ROOT/../tabula-distrib/guardian" ]; then
  DISTRO_SOURCE="$(cd "$REPO_ROOT/../tabula-distrib/guardian" && pwd)"
else
  DISTRO_SOURCE='git+https://github.com/bamanoz/tabula-distrib.git@main#path=guardian'
fi

echo "==> Installing guardian distro from $DISTRO_SOURCE"
"$TABULA_HOME/.venv/bin/tabula-distro" --home "$TABULA_HOME" install "$DISTRO_SOURCE"

# Post-install hook (sandbox image build) lives inside the materialized generation.
POST_INSTALL="$TABULA_HOME/distrib/guardian/current/install.sh"
if [ -f "$POST_INSTALL" ]; then
  echo "==> Running guardian post-install hook"
  TABULA_HOME="$TABULA_HOME" bash "$POST_INSTALL"
fi

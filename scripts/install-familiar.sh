#!/bin/bash
# Convenience wrapper: install Tabula with the familiar distro active.
# Defaults to the sibling ../tabula-distrib/familiar checkout; if missing,
# the user must pass --distro <path-or-git+url>.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

if [ "$#" -eq 0 ]; then
  if [ -d "$REPO_ROOT/../tabula-distrib/familiar" ]; then
    exec bash "$SCRIPT_DIR/install-dev.sh" --distro "$REPO_ROOT/../tabula-distrib/familiar"
  fi
  exec bash "$SCRIPT_DIR/install-dev.sh" \
    --distro 'git+https://github.com/bamanoz/tabula-distrib.git@main#path=familiar'
fi
exec bash "$SCRIPT_DIR/install-dev.sh" "$@"

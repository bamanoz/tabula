#!/bin/bash
# Convenience wrapper: install Tabula with the assistant distro active.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec bash "$SCRIPT_DIR/install-dev.sh" --distro assistant "$@"

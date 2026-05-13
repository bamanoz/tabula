#!/bin/bash
# Create or refresh one Tabula tenant outside project bootstrap.
set -euo pipefail

TABULA_HOME="${TABULA_HOME:-$HOME/.tabula}"
TENANT_ID=""
DISPLAY_NAME=""

die() { printf 'error: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Usage: scripts/tenant-init.sh <tenant-id> [options]

Options:
  --tabula-home PATH      Runtime/config/state root (default: TABULA_HOME or ~/.tabula)
  --display-name NAME     Tenant display name
  -h, --help              Show this help
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --tabula-home)
      [ "$#" -ge 2 ] || die "--tabula-home requires a path"
      TABULA_HOME="$2"
      shift 2
      ;;
    --display-name)
      [ "$#" -ge 2 ] || die "--display-name requires a value"
      DISPLAY_NAME="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --*)
      die "unknown arg: $1"
      ;;
    *)
      [ -z "$TENANT_ID" ] || die "unexpected argument: $1"
      TENANT_ID="$1"
      shift
      ;;
  esac
done

[ -n "$TENANT_ID" ] || { usage >&2; exit 1; }

TABULA_HOME="$(cd "$(dirname "$TABULA_HOME")" && pwd)/$(basename "$TABULA_HOME")"
BIN_DIR="$TABULA_HOME/bin"
if [ -x "$BIN_DIR/tabula" ]; then
  TABULA_BIN="$BIN_DIR/tabula"
else
  TABULA_BIN="$(command -v tabula 2>/dev/null)" || die "tabula binary not found in $BIN_DIR or PATH"
fi

export TABULA_HOME
if [ -n "$DISPLAY_NAME" ]; then
  "$TABULA_BIN" tenant create "$TENANT_ID" --display-name "$DISPLAY_NAME" --exists-ok
else
  "$TABULA_BIN" tenant create "$TENANT_ID" --exists-ok
fi

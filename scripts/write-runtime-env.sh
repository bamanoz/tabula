#!/bin/bash
set -euo pipefail

if [ "$#" -ne 3 ]; then
  echo "usage: write-runtime-env.sh ENV_FILE TABULA_VENV TABULA_PATH" >&2
  exit 2
fi

env_file="$1"
tabula_venv="$2"
tabula_path="$3"
mkdir -p "$(dirname "$env_file")"
tmp="$(mktemp "${env_file}.tmp.XXXXXX")"
trap 'rm -f "$tmp"' EXIT

if [ -f "$env_file" ]; then
  grep -v -e '^TABULA_VENV=' -e '^TABULA_PATH=' "$env_file" >"$tmp" || true
fi
printf 'TABULA_VENV=%s\nTABULA_PATH=%s\n' "$tabula_venv" "$tabula_path" >>"$tmp"
chmod 600 "$tmp"
mv "$tmp" "$env_file"
trap - EXIT

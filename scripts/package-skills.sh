#!/bin/bash
# Package the Tabula kernel runtime payload (no distros, no bundles).
# Distros come from https://github.com/bamanoz/tabula-distrib and are pulled in
# at install time by ``tabula-distro``. Called by GoReleaser before hook:
#   bash scripts/package-skills.sh <version>
set -euo pipefail

VERSION="${1:?usage: package-skills.sh <version>}"
mkdir -p extra

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
payload="$tmp/payload"
mkdir -p "$payload/bin" "$payload/config" "$payload/libexec" "$payload/tools"
cp config/global.toml "$payload/config/global.toml.example"
cp scripts/install_payload.py "$payload/libexec/install_payload.py"
cp bin/tabula-runner "$payload/bin/tabula-runner"
cp bin/tabula-runner.ps1 "$payload/bin/tabula-runner.ps1"
cp bin/tabula-cli "$payload/bin/tabula-cli"
cp bin/tabula-cli.ps1 "$payload/bin/tabula-cli.ps1"
cp -R tools/tabula-distro "$payload/tools/tabula-distro"

tar -czf "extra/tabula-skills-${VERSION}.tar.gz" \
  -C "$payload" \
  --exclude='skills/.venv' \
  --exclude='tools/tabula-distro/**/__pycache__' \
  --exclude='tools/tabula-distro/**/*.pyc' \
  --exclude='tools/tabula-distro/**/*.egg-info' \
  --exclude='tools/tabula-distro/tests' \
  --exclude='tools/tabula-distro/.pytest_cache' \
  config/global.toml.example \
  libexec/install_payload.py \
  bin/tabula-runner \
  bin/tabula-runner.ps1 \
  bin/tabula-cli \
  bin/tabula-cli.ps1 \
  tools/tabula-distro/

echo "Created extra/tabula-skills-${VERSION}.tar.gz"

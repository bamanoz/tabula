#!/bin/bash
# Package the Tabula kernel runtime payload (no distros, no bundles).
# Distros come from https://github.com/bamanoz/tabula-distrib and are pulled in
# at install time by ``tabula-distro``. Called by GoReleaser before hook:
#   bash scripts/package-skills.sh <version>
set -euo pipefail

VERSION="${1:?usage: package-skills.sh <version>}"
mkdir -p extra

tar -czf "extra/tabula-skills-${VERSION}.tar.gz" \
  --exclude='skills/.venv' \
  --exclude='tools/tabula-distro/**/__pycache__' \
  --exclude='tools/tabula-distro/**/*.pyc' \
  --exclude='tools/tabula-distro/tests' \
  --exclude='tools/tabula-distro/.pytest_cache' \
  config/global.toml \
  examples/boot-cicd.py \
  bin/tabula-server \
  bin/tabula-server.ps1 \
  bin/tabula-cli \
  bin/tabula-cli.ps1 \
  bin/tabula-api \
  bin/tabula-api.ps1 \
  bin/tabula-coder \
  bin/tabula-claw \
  bin/tabula-install-distro \
  bin/tabula-install-distro.ps1 \
  scripts/install-distro.py \
  tools/tabula-distro/ \
  service/

echo "Created extra/tabula-skills-${VERSION}.tar.gz"

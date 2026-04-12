#!/bin/bash
# Package skills, templates, config, and launch scripts into a tarball.
# Called by GoReleaser before hook: bash scripts/package-skills.sh <version>
set -euo pipefail

VERSION="${1:?usage: package-skills.sh <version>}"
mkdir -p dist

tar -czf "dist/tabula-skills-${VERSION}.tar.gz" \
  --exclude='skills/driver-mock' \
  --exclude='skills/subagent-mock' \
  --exclude='skills/gateway-test' \
  --exclude='skills/*/__pycache__' \
  --exclude='skills/__pycache__' \
  --exclude='skills/*/*.pyc' \
  --exclude='skills/.venv' \
  skills/ \
  templates/ \
  boot.py \
  tabula.yaml \
  bin/tabula-headless \
  bin/tabula-cli \
  bin/tabula-api

echo "Created dist/tabula-skills-${VERSION}.tar.gz"

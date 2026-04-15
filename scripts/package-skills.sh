#!/bin/bash
# Package skills, templates, config, and launch scripts into a tarball.
# Called by GoReleaser before hook: bash scripts/package-skills.sh <version>
set -euo pipefail

VERSION="${1:?usage: package-skills.sh <version>}"
mkdir -p extra

tar -czf "extra/tabula-skills-${VERSION}.tar.gz" \
  --exclude='skills/driver-mock' \
  --exclude='skills/subagent-mock' \
  --exclude='skills/gateway-test' \
  --exclude='skills/*/__pycache__' \
  --exclude='skills/__pycache__' \
  --exclude='skills/*/*.pyc' \
  --exclude='skills/.venv' \
  --exclude='bundles/*/__pycache__' \
  --exclude='bundles/*/*/__pycache__' \
  --exclude='bundles/*/*.pyc' \
  --exclude='bundles/*/*/*.pyc' \
  skills/ \
  bundles/ \
  templates/ \
  boot.py \
  tabula.yaml \
  bin/tabula-server \
  bin/tabula-cli \
  bin/tabula-api \
  service/

echo "Created extra/tabula-skills-${VERSION}.tar.gz"

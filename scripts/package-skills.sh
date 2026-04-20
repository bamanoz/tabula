#!/bin/bash
# Package distrib/assistant, shared libs, config, and launch scripts into a tarball.
# Called by GoReleaser before hook: bash scripts/package-skills.sh <version>
set -euo pipefail

VERSION="${1:?usage: package-skills.sh <version>}"
mkdir -p extra

tar -czf "extra/tabula-skills-${VERSION}.tar.gz" \
  --exclude='distrib/*/__pycache__' \
  --exclude='distrib/*/*.pyc' \
  --exclude='distrib/*/*/__pycache__' \
  --exclude='distrib/*/*/*.pyc' \
  --exclude='distrib/*/*/*/__pycache__' \
  --exclude='distrib/*/*/*/*.pyc' \
  --exclude='distrib/*/*/*/*/__pycache__' \
  --exclude='distrib/*/*/*/*/*.pyc' \
  --exclude='distrib/assistant/skills/*/__pycache__' \
  --exclude='distrib/assistant/skills/*/.pytest_cache' \
  --exclude='distrib/assistant/skills/*/.pytest_cache/**' \
  --exclude='distrib/assistant/skills/__pycache__' \
  --exclude='distrib/assistant/skills/*/*.pyc' \
  --exclude='distrib/assistant/templates/__pycache__' \
  --exclude='distrib/assistant/__pycache__' \
  --exclude='distrib/assistant/boot.pyc' \
  --exclude='skills/lib/__pycache__' \
  --exclude='skills/lib/.pytest_cache' \
  --exclude='skills/lib/.pytest_cache/**' \
  --exclude='skills/lib/*.pyc' \
  --exclude='skills/lib/*/__pycache__' \
  --exclude='skills/.venv' \
  --exclude='bundles/*/__pycache__' \
  --exclude='bundles/*/*/__pycache__' \
  --exclude='bundles/*/*.pyc' \
  --exclude='bundles/*/*/*.pyc' \
  distrib/ \
  skills/lib/ \
  bundles/ \
  config/global.toml \
  examples/boot-cicd.py \
  bin/tabula-server \
  bin/tabula-server.ps1 \
  bin/tabula-cli \
  bin/tabula-cli.ps1 \
  bin/tabula-api \
  bin/tabula-api.ps1 \
  bin/tabula-install-distro \
  bin/tabula-install-distro.ps1 \
  scripts/install-distro.py \
  service/

echo "Created extra/tabula-skills-${VERSION}.tar.gz"

#!/bin/bash
# Install Tabula kernel + runtime daemon from source.
#
# Installs the local runtime layer:
#   * Go binaries (`tabula` + `tabula-runtime`, built from this repo)
#   * agent launcher (tabula-agent)
#   * Python venv with runtime + dev dependencies
#   * tabula-distro installer (editable, from tools/tabula-distro)
#
# Normal dev-agent entrypoint:
#
#   make agent dev
#
# That target installs this runtime, installs the local code-immune distro with
# local ../tabula-bundles source overrides, binds the current checkout, and runs
# tabula-agent on the dev port.
set -euo pipefail

while [ "$#" -gt 0 ]; do
  case "$1" in
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

TABULA_HOME="${TABULA_HOME:-$HOME/.tabula}"
BIN_DIR="$TABULA_HOME/bin"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
VENV="$TABULA_HOME/.venv"
if [ -z "${TABULA_VENV:-}" ]; then
  platform="$(uname -s)"
  arch="$(uname -m)"
  case "$platform" in
    MSYS_NT*|MINGW*|CYGWIN*)
      platform="Windows"
      ;;
  esac
  case "$arch" in
    x86_64) arch="AMD64" ;;
    aarch64|arm64) arch="ARM64" ;;
  esac
  VENV="$TABULA_HOME/.venv-$platform-$arch"
else
  VENV="$TABULA_VENV"
fi

echo "==> Stopping any running tabula kernel/runtime"
pkill -f "$TABULA_HOME/bin/tabula serve" 2>/dev/null || true
pkill -f "$TABULA_HOME/bin/tabula-runtime start" 2>/dev/null || true
sleep 0.3

echo "==> Installing Tabula kernel/runtime to $TABULA_HOME"
mkdir -p "$TABULA_HOME" "$BIN_DIR"

# Seed config only on a fresh install; an existing tree is entirely user-owned.
if [ ! -e "$TABULA_HOME/config" ]; then
  mkdir -p "$TABULA_HOME/config"
  cp "$REPO_ROOT/config/global.toml" "$TABULA_HOME/config/global.toml"
fi

# Optional service unit templates for host service managers. Agent/dev installs do
# not need them, and newer source checkouts may not carry this packaging surface.
if [ -d "$REPO_ROOT/service" ]; then
  rsync -a --delete "$REPO_ROOT/service/" "$TABULA_HOME/service/"
fi

# Python venv with dependencies
if [ -d "$VENV" ] && { ! "$VENV/bin/python" -c 'import sys; print(sys.version)' >/dev/null 2>&1 || ! "$VENV/bin/pip" --version >/dev/null 2>&1; }; then
  echo "==> Recreating invalid Python venv"
  rm -rf "$VENV"
fi
if [ ! -d "$VENV" ]; then
  echo "==> Creating Python venv"
  python3 -m venv "$VENV"
fi
"$VENV/bin/pip" install -q --upgrade pip
"$VENV/bin/pip" install -q -r "$SCRIPT_DIR/requirements-dev.txt"
TABULA_BUNDLES_ROOT="${TABULA_BUNDLES_ROOT:-$REPO_ROOT/../tabula-bundles}"
if [ -d "$TABULA_BUNDLES_ROOT" ]; then
  "$VENV/bin/python" - "$TABULA_BUNDLES_ROOT" <<'PY'
import site
import sys
from pathlib import Path

root = Path(sys.argv[1]).resolve()
paths = [
    root / "extensions" / "plugin-sdk" / "sdk" / "python" / "src",
    root / "extensions" / "skills" / "sdk" / "python" / "src",
    root / "async" / "tool-result-store" / "sdk" / "python" / "src",
    root / "collaboration" / "sessions" / "sdk" / "python" / "src",
    root / "async" / "deferred-tools" / "sdk" / "python" / "src",
    root / "drivers" / "driver" / "sdk" / "python" / "src",
    root / "mempalace" / "mempalace-common" / "sdk" / "python" / "src",
]
existing = [path for path in paths if path.is_dir()]
if not existing:
    raise SystemExit(f"no component-owned Python SDK roots found under {root}")
site_packages = Path(site.getsitepackages()[0])
site_packages.mkdir(parents=True, exist_ok=True)
(site_packages / "tabula-bundles-sdk-roots.pth").write_text("\n".join(str(path) for path in existing) + "\n", encoding="utf-8")
PY
else
  echo "warning: tabula-bundles checkout not found at $TABULA_BUNDLES_ROOT" >&2
  echo "         distro materializers may require tabula_plugin_sdk; set TABULA_BUNDLES_ROOT to your checkout" >&2
fi
"$VENV/bin/pip" install -q -e "$REPO_ROOT/tools/tabula-distro"
echo "    Python dependencies installed"

# Record the local platform venv for host-side launch scripts. Docker falls
# back to $TABULA_HOME/.venv when this path does not exist inside the container.
if [ ! -f "$TABULA_HOME/.env" ] || ! grep -qF 'TABULA_VENV=' "$TABULA_HOME/.env"; then
  printf 'TABULA_VENV=%s\n' "$VENV" >> "$TABULA_HOME/.env"
fi

# Go binaries
echo "==> Building Go binaries"
VERSION_STR="$(cat "$REPO_ROOT/VERSION")"
COMMIT_STR="$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
DATE_STR="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-X main.version=$VERSION_STR -X main.commit=$COMMIT_STR -X main.date=$DATE_STR"
( cd "$REPO_ROOT" && go build -ldflags "$LDFLAGS" -o "$BIN_DIR/tabula" ./cmd/tabula/ )
( cd "$REPO_ROOT" && go build -ldflags "$LDFLAGS" -o "$BIN_DIR/tabula-runtime" ./cmd/tabula-runtime/ )
# Record installed Tabula version for tabula-distro compatibility checks.
echo "$VERSION_STR" > "$TABULA_HOME/VERSION"
# Record the supported runtime plugin compatibility range so the distro tool can
# enforce `requires.protocol_version` on plugin manifests offline.
"$BIN_DIR/tabula" --protocol > "$TABULA_HOME/PROTOCOL"
if [ "$(uname)" = "Darwin" ]; then
  codesign --force --sign - "$BIN_DIR/tabula" 2>/dev/null || true
  codesign --force --sign - "$BIN_DIR/tabula-runtime" 2>/dev/null || true
fi


# Symlink installer entrypoints from venv into bin/ so they're on PATH alongside the rest.
ln -sf "$VENV/bin/tabula-agent" "$BIN_DIR/tabula-agent"
ln -sf "$VENV/bin/tabula-install" "$BIN_DIR/tabula-install"
ln -sf "$VENV/bin/tabula-distro" "$BIN_DIR/tabula-distro"

# PATH config
SHELL_RC=""
if [ -n "${ZSH_VERSION:-}" ] || [ -f "$HOME/.zshrc" ]; then
  SHELL_RC="$HOME/.zshrc"
elif [ -f "$HOME/.bashrc" ]; then
  SHELL_RC="$HOME/.bashrc"
elif [ -f "$HOME/.bash_profile" ]; then
  SHELL_RC="$HOME/.bash_profile"
fi

PATH_LINE="export PATH=\"$TABULA_HOME/bin:\$PATH\""
HOME_LINE="export TABULA_HOME=\"$TABULA_HOME\""
TABULA_PATH_VALUE="$TABULA_HOME/.venv/bin:$TABULA_HOME/bin:$PATH"
TABULA_PATH_VALUE="$VENV/bin:$TABULA_HOME/bin:$PATH"
TABULA_PATH_LINE="export TABULA_PATH=\"$TABULA_PATH_VALUE\""

if [ -n "$SHELL_RC" ]; then
  if ! grep -qF 'TABULA_HOME' "$SHELL_RC"; then
    {
      echo ""
      echo "# Tabula"
      echo "$HOME_LINE"
      echo "$PATH_LINE"
      echo "$TABULA_PATH_LINE"
    } >> "$SHELL_RC"
    echo "    Added to $SHELL_RC"
  else
    echo "    Already configured in $SHELL_RC"
  fi
  export TABULA_HOME="$TABULA_HOME"
  export TABULA_PATH="$TABULA_PATH_VALUE"
  export PATH="$TABULA_HOME/bin:$PATH"
else
  echo "Could not detect shell rc file. Add manually:"
  printf '  %s\n  %s\n  %s\n' "$HOME_LINE" "$PATH_LINE" "$TABULA_PATH_LINE"
fi

cat <<EOF

Tabula kernel/runtime installed at $TABULA_HOME.

Next for this checkout:

  make agent dev

That installs/updates local code-immune with local ../tabula-bundles, binds this checkout, and runs tabula-agent.

EOF

#!/bin/bash
# Verify an installed Tabula local runtime is ready.
#
# This is a readiness wrapper for an already installed/dev-installed
# TABULA_HOME. It is intentionally not a distro installer. The default mode is
# CI-safe: create the project tenant, launch `tabula serve`, wait for `tabula
# status --json` to report an attached local runtime serving that tenant, then
# tear down the kernel and verify the managed runtime exits.
set -euo pipefail

TABULA_HOME="${TABULA_HOME:-$HOME/.tabula}"
TIMEOUT_SECONDS=15
KEEP_RUNNING=0
PROJECT_FILE=""
PROJECT_NAME="default"
PROJECT_DISPLAY_NAME="Default"
PROJECT_ROOT="$PWD"

info() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
ok() { printf '\033[1;32m  ✓\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Usage: scripts/bootstrap.sh [options]

Options:
  --tabula-home PATH   Runtime/config/state root to check (default: TABULA_HOME or ~/.tabula)
  --project PATH       Optional tabula.project.toml to inspect for local/remote mode
  --timeout SECONDS    Readiness timeout (default: 15)
  --keep-running       Leave the launched kernel running after readiness succeeds
  -h, --help           Show this help

Default behavior ensures the project tenant exists, launches the kernel, waits
for `tabula status --json` to show kernel.running=true plus an attached local
runtime serving that tenant, then terminates the kernel and verifies the local
`tabula-runtime` process exits.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --tabula-home)
      [ "$#" -ge 2 ] || die "--tabula-home requires a path"
      TABULA_HOME="$2"
      shift 2
      ;;
    --project)
      [ "$#" -ge 2 ] || die "--project requires a path"
      PROJECT_FILE="$2"
      shift 2
      ;;
    --timeout)
      [ "$#" -ge 2 ] || die "--timeout requires seconds"
      TIMEOUT_SECONDS="$2"
      shift 2
      ;;
    --keep-running)
      KEEP_RUNNING=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown arg: $1"
      ;;
  esac
done

case "$TIMEOUT_SECONDS" in
  ''|*[!0-9]*) die "--timeout must be a positive integer" ;;
  0) die "--timeout must be greater than zero" ;;
esac

TABULA_HOME="$(cd "$(dirname "$TABULA_HOME")" && pwd)/$(basename "$TABULA_HOME")"
BIN_DIR="$TABULA_HOME/bin"
KERNEL_CONFIG="$TABULA_HOME/config/kernel.toml"
LOG_DIR="$TABULA_HOME/logs"
KERNEL_STDOUT="$LOG_DIR/bootstrap-kernel.out.log"
KERNEL_STDERR="$LOG_DIR/bootstrap-kernel.err.log"
STATUS_SNAPSHOT="$LOG_DIR/bootstrap-status-last.json"
STATUS_ERROR="$LOG_DIR/bootstrap-status-last.error.txt"
KERNEL_PID=0
RUNTIME_PID=0
LAST_STATUS=""
LAST_ERROR=""

find_python() {
  if [ -x "$TABULA_HOME/.venv/bin/python3" ]; then
    printf '%s\n' "$TABULA_HOME/.venv/bin/python3"
    return 0
  fi
  for candidate in python3.13 python3.12 python3.11 python3; do
    if command -v "$candidate" >/dev/null 2>&1; then
      "$candidate" - <<'PY' >/dev/null 2>&1 || continue
import sys
raise SystemExit(0 if sys.version_info >= (3, 11) else 1)
PY
      command -v "$candidate"
      return 0
    fi
  done
  return 1
}

resolve_binary() {
  local name="$1"
  if [ -x "$BIN_DIR/$name" ]; then
    printf '%s\n' "$BIN_DIR/$name"
    return 0
  fi
  command -v "$name" 2>/dev/null || return 1
}

find_runtime_pid() {
  if ! command -v pgrep >/dev/null 2>&1; then
    return 1
  fi
  pgrep -f "$TABULA_HOME/config/runtime.toml" | head -n 1
}

load_tabula_env() {
  local env_file="$TABULA_HOME/.env"
  [ -f "$env_file" ] || return 0
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      ''|'#'*) continue ;;
    esac
    local key="${line%%=*}"
    local value="${line#*=}"
    key="${key#${key%%[![:space:]]*}}"
    key="${key%${key##*[![:space:]]}}"
    value="${value#${value%%[![:space:]]*}}"
    value="${value%${value##*[![:space:]]}}"
    [ -n "$key" ] || continue
    case "$key" in
      [A-Za-z_][A-Za-z0-9_]*) ;;
      *) continue ;;
    esac
    if [ -z "${!key+x}" ]; then
      export "$key=$value"
    fi
  done < "$env_file"
}

inspect_project_mode() {
  local file="$1"
  [ -f "$file" ] || return 0
  "$PYTHON_BIN" - "$file" <<'PY'
import json
import sys
import tomllib

path = sys.argv[1]
with open(path, "rb") as fh:
    data = tomllib.load(fh)
project = data.get("project", {}) if isinstance(data, dict) else {}
kernel = data.get("kernel", {}) if isinstance(data, dict) else {}
print(json.dumps({
    "name": str(project.get("name") or "default"),
    "display_name": str(project.get("display_name") or project.get("name") or "Default"),
    "root": str(project.get("root") or ""),
    "mode": str(kernel.get("mode") or "local"),
}))
PY
}

cleanup() {
  local exit_code=$?
  if [ "$KEEP_RUNNING" -eq 0 ] && [ "$KERNEL_PID" -gt 0 ]; then
    if kill -0 "$KERNEL_PID" 2>/dev/null; then
      kill -TERM "$KERNEL_PID" 2>/dev/null || true
      for _ in 1 2 3 4 5 6 7 8 9 10; do
        kill -0 "$KERNEL_PID" 2>/dev/null || break
        sleep 0.5
      done
      if kill -0 "$KERNEL_PID" 2>/dev/null; then
        kill -KILL "$KERNEL_PID" 2>/dev/null || true
      fi
      wait "$KERNEL_PID" 2>/dev/null || true
    fi
  fi
  if [ "$KEEP_RUNNING" -eq 0 ] && [ "$RUNTIME_PID" -gt 0 ]; then
    for _ in 1 2 3 4 5 6 7 8 9 10; do
      kill -0 "$RUNTIME_PID" 2>/dev/null || return "$exit_code"
      sleep 0.5
    done
    if kill -0 "$RUNTIME_PID" 2>/dev/null; then
      printf '\033[1;31merror:\033[0m runtime pid %s did not exit after kernel shutdown\n' "$RUNTIME_PID" >&2
      return 1
    fi
  elif [ "$KEEP_RUNNING" -eq 0 ]; then
    local observed_pid
    observed_pid="$(find_runtime_pid 2>/dev/null || true)"
    if [ -n "$observed_pid" ] && kill -0 "$observed_pid" 2>/dev/null; then
      printf '\033[1;31merror:\033[0m runtime pid %s did not exit after kernel shutdown\n' "$observed_pid" >&2
      return 1
    fi
  fi
  return "$exit_code"
}
trap cleanup EXIT INT TERM

PYTHON_BIN="$(find_python)" || die "Python 3.11+ is required"
load_tabula_env

TABULA_BIN="$(resolve_binary tabula)" || die "tabula binary not found in $BIN_DIR or PATH"
RUNTIME_BIN="$(resolve_binary tabula-runtime)" || die "tabula-runtime binary not found in $BIN_DIR or PATH"

info "Checking Tabula home: $TABULA_HOME"
[ -x "$TABULA_BIN" ] || die "tabula is not executable: $TABULA_BIN"
[ -x "$RUNTIME_BIN" ] || die "tabula-runtime is not executable: $RUNTIME_BIN"
[ -f "$KERNEL_CONFIG" ] || die "missing kernel config at $KERNEL_CONFIG; install a distro before bootstrap"

"$TABULA_BIN" --version >/dev/null
ok "tabula --version"
"$RUNTIME_BIN" --version >/dev/null
ok "tabula-runtime --version"

if [ -z "$PROJECT_FILE" ] && [ -f "tabula.project.toml" ]; then
  PROJECT_FILE="tabula.project.toml"
fi
if [ -n "$PROJECT_FILE" ]; then
  project_json="$(inspect_project_mode "$PROJECT_FILE")"
  project_mode="$(printf '%s' "$project_json" | "$PYTHON_BIN" -c 'import json,sys; print(json.load(sys.stdin)["mode"])')"
  PROJECT_NAME="$(printf '%s' "$project_json" | "$PYTHON_BIN" -c 'import json,sys; print(json.load(sys.stdin)["name"])')"
  PROJECT_DISPLAY_NAME="$(printf '%s' "$project_json" | "$PYTHON_BIN" -c 'import json,sys; print(json.load(sys.stdin)["display_name"])')"
  parsed_project_root="$(printf '%s' "$project_json" | "$PYTHON_BIN" -c 'import json,sys; print(json.load(sys.stdin)["root"])')"
  if [ -n "$parsed_project_root" ]; then
    PROJECT_ROOT="$parsed_project_root"
  fi
  info "Project: $PROJECT_NAME ($project_mode)"
  if [ "$project_mode" = "remote" ]; then
    die "remote bootstrap is not implemented in M2"
  fi
else
  info "Project: default (implicit local mode)"
fi

export TABULA_HOME
export TABULA_PATH="${TABULA_PATH:-$TABULA_HOME/.venv/bin:$BIN_DIR:$PATH}"
export PATH="$BIN_DIR:$PATH"
TABULA_RUNNER_BIN="${TABULA_RUNNER_BIN:-$BIN_DIR/tabula-runner}"

mkdir -p "$LOG_DIR"
rm -f "$STATUS_SNAPSHOT"
rm -f "$STATUS_ERROR"
info "Ensuring tenant: $PROJECT_NAME"
"$TABULA_BIN" tenant create "$PROJECT_NAME" --display-name "$PROJECT_DISPLAY_NAME" --exists-ok >/dev/null
"$TABULA_BIN" tenant set "$PROJECT_NAME" --workspace-root "$PROJECT_ROOT" >/dev/null
ok "tenant ready: $PROJECT_NAME"
info "Launching tabula-runner"
"$TABULA_RUNNER_BIN" >"$KERNEL_STDOUT" 2>"$KERNEL_STDERR" &
KERNEL_PID=$!

info "Waiting up to ${TIMEOUT_SECONDS}s for local runtime tenant readiness"
deadline=$(( $(date +%s) + TIMEOUT_SECONDS ))
while [ "$(date +%s)" -lt "$deadline" ]; do
  if raw="$($TABULA_BIN status --json 2>&1)"; then
    LAST_STATUS="$raw"
    printf '%s\n' "$LAST_STATUS" >"$STATUS_SNAPSHOT"
    rm -f "$STATUS_ERROR"
    if STATUS_JSON="$raw" PROJECT_NAME="$PROJECT_NAME" "$PYTHON_BIN" - <<'PY' 2>/dev/null
import json
import os
import sys

doc = json.loads(os.environ["STATUS_JSON"])
tenant = os.environ["PROJECT_NAME"]
if not doc.get("kernel", {}).get("running"):
    raise SystemExit(1)
for runtime in doc.get("runtimes", []):
    tenants = set(runtime.get("tenants_served") or [])
    if runtime.get("id") == "local" and runtime.get("attached") and tenant in tenants:
        raise SystemExit(0)
raise SystemExit(1)
PY
    then
      RUNTIME_PID="$(find_runtime_pid 2>/dev/null || true)"
      if [ -n "$RUNTIME_PID" ]; then
        ok "ready: tenant=$PROJECT_NAME runtime=local pid=$RUNTIME_PID"
      else
        ok "ready: tenant=$PROJECT_NAME runtime=local"
      fi
      if [ "$KEEP_RUNNING" -eq 1 ]; then
        ok "kernel left running (pid $KERNEL_PID)"
        KERNEL_PID=0
      fi
      exit 0
    fi
  else
    LAST_ERROR="$raw"
    printf '%s\n' "$LAST_ERROR" >"$STATUS_ERROR"
  fi
  sleep 0.5
done

printf '\033[1;31merror:\033[0m local runtime did not become ready before timeout\n' >&2
if [ -n "$LAST_STATUS" ]; then
  printf '\nLast status body:\n%s\n' "$LAST_STATUS" >&2
fi
if [ -n "$LAST_ERROR" ]; then
  printf '\nLast status error:\n%s\n' "$LAST_ERROR" >&2
fi
printf '\nKernel stdout: %s\nKernel stderr: %s\n' "$KERNEL_STDOUT" "$KERNEL_STDERR" >&2
exit 1

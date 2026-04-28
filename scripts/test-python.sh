#!/usr/bin/env bash
set -euo pipefail

PYTHON_BIN="${PYTHON_BIN:-.venv/bin/python3}"
LAYER="${1:-unit}"
PYTHONPATH="tools/tabula-distro/src${PYTHONPATH:+:$PYTHONPATH}"
export PYTHONPATH

run_pytest_allow_empty() {
  set +e
  "$PYTHON_BIN" -m pytest "$@"
  status=$?
  set -e
  if [ "$status" -eq 5 ]; then
    echo "no tests matched"
    return 0
  fi
  return "$status"
}

case "$LAYER" in
  unit)
    exec "$PYTHON_BIN" -m pytest -m unit tests tools/tabula-distro/tests -q
    ;;
  smoke)
    run_pytest_allow_empty -m smoke tests -q
    exit $?
    ;;
  e2e)
    run_pytest_allow_empty -m e2e tests -q
    exit $?
    ;;
  contract)
    exec "$PYTHON_BIN" -m pytest -m contract tests tools/tabula-distro/tests -q
    ;;
  all)
    "$0" unit
    exec "$0" contract
    ;;
  list)
    exec "$PYTHON_BIN" -m pytest --collect-only tests tools/tabula-distro/tests -q
    ;;
  *)
    echo "usage: scripts/test-python.sh [unit|smoke|e2e|contract|all|list]" >&2
    exit 1
    ;;
esac

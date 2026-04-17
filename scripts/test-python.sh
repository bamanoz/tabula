#!/usr/bin/env bash
set -euo pipefail

PYTHON_BIN="${PYTHON_BIN:-.venv/bin/python3}"
LAYER="${1:-unit}"

case "$LAYER" in
  unit)
    exec "$PYTHON_BIN" -m pytest -m unit tests -q
    ;;
  smoke)
    exec "$PYTHON_BIN" -m pytest -m smoke tests -q
    ;;
  e2e)
    exec "$PYTHON_BIN" -m pytest -m e2e tests -q
    ;;
  contract)
    PYTHONPATH="skills/lib${PYTHONPATH:+:$PYTHONPATH}" exec "$PYTHON_BIN" -m pytest -m contract skills/lib/test_protocol.py skills/hook-permissions/test_permissions.py -q
    ;;
  all)
    "$0" unit
    "$0" contract
    "$0" smoke
    exec "$0" e2e
    ;;
  list)
    "$PYTHON_BIN" -m pytest --collect-only tests -q
    PYTHONPATH="skills/lib${PYTHONPATH:+:$PYTHONPATH}" exec "$PYTHON_BIN" -m pytest --collect-only skills/lib/test_protocol.py skills/hook-permissions/test_permissions.py -q
    ;;
  *)
    echo "usage: scripts/test-python.sh [unit|smoke|e2e|contract|all|list]" >&2
    exit 1
    ;;
esac

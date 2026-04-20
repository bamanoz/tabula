"""Shared helpers for the memory bundle (MemPalace wrapper)."""

from __future__ import annotations

import json
import os
import sys


TABULA_HOME = os.environ.get("TABULA_HOME", os.path.join(os.path.expanduser("~"), ".tabula"))
PALACE_PATH = os.path.join(TABULA_HOME, "data", "memory", "palace")
os.environ.setdefault("MEMPALACE_PALACE_PATH", PALACE_PATH)
os.makedirs(PALACE_PATH, exist_ok=True)


def import_mempalace():
    """Import mempalace lazily so a missing install fails with a clear message."""
    try:
        from mempalace import mcp_server  # noqa: WPS433 (intentional lazy import)
    except ImportError as e:
        print(
            json.dumps(
                {"error": f"mempalace is not installed: {e}", "hint": "pip install mempalace"}
            )
        )
        sys.exit(1)
    return mcp_server


def read_params() -> dict:
    """Read JSON params from stdin. Returns {} on empty input."""
    raw = sys.stdin.read().strip()
    if not raw:
        return {}
    try:
        return json.loads(raw)
    except json.JSONDecodeError as e:
        print(json.dumps({"error": f"invalid JSON params: {e}"}))
        sys.exit(1)


def emit(payload: dict) -> None:
    print(json.dumps(payload, ensure_ascii=False))


def dispatch(tools: dict, argv: list[str]) -> None:
    """Standard `run.py tool <name>` dispatcher used across the bundle."""
    if len(argv) >= 3 and argv[1] == "tool":
        name = argv[2]
        handler = tools.get(name)
        if not handler:
            emit({"error": f"unknown tool: {name}"})
            sys.exit(1)
        handler(read_params())
        return
    print(f"Usage: {argv[0]} tool <tool_name>", file=sys.stderr)
    sys.exit(1)

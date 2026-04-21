#!/usr/bin/env python3
"""memory-search — MemPalace read tools."""

from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from _memory.lib import PALACE_PATH, dispatch, emit, import_mempalace  # noqa: E402


def tool_memory_search(params: dict) -> None:
    query = params.get("query", "").strip()
    if not query:
        emit({"error": "query is required"})
        return
    mp = import_mempalace()
    emit(
        mp.tool_search(
            query=query,
            wing=params.get("wing") or None,
            room=params.get("room") or None,
            limit=int(params.get("limit", 5)),
        )
    )


def tool_memory_wake_up(params: dict) -> None:
    mp = import_mempalace()
    payload = {
        "palace_path": PALACE_PATH,
        "status": mp.tool_status(),
        "wings": mp.tool_list_wings(),
    }
    if params.get("wing"):
        payload["rooms"] = mp.tool_list_rooms(wing=params["wing"])
    emit(payload)


TOOLS = {
    "memory_search": tool_memory_search,
    "memory_wake_up": tool_memory_wake_up,
}


if __name__ == "__main__":
    dispatch(TOOLS, sys.argv)

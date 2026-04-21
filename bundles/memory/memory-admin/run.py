#!/usr/bin/env python3
"""memory-admin — MemPalace browse / manage tools."""

from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from _memory.lib import dispatch, emit, import_mempalace  # noqa: E402


def tool_memory_list(params: dict) -> None:
    mp = import_mempalace()
    emit(
        mp.tool_list_drawers(
            wing=params.get("wing") or None,
            room=params.get("room") or None,
            limit=int(params.get("limit", 20)),
            offset=int(params.get("offset", 0)),
        )
    )


def tool_memory_get(params: dict) -> None:
    drawer_id = params.get("drawer_id", "").strip()
    if not drawer_id:
        emit({"error": "drawer_id is required"})
        return
    mp = import_mempalace()
    emit(mp.tool_get_drawer(drawer_id=drawer_id))


def tool_memory_delete(params: dict) -> None:
    drawer_id = params.get("drawer_id", "").strip()
    if not drawer_id:
        emit({"error": "drawer_id is required"})
        return
    mp = import_mempalace()
    emit(mp.tool_delete_drawer(drawer_id=drawer_id))


def tool_memory_wings(_: dict) -> None:
    mp = import_mempalace()
    emit(mp.tool_list_wings())


def tool_memory_rooms(params: dict) -> None:
    mp = import_mempalace()
    emit(mp.tool_list_rooms(wing=params.get("wing") or None))


def tool_memory_status(_: dict) -> None:
    mp = import_mempalace()
    emit(mp.tool_status())


TOOLS = {
    "memory_list": tool_memory_list,
    "memory_get": tool_memory_get,
    "memory_delete": tool_memory_delete,
    "memory_wings": tool_memory_wings,
    "memory_rooms": tool_memory_rooms,
    "memory_status": tool_memory_status,
}


if __name__ == "__main__":
    dispatch(TOOLS, sys.argv)

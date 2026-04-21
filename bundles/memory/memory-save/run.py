#!/usr/bin/env python3
"""memory-save — MemPalace write tool."""

from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
from _memory.lib import dispatch, emit, import_mempalace  # noqa: E402


def tool_memory_save(params: dict) -> None:
    wing = params.get("wing", "").strip()
    room = params.get("room", "").strip()
    content = params.get("content", "")
    source = params.get("source") or None

    if not wing or not room:
        emit({"error": "wing and room are required"})
        return
    if not content or not content.strip():
        emit({"error": "content is empty"})
        return

    mp = import_mempalace()
    emit(mp.tool_add_drawer(wing=wing, room=room, content=content, source_file=source, added_by="tabula"))


TOOLS = {"memory_save": tool_memory_save}


if __name__ == "__main__":
    dispatch(TOOLS, sys.argv)

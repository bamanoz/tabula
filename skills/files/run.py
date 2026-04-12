#!/usr/bin/env python3
"""File tools — read, write, and edit files."""

from __future__ import annotations

import hashlib
import json
import os
import sys
import tempfile


def _state_path() -> str:
    """Return path to per-session state file that tracks read files."""
    session = os.environ.get("TABULA_SESSION", "default")
    safe = hashlib.md5(session.encode()).hexdigest()[:12]
    return os.path.join(tempfile.gettempdir(), f"tabula-files-{safe}")


def _mark_read(path: str) -> None:
    """Record that a file has been read in this session."""
    abs_path = os.path.abspath(path)
    state = _state_path()
    existing = set()
    if os.path.isfile(state):
        with open(state) as f:
            existing = set(line.strip() for line in f if line.strip())
    if abs_path not in existing:
        with open(state, "a") as f:
            f.write(abs_path + "\n")


def _was_read(path: str) -> bool:
    """Check if a file was read in this session."""
    abs_path = os.path.abspath(path)
    state = _state_path()
    if not os.path.isfile(state):
        return False
    with open(state) as f:
        return abs_path in set(line.strip() for line in f if line.strip())


def tool_read_file(params: dict) -> str:
    path = params.get("path", "")
    if not path:
        return json.dumps({"error": "path is required"})

    offset = max(1, int(params.get("offset", 1)))
    limit = int(params.get("limit", 2000))

    try:
        with open(path) as f:
            lines = f.readlines()
    except FileNotFoundError:
        return json.dumps({"error": f"file not found: {path}"})
    except PermissionError:
        return json.dumps({"error": f"permission denied: {path}"})

    _mark_read(path)

    total = len(lines)
    selected = lines[offset - 1 : offset - 1 + limit]

    out = []
    for i, line in enumerate(selected, start=offset):
        out.append(f"{i}\t{line.rstrip('\n')}")

    header = f"[{path}] lines {offset}-{min(offset + len(selected) - 1, total)} of {total}"
    return header + "\n" + "\n".join(out)


def tool_write_file(params: dict) -> str:
    path = params.get("path", "")
    content = params.get("content", "")
    if not path:
        return json.dumps({"error": "path is required"})

    if os.path.isfile(path) and not _was_read(path):
        return json.dumps({"error": "file exists but was not read first — use read_file before overwriting"})

    try:
        parent = os.path.dirname(path)
        if parent:
            os.makedirs(parent, exist_ok=True)
        with open(path, "w") as f:
            f.write(content)
    except PermissionError:
        return json.dumps({"error": f"permission denied: {path}"})

    _mark_read(path)

    lines = content.count("\n") + (1 if content and not content.endswith("\n") else 0)
    return f"Wrote {lines} lines to {path}"


def tool_str_replace(params: dict) -> str:
    path = params.get("path", "")
    old = params.get("old_string", "")
    new = params.get("new_string", "")
    replace_all = params.get("replace_all", False)

    if not path:
        return json.dumps({"error": "path is required"})
    if not old:
        return json.dumps({"error": "old_string is required"})

    if not _was_read(path):
        return json.dumps({"error": "file was not read first — use read_file before editing"})

    try:
        with open(path) as f:
            content = f.read()
    except FileNotFoundError:
        return json.dumps({"error": f"file not found: {path}"})
    except PermissionError:
        return json.dumps({"error": f"permission denied: {path}"})

    count = content.count(old)
    if count == 0:
        return json.dumps({"error": "old_string not found in file"})
    if count > 1 and not replace_all:
        return json.dumps({"error": f"old_string found {count} times — set replace_all: true to replace all"})

    if replace_all:
        new_content = content.replace(old, new)
    else:
        new_content = content.replace(old, new, 1)

    with open(path, "w") as f:
        f.write(new_content)

    return f"Replaced {count if replace_all else 1} occurrence(s) in {path}"


TOOLS = {
    "read_file": tool_read_file,
    "write_file": tool_write_file,
    "str_replace": tool_str_replace,
}


def main():
    if len(sys.argv) >= 3 and sys.argv[1] == "tool":
        tool_name = sys.argv[2]
        handler = TOOLS.get(tool_name)
        if not handler:
            print(f"ERROR: unknown tool {tool_name}")
            sys.exit(1)
        params = json.load(sys.stdin)
        result = handler(params)
        print(result)
    else:
        print("Usage: run.py tool <tool_name>", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
from __future__ import annotations

import json
import sys


TOOLS = [
    {
        "name": "my_echo",
        "description": "Echo text for example tests.",
        "schema": {
            "type": "object",
            "properties": {"text": {"type": "string", "description": "Text to echo."}},
            "required": [],
        },
    }
]


def send(frame: dict) -> None:
    sys.stdout.write(json.dumps(frame, separators=(",", ":")) + "\n")
    sys.stdout.flush()


def main() -> int:
    for line in sys.stdin:
        if not line.strip():
            continue
        frame = json.loads(line)
        op = frame.get("op")
        if op == "init":
            send({"op": "init_ack", "ready": True, "tools": TOOLS, "subscriptions": []})
        elif op == "call":
            args = frame.get("args") or {}
            send({"op": "result", "call_id": frame.get("call_id", ""), "ok": True, "data": {"ok": True, "text": args.get("text", "")}})
        elif op == "shutdown":
            return 0
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

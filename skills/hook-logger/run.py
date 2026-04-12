#!/usr/bin/env python3
"""Audit logger hook — writes kernel events to a JSONL file."""

from __future__ import annotations

import argparse
import json
import os
import sys
import time

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from skills.lib.kernel_client import KernelConnection

TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
DEFAULT_LOG = os.path.join(os.path.expanduser("~"), ".tabula", "logs", "hooks.jsonl")

HOOK_EVENTS = [
    {"event": "after_message", "priority": 0},
    {"event": "after_tool_call", "priority": 0},
    {"event": "session_start", "priority": 0},
]

# Modifying hooks require a hook_result response.
MODIFYING_EVENTS = {"session_start"}


def run(log_file: str, url: str = TABULA_URL):
    os.makedirs(os.path.dirname(log_file), exist_ok=True)

    conn = KernelConnection(url)
    conn.send({
        "type": "connect",
        "name": "hook-logger",
        "sends": ["hook_result"],
        "receives": ["hook"],
        "hooks": HOOK_EVENTS,
    })
    conn.recv()  # connected

    # No join — global subscriber (session=""), receives hooks for all sessions.

    try:
        while True:
            msg = conn.recv()
            if msg is None:
                break
            if msg.get("type") != "hook":
                continue

            entry = {
                "ts": time.time(),
                "event": msg.get("name", ""),
                "id": msg.get("id", ""),
                "payload": msg.get("payload"),
            }
            line = json.dumps(entry, ensure_ascii=False)
            with open(log_file, "a", encoding="utf-8") as f:
                f.write(line + "\n")

            # Modifying hooks require a response.
            if msg.get("name", "") in MODIFYING_EVENTS:
                conn.send({
                    "type": "hook_result",
                    "id": msg["id"],
                    "action": "pass",
                })
    except KeyboardInterrupt:
        pass
    finally:
        conn.close()


def main():
    parser = argparse.ArgumentParser(description="Tabula audit logger hook")
    parser.add_argument(
        "--log-file",
        default=os.environ.get("TABULA_LOG_FILE", DEFAULT_LOG),
        help="Path to JSONL log file",
    )
    parser.add_argument("--url", default=TABULA_URL, help="Kernel WebSocket URL")
    args = parser.parse_args()
    run(args.log_file, args.url)


if __name__ == "__main__":
    main()

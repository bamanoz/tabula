#!/usr/bin/env python3
"""Audit logger hook — writes kernel events to a JSONL file."""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
from pathlib import Path

ROOT = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

os.environ.setdefault("TABULA_HOME", ROOT)

from skills.lib import load_skill_config
from skills.lib.paths import skill_logs_dir
from skills.lib.kernel_client import KernelConnection
from skills.lib.protocol import (
    MSG_CONNECT, MSG_HOOK, MSG_HOOK_RESULT, HOOK_PASS,
    HOOK_AFTER_MESSAGE, HOOK_AFTER_TOOL_CALL, HOOK_SESSION_START,
)

TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")


def load_hook_logger_settings() -> dict:
    return load_skill_config(Path(__file__).resolve().parent)


SETTINGS = load_hook_logger_settings()
DEFAULT_LOG = SETTINGS.get("log_file") or str(skill_logs_dir("hook-logger") / "hooks.jsonl")

HOOK_EVENTS = [
    {"event": HOOK_AFTER_MESSAGE, "priority": 0},
    {"event": HOOK_AFTER_TOOL_CALL, "priority": 0},
    {"event": HOOK_SESSION_START, "priority": 0},
]

# Modifying hooks require a hook_result response.
MODIFYING_EVENTS = {HOOK_SESSION_START}


def run(log_file: str, url: str = TABULA_URL):
    os.makedirs(os.path.dirname(log_file), exist_ok=True)

    conn = KernelConnection(url)
    conn.send({
        "type": MSG_CONNECT,
        "name": "hook-logger",
        "sends": [MSG_HOOK_RESULT],
        "receives": [MSG_HOOK],
        "hooks": HOOK_EVENTS,
    })
    conn.recv()  # connected

    # No join — global subscriber (session=""), receives hooks for all sessions.

    try:
        while True:
            msg = conn.recv()
            if msg is None:
                break
            if msg.get("type") != MSG_HOOK:
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
                    "type": MSG_HOOK_RESULT,
                    "id": msg["id"],
                    "action": HOOK_PASS,
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

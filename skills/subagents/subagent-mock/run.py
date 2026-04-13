#!/usr/bin/env python3
"""Deterministic mock subagent."""

from __future__ import annotations

import argparse
import os
import sys
import time

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from skills.lib.kernel_client import KernelConnection


TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
TABULA_SPAWN_TOKEN = os.environ.get("TABULA_SPAWN_TOKEN", "")
VERBOSE = os.environ.get("TABULA_VERBOSE", "") == "1"


def log(msg: str):
    if VERBOSE:
        sys.stderr.write(f"[subagent:mock] {msg}\n")
        sys.stderr.flush()


def simulate(task: str, agent_id: str, index: int, max_turns: int, sleep_ms: int) -> str:
    steps = []
    for turn in range(1, max_turns + 1):
        if sleep_ms > 0:
            time.sleep(sleep_ms / 1000)
        steps.append(f"turn-{turn}")
    return f"mock result agent={agent_id} index={index} turns={max_turns} steps={','.join(steps)} task={task}"


def send_result(conn: KernelConnection, parent_session: str, agent_id: str, text: str):
    conn.send({"type": "message", "session": parent_session, "id": agent_id, "text": text})


def main():
    parser = argparse.ArgumentParser(description="Tabula mock subagent")
    parser.add_argument("--id", required=True)
    parser.add_argument("--parent-session", required=True)
    parser.add_argument("--task", required=True)
    parser.add_argument("--index", type=int, default=0)
    parser.add_argument("--max-turns", type=int, default=5)
    parser.add_argument("--sleep-ms", type=int, default=25)
    parser.add_argument("--timeout", type=int, default=0)
    args = parser.parse_args()

    session_name = f"subagent-{args.id}"
    conn = KernelConnection(TABULA_URL)
    connect_msg = {
        "type": "connect",
        "name": session_name,
        "sends": ["message"],
        "receives": ["message", "init"],
    }
    if TABULA_SPAWN_TOKEN:
        connect_msg["token"] = TABULA_SPAWN_TOKEN
    conn.send(connect_msg)
    conn.recv()
    conn.send({"type": "join", "session": session_name})
    conn.recv()
    init_msg = conn.recv()
    if init_msg is None or init_msg.get("type") != "init":
        log("did not receive init")

    result = simulate(args.task, args.id, args.index, args.max_turns, args.sleep_ms)
    send_result(conn, args.parent_session, args.id, result)

    if args.timeout <= 0:
        conn.close()
        return

    while True:
        try:
            msg = conn.recv(timeout=args.timeout)
        except TimeoutError:
            break
        if msg is None:
            break
        if msg.get("type") != "message":
            continue
        text = msg.get("text", "")
        if not text.strip():
            continue
        result = simulate(text, args.id, args.index, args.max_turns, args.sleep_ms)
        send_result(conn, args.parent_session, args.id, result)

    conn.close()


if __name__ == "__main__":
    main()

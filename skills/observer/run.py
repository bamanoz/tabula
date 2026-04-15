#!/usr/bin/env python3
"""Observer skill — collects metrics from kernel hooks and exposes them via HTTP."""

from __future__ import annotations

import argparse
import json
import os
import sys
import threading
import time
from http.server import HTTPServer, BaseHTTPRequestHandler

ROOT = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from skills.lib.kernel_client import KernelConnection
from skills.lib.protocol import (
    MSG_CONNECT, MSG_HOOK, MSG_HOOK_RESULT,
    HOOK_BEFORE_MESSAGE, HOOK_AFTER_MESSAGE,
    HOOK_BEFORE_TOOL_CALL, HOOK_AFTER_TOOL_CALL,
    HOOK_SESSION_START, HOOK_SESSION_END,
    HOOK_BEFORE_SPAWN, HOOK_AFTER_SPAWN,
)

TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")

# Observer subscribes to all observability hooks.
# For modifying hooks it always responds with "pass" (no-op).
HOOK_EVENTS = [
    {"event": HOOK_BEFORE_MESSAGE, "priority": 0},
    {"event": HOOK_AFTER_MESSAGE, "priority": 0},
    {"event": HOOK_BEFORE_TOOL_CALL, "priority": 0},
    {"event": HOOK_AFTER_TOOL_CALL, "priority": 0},
    {"event": HOOK_SESSION_START, "priority": 0},
    {"event": HOOK_SESSION_END, "priority": 0},
    {"event": HOOK_BEFORE_SPAWN, "priority": 0},
    {"event": HOOK_AFTER_SPAWN, "priority": 0},
]

# Modifying hooks that require a hook_result response.
MODIFYING_HOOKS = {HOOK_BEFORE_MESSAGE, HOOK_BEFORE_TOOL_CALL, HOOK_SESSION_START, HOOK_BEFORE_SPAWN}


class Metrics:
    """Thread-safe metrics store."""

    def __init__(self):
        self._lock = threading.Lock()
        self._msg_start: dict[str, float] = {}  # session → timestamp
        self._tool_start: dict[str, float] = {}  # tool_id → timestamp
        self.sessions: dict[str, dict] = {}
        self.tools: dict[str, dict] = {}  # tool_name → {calls, errors, total_ms}
        self.spawns: dict[str, dict] = {}  # command → {count, alive}
        self.started_at = time.time()

    def handle_hook(self, event: str, payload: dict):
        session = payload.get("session", "")
        now = time.time()

        with self._lock:
            if event == HOOK_BEFORE_MESSAGE:
                self._msg_start[session] = now
                self.sessions.setdefault(session, {})["last_message_at"] = now

            elif event == HOOK_AFTER_MESSAGE:
                start = self._msg_start.pop(session, None)
                info = self.sessions.setdefault(session, {})
                info["last_message_at"] = now
                info["message_count"] = info.get("message_count", 0) + 1
                if start:
                    elapsed = (now - start) * 1000
                    info["avg_latency_ms"] = (
                        (info.get("avg_latency_ms", 0) * (info["message_count"] - 1) + elapsed)
                        / info["message_count"]
                    )

            elif event == HOOK_BEFORE_TOOL_CALL:
                tool_id = payload.get("id", "")
                tool_name = payload.get("tool", "")
                self._tool_start[tool_id] = now
                info = self.tools.setdefault(tool_name, {"calls": 0, "errors": 0, "total_ms": 0})
                info["calls"] += 1

            elif event == HOOK_AFTER_TOOL_CALL:
                tool_id = payload.get("id", "")
                tool_name = payload.get("tool", "")
                start = self._tool_start.pop(tool_id, None)
                info = self.tools.setdefault(tool_name, {"calls": 0, "errors": 0, "total_ms": 0})
                if start:
                    info["total_ms"] += (now - start) * 1000

            elif event == HOOK_SESSION_START:
                client = payload.get("client", "")
                self.sessions.setdefault(session, {})["clients"] = sorted(
                    set(self.sessions.get(session, {}).get("clients", []) + [client])
                )

            elif event == HOOK_SESSION_END:
                self.sessions.setdefault(session, {})["ended_at"] = now

            elif event == HOOK_BEFORE_SPAWN:
                cmd = payload.get("command", "")
                info = self.spawns.setdefault(cmd, {"count": 0, "alive": True})
                info["count"] += 1

            elif event == HOOK_AFTER_SPAWN:
                cmd = payload.get("command", "")
                self.spawns.setdefault(cmd, {"count": 0, "alive": True})["alive"] = True

    def snapshot(self) -> dict:
        with self._lock:
            return {
                "uptime_sec": round(time.time() - self.started_at, 1),
                "sessions": dict(self.sessions),
                "tools": {
                    name: {
                        **info,
                        "avg_ms": round(info["total_ms"] / info["calls"], 1) if info["calls"] else 0,
                    }
                    for name, info in self.tools.items()
                },
                "spawns": dict(self.spawns),
            }


metrics = Metrics()


def run_hook_listener(url: str):
    """Connect to kernel and listen for hook events."""
    conn = KernelConnection(url)
    conn.send({
        "type": MSG_CONNECT,
        "name": "observer",
        "sends": [MSG_HOOK_RESULT],
        "receives": [MSG_HOOK],
        "hooks": HOOK_EVENTS,
    })
    conn.recv()  # connected

    try:
        while True:
            msg = conn.recv()
            if msg is None:
                break
            if msg.get("type") != MSG_HOOK:
                continue
            event = msg.get("name", "")
            metrics.handle_hook(event, msg.get("payload", {}))
            # Respond with "pass" on modifying hooks so kernel doesn't block.
            if event in MODIFYING_HOOKS:
                conn.send({
                    "type": MSG_HOOK_RESULT,
                    "id": msg.get("id", ""),
                    "action": "pass",
                })
    except (ConnectionError, OSError):
        pass
    finally:
        conn.close()


class MetricsHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/metrics":
            data = json.dumps(metrics.snapshot(), ensure_ascii=False, indent=2)
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(data.encode())
        else:
            self.send_response(404)
            self.end_headers()

    def log_message(self, format, *args):
        pass  # silence access logs


def main():
    parser = argparse.ArgumentParser(description="Tabula observer — metrics collector")
    parser.add_argument("--url", default=TABULA_URL, help="Kernel WebSocket URL")
    parser.add_argument("--port", type=int, default=8091, help="HTTP metrics port")
    args = parser.parse_args()

    # Start hook listener in background.
    t = threading.Thread(target=run_hook_listener, args=(args.url,), daemon=True)
    t.start()

    # Serve metrics on HTTP.
    server = HTTPServer(("127.0.0.1", args.port), MetricsHandler)
    print(f"observer: listening on http://127.0.0.1:{args.port}/metrics")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()

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
from urllib import parse as urlparse
from urllib import request as urlrequest

ROOT = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from skills.lib.kernel_client import KernelConnection
from skills.lib.protocol import (
    MSG_CONNECT, MSG_HOOK,
    HOOK_AFTER_MESSAGE, HOOK_AFTER_TOOL_CALL,
    HOOK_SESSION_END, HOOK_AFTER_SPAWN,
)

TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
SNAPSHOT_POLL_SEC = 0.5

# Observer only subscribes to observability hooks.
# Session/process topology is reconciled from /sessions snapshots so telemetry
# stays out of the policy/modifying path.
HOOK_EVENTS = [
    {"event": HOOK_AFTER_MESSAGE, "priority": 0},
    {"event": HOOK_AFTER_TOOL_CALL, "priority": 0},
    {"event": HOOK_SESSION_END, "priority": 0},
    {"event": HOOK_AFTER_SPAWN, "priority": 0},
]


class Metrics:
    """Thread-safe metrics store."""

    def __init__(self):
        self._lock = threading.Lock()
        self.sessions: dict[str, dict] = {}
        self.tools: dict[str, dict] = {}  # tool_name -> {calls, errors, total_ms}
        self.spawns: dict[str, dict] = {}  # command -> {count, alive}
        self.started_at = time.time()

    def handle_hook(self, event: str, payload: dict):
        session = payload.get("session", "")
        now = time.time()

        with self._lock:
            if event == HOOK_AFTER_MESSAGE:
                info = self.sessions.setdefault(session, {})
                info["last_message_at"] = now
                info["message_count"] = info.get("message_count", 0) + 1

            elif event == HOOK_AFTER_TOOL_CALL:
                tool_name = payload.get("tool", "")
                info = self.tools.setdefault(tool_name, {"calls": 0, "errors": 0, "total_ms": 0})
                info["calls"] += 1
                if str(payload.get("output", "")).startswith("ERROR:"):
                    info["errors"] += 1

            elif event == HOOK_SESSION_END:
                info = self.sessions.setdefault(session, {})
                info["ended_at"] = now
                info["clients"] = []

            elif event == HOOK_AFTER_SPAWN:
                cmd = payload.get("command", "")
                self.spawns.setdefault(cmd, {"count": 0, "alive": True})["alive"] = True
                self.spawns[cmd]["count"] += 1

    def reconcile_snapshot(self, snapshot: dict):
        with self._lock:
            live_counts: dict[str, int] = {}
            for session, info in snapshot.items():
                session_info = self.sessions.setdefault(session, {})
                session_info["clients"] = sorted(info.get("clients", []))
                for proc in info.get("processes", []):
                    cmd = proc.get("command", "")
                    if not cmd:
                        continue
                    live_counts[cmd] = live_counts.get(cmd, 0) + (1 if proc.get("alive") else 0)

            for cmd, alive_count in live_counts.items():
                spawn_info = self.spawns.setdefault(cmd, {"count": 0, "alive": False})
                if spawn_info["count"] < alive_count:
                    spawn_info["count"] = alive_count
                spawn_info["alive"] = alive_count > 0

            for cmd, spawn_info in self.spawns.items():
                if cmd not in live_counts:
                    spawn_info["alive"] = False

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


def sessions_url(kernel_url: str) -> str:
    parsed = urlparse.urlsplit(kernel_url)
    scheme = "https" if parsed.scheme == "wss" else "http"
    return urlparse.urlunsplit((scheme, parsed.netloc, "/sessions", "", ""))


def run_hook_listener(url: str):
    """Connect to kernel and listen for hook events."""
    conn = KernelConnection(url)
    conn.send({
        "type": MSG_CONNECT,
        "name": "observer",
        "sends": [],
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
    except (ConnectionError, OSError):
        pass
    finally:
        conn.close()


def poll_sessions(url: str):
    snapshot_url = sessions_url(url)
    opener = urlrequest.build_opener(urlrequest.ProxyHandler({}))
    while True:
        try:
            with opener.open(snapshot_url, timeout=1) as resp:
                metrics.reconcile_snapshot(json.loads(resp.read()))
        except Exception:
            pass
        time.sleep(SNAPSHOT_POLL_SEC)


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
    snapshot_thread = threading.Thread(target=poll_sessions, args=(args.url,), daemon=True)
    snapshot_thread.start()

    # Serve metrics on HTTP.
    server = HTTPServer(("127.0.0.1", args.port), MetricsHandler)
    print(f"observer: listening on http://127.0.0.1:{args.port}/metrics")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()

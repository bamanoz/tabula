#!/usr/bin/env python3
"""
Session registry skill for Tabula.

Subcommands:
  list              List all active sessions
  info <session>    Show details for a session
  daemon            Run as persistent daemon with lifecycle events
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.request
from pathlib import Path

SKILL_DIR = os.path.dirname(os.path.abspath(__file__))
ROOT = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

os.environ.setdefault("TABULA_HOME", ROOT)

from skills.lib import load_skill_config
from skills.lib.paths import skill_data_dir, skill_logs_dir
from skills.lib.protocol import MSG_MESSAGE

TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
TABULA_HOME = os.environ.get("TABULA_HOME", os.path.join(os.path.expanduser("~"), ".tabula"))


def load_sessions_settings() -> dict:
    return load_skill_config(Path(__file__).resolve().parent)


SETTINGS = load_sessions_settings()
IDLE_TIMEOUT = SETTINGS["idle_timeout"]
POLL_INTERVAL = SETTINGS["poll_interval"]


def _http_base() -> str:
    """Derive HTTP base URL from TABULA_URL (ws://host:port/ws → http://host:port)."""
    url = TABULA_URL
    url = url.replace("wss://", "https://").replace("ws://", "http://")
    if url.endswith("/ws"):
        url = url[:-3]
    return url


def _fetch_sessions() -> dict:
    """GET /sessions from the kernel."""
    url = _http_base() + "/sessions"
    req = urllib.request.Request(url)
    with urllib.request.urlopen(req, timeout=5) as resp:
        return json.loads(resp.read())


# ---------------------------------------------------------------------------
# CLI commands
# ---------------------------------------------------------------------------

def cmd_list(_args):
    """List all active sessions."""
    sessions = _fetch_sessions()
    if not sessions:
        print("No active sessions.")
        return
    for name, info in sorted(sessions.items()):
        clients = ", ".join(info.get("clients", []))
        procs = len([p for p in info.get("processes", []) if p.get("alive")])
        print(f"  {name}  clients=[{clients}]  processes={procs}")


def cmd_info(args):
    """Show details for one session."""
    sessions = _fetch_sessions()
    info = sessions.get(args.session)
    if not info:
        print(f"Session {args.session!r} not found.", file=sys.stderr)
        sys.exit(1)
    print(json.dumps({args.session: info}, indent=2))


def cmd_send(args):
    """Send a message to another session."""
    from skills.lib.kernel_client import KernelConnection
    from skills.lib.protocol import MSG_CONNECT, MSG_JOIN, MSG_MESSAGE
    conn = KernelConnection(TABULA_URL)
    conn.send({
        "type": MSG_CONNECT,
        "name": "sessions-send",
        "sends": [MSG_MESSAGE],
        "receives": [],
    })
    conn.recv()  # connected
    conn.send({"type": MSG_JOIN, "session": args.session})
    conn.recv()  # joined
    conn.send({
        "type": MSG_MESSAGE,
        "from_session": args.from_session,
        "text": args.message,
    })
    conn.close()
    print(f"Message sent to {args.session}.")


def cmd_history(args):
    """Show conversation history for a session."""
    history_file = str(skill_data_dir("sessions") / args.session / "history.jsonl")
    if not os.path.isfile(history_file):
        print(f"No history for session {args.session!r}.", file=sys.stderr)
        sys.exit(1)

    lines = []
    with open(history_file) as f:
        for line in f:
            line = line.strip()
            if line:
                try:
                    lines.append(json.loads(line))
                except json.JSONDecodeError:
                    continue

    if args.last:
        lines = lines[-args.last:]

    for entry in lines:
        ts = entry.get("ts", 0)
        ts_str = time.strftime("%H:%M:%S", time.localtime(ts)) if ts else "??:??:??"
        role = entry.get("role", "?")

        if args.summary and role == "tool":
            continue
        if args.summary and "tool_use" in entry:
            continue

        if "text" in entry:
            print(f"[{ts_str}] {role}: {entry['text']}")
        elif "tool_use" in entry:
            tu = entry["tool_use"]
            print(f"[{ts_str}] {role}: tool_use {tu['name']}({json.dumps(tu.get('input', {}), ensure_ascii=False)})")
        elif "output" in entry:
            output = entry["output"]
            if len(output) > 200:
                output = output[:200] + "..."
            print(f"[{ts_str}] {role}: result({entry.get('tool_use_id', '?')}) → {output}")
        else:
            print(f"[{ts_str}] {role}: {json.dumps(entry, ensure_ascii=False)}")


# ---------------------------------------------------------------------------
# Daemon
# ---------------------------------------------------------------------------

class SessionRegistry:
    """Polls kernel, tracks sessions, emits lifecycle events."""

    def __init__(self):
        self.known: dict[str, dict] = {}  # session_name → snapshot
        self.last_activity: dict[str, float] = {}  # session_name → timestamp
        self.idle_sessions: set[str] = set()
        self.conn = None

    def _connect(self):
        from skills.lib.kernel_client import KernelConnection
        from skills.lib.protocol import MSG_CONNECT, MSG_JOIN, MSG_MESSAGE
        self.conn = KernelConnection(TABULA_URL)
        self.conn.send({
            "type": MSG_CONNECT,
            "name": "session-registry",
            "sends": [MSG_MESSAGE],
            "receives": [],
        })
        self.conn.recv()  # connected
        self.conn.send({"type": MSG_JOIN, "session": "_system"})
        self.conn.recv()  # joined

    def _emit(self, event: str, session: str, data: dict | None = None):
        """Broadcast a lifecycle event to _system session."""
        payload = {"event": event, "session": session}
        if data:
            payload["data"] = data
        if self.conn:
            self.conn.send({
                "type": MSG_MESSAGE,
                "session": "_system",
                "text": json.dumps(payload),
            })
        _log(f"{event}: {session}")

    def poll(self):
        try:
            snapshot = _fetch_sessions()
        except Exception as e:
            _log(f"poll failed: {e}")
            return

        now = time.time()
        current_names = set(snapshot.keys())
        known_names = set(self.known.keys())

        # New sessions
        for name in current_names - known_names:
            self.known[name] = snapshot[name]
            self.last_activity[name] = now
            self._emit("created", name, snapshot[name])

        # Destroyed sessions
        for name in known_names - current_names:
            del self.known[name]
            self.last_activity.pop(name, None)
            self.idle_sessions.discard(name)
            self._emit("destroyed", name)

        # Update existing — check for activity changes
        for name in current_names & known_names:
            old = self.known[name]
            new = snapshot[name]
            # If clients or processes changed, there's activity
            if old != new:
                self.last_activity[name] = now
                self.known[name] = new
                if name in self.idle_sessions:
                    self.idle_sessions.discard(name)
                    self._emit("active", name)

        # Idle detection
        for name in list(current_names):
            if name.startswith("_"):
                continue  # skip system sessions
            last = self.last_activity.get(name, now)
            if now - last > IDLE_TIMEOUT and name not in self.idle_sessions:
                self.idle_sessions.add(name)
                self._emit("idle", name)

    def run(self):
        self._connect()
        _log(f"daemon started (poll={POLL_INTERVAL}s, idle={IDLE_TIMEOUT}s)")
        try:
            while True:
                self.poll()
                time.sleep(POLL_INTERVAL)
        except KeyboardInterrupt:
            pass


def cmd_daemon(_args):
    """Run the session registry daemon."""
    global _log_file
    log_path = str(skill_logs_dir("sessions") / "daemon.log")
    try:
        os.makedirs(os.path.dirname(log_path), exist_ok=True)
        _log_file = open(log_path, "a")
    except OSError:
        pass  # fall back to stderr
    registry = SessionRegistry()
    registry.run()


_log_file = None


def _log(msg: str):
    out = _log_file or sys.stderr
    out.write(f"[sessions] {msg}\n")
    out.flush()


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main():
    parser = argparse.ArgumentParser(description="Tabula session registry")
    sub = parser.add_subparsers(dest="command")

    sub.add_parser("list", help="List all active sessions")

    p_info = sub.add_parser("info", help="Show session details")
    p_info.add_argument("session", help="Session name")

    p_send = sub.add_parser("send", help="Send message to a session")
    p_send.add_argument("session", help="Target session name")
    p_send.add_argument("message", help="Message text")
    p_send.add_argument("--from", dest="from_session", default="", help="Sender session name")

    p_history = sub.add_parser("history", help="Show session history")
    p_history.add_argument("session", help="Session name")
    p_history.add_argument("--last", type=int, default=0, help="Show last N entries")
    p_history.add_argument("--summary", action="store_true", help="Omit tool calls/results")

    sub.add_parser("daemon", help="Run session registry daemon")

    args = parser.parse_args()
    if not args.command:
        parser.print_help()
        sys.exit(1)

    commands = {
        "list": cmd_list,
        "info": cmd_info,
        "send": cmd_send,
        "history": cmd_history,
        "daemon": cmd_daemon,
    }
    commands[args.command](args)


if __name__ == "__main__":
    main()

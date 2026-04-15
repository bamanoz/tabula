#!/usr/bin/env python3
"""E2E tests for subagents over the current WebSocket kernel protocol."""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import tempfile
import threading
import time
from contextlib import closing
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path

import websocket as ws_client


ROOT = Path(__file__).resolve().parents[1]
VENV_PYTHON = os.path.join(ROOT, ".venv", "bin", "python3")


class MockAnthropicHandler(BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(length) or b"{}")

        messages = body.get("messages", [])
        last_user = ""
        for message in reversed(messages):
            if message.get("role") == "user":
                content = message.get("content")
                if isinstance(content, str):
                    last_user = content
                    break

        text = f"mock result for: {last_user[:80]}"
        events = [
            {
                "type": "message_start",
                "message": {"id": "mock-1", "type": "message", "role": "assistant", "content": []},
            },
            {
                "type": "content_block_start",
                "index": 0,
                "content_block": {"type": "text", "text": ""},
            },
            {
                "type": "content_block_delta",
                "index": 0,
                "delta": {"type": "text_delta", "text": text},
            },
            {"type": "content_block_stop", "index": 0},
            {"type": "message_delta", "delta": {"stop_reason": "end_turn", "stop_sequence": None}},
            {"type": "message_stop"},
        ]
        payload = "".join(f"data: {json.dumps(event)}\n\n" for event in events).encode()
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, *args):
        pass


def start_mock_api():
    with closing(__import__("socket").socket()) as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    server = HTTPServer(("127.0.0.1", port), MockAnthropicHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, port


def get_free_port() -> int:
    with closing(__import__("socket").socket()) as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def setup_test_home(tabula_port: int) -> str:
    home = tempfile.mkdtemp(prefix="tabula-e2e-")
    shutil.copytree(ROOT / "skills", Path(home) / "skills")
    shutil.copytree(ROOT / ".venv", Path(home) / ".venv", dirs_exist_ok=True)
    (Path(home) / "tabula.yaml").write_text("boot: python3 boot.py\n")
    (Path(home) / "boot.py").write_text(
        "import json, os, sys\n"
        "json.dump({\n"
        f"  'url': 'ws://127.0.0.1:{tabula_port}/ws',\n"
        "  'system_prompt': 'test system prompt',\n"
        "  'spawn': []\n"
        "}, sys.stdout)\n"
    )
    return home


def start_kernel(home: str, api_port: int, tabula_port: int) -> subprocess.Popen:
    env = os.environ.copy()
    env["TABULA_HOME"] = home
    env["TABULA_URL"] = f"ws://127.0.0.1:{tabula_port}/ws"
    env["ANTHROPIC_API_KEY"] = "mock-key"
    env["ANTHROPIC_BASE_URL"] = f"http://127.0.0.1:{api_port}"
    env["TABULA_PROVIDER"] = "anthropic"
    env["TABULA_VERBOSE"] = "1"

    proc = subprocess.Popen(
        ["go", "run", "./cmd/tabula"],
        cwd=ROOT,
        env=env,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
    )
    deadline = time.time() + 10
    last_error = None
    while time.time() < deadline:
        try:
            conn = ws_client.create_connection(env["TABULA_URL"], timeout=1)
            conn.close()
            return proc
        except Exception as exc:
            last_error = exc
            time.sleep(0.2)
    proc.terminate()
    stderr = ""
    if proc.stderr:
        try:
            stderr = proc.stderr.read().decode("utf-8", errors="replace")
        except Exception:
            stderr = ""
    raise RuntimeError(f"kernel did not start: {last_error}\n{stderr}")


def connect_client(tabula_port: int):
    conn = ws_client.create_connection(f"ws://127.0.0.1:{tabula_port}/ws", timeout=5)
    conn.send(
        json.dumps(
            {
                "type": "connect",
                "name": "test-parent",
                "sends": ["message", "tool_use"],
                "receives": ["message", "tool_result", "init", "error"],
            }
        )
    )
    json.loads(conn.recv())
    conn.send(json.dumps({"type": "join", "session": "main"}))
    json.loads(conn.recv())
    init_msg = json.loads(conn.recv())
    assert init_msg["type"] == "init"
    return conn


def recv_json(conn, timeout=10):
    conn.settimeout(timeout)
    return json.loads(conn.recv())


def test_subagent_initial_task():
    mock, api_port = start_mock_api()
    tabula_port = get_free_port()
    home = setup_test_home(tabula_port)
    proc = None
    conn = None
    try:
        proc = start_kernel(home, api_port, tabula_port)
        conn = connect_client(tabula_port)
        conn.send(
            json.dumps(
                {
                    "type": "tool_use",
                    "id": "spawn-1",
                    "name": "SPAWN",
                    "input": {
                        "command": f"{VENV_PYTHON} skills/subagent-anthropic/run.py --id task_42 --parent-session main --task 'List files in /tmp' --timeout 5"
                    },
                }
            )
        )
        spawn_result = recv_json(conn, 5)
        assert spawn_result["type"] == "tool_result"
        assert spawn_result["id"] == "spawn-1"
        assert spawn_result["output"].startswith("PID ")

        msg = recv_json(conn, 10)
        assert msg["type"] == "message", msg
        assert msg["id"] == "task_42"
        assert "mock result for:" in msg["text"]
    finally:
        if conn is not None:
            conn.close()
        if proc is not None:
            proc.terminate()
            proc.wait(timeout=10)
        mock.shutdown()
        mock.server_close()
        shutil.rmtree(home, ignore_errors=True)


def test_subagent_followup():
    mock, api_port = start_mock_api()
    tabula_port = get_free_port()
    home = setup_test_home(tabula_port)
    proc = None
    conn = None
    try:
        proc = start_kernel(home, api_port, tabula_port)
        conn = connect_client(tabula_port)
        conn.send(
            json.dumps(
                {
                    "type": "tool_use",
                    "id": "spawn-2",
                    "name": "SPAWN",
                    "input": {
                        "command": f"{VENV_PYTHON} skills/subagent-anthropic/run.py --id followup_1 --parent-session main --task 'Initial task' --timeout 10"
                    },
                }
            )
        )
        recv_json(conn, 5)
        msg = recv_json(conn, 10)
        assert msg["type"] == "message", msg
        assert msg["id"] == "followup_1"

        conn.send(json.dumps({"type": "message", "session": "subagent-followup_1", "text": "Now do a second thing"}))
        msg2 = recv_json(conn, 10)
        assert msg2["type"] == "message"
        assert msg2["id"] == "followup_1"
        assert "mock result for:" in msg2["text"]
    finally:
        if conn is not None:
            conn.close()
        if proc is not None:
            proc.terminate()
            proc.wait(timeout=10)
        mock.shutdown()
        mock.server_close()
        shutil.rmtree(home, ignore_errors=True)


if __name__ == "__main__":
    try:
        test_subagent_initial_task()
        test_subagent_followup()
    except Exception as exc:
        print(f"FAIL: {exc}", file=sys.stderr)
        sys.exit(1)
    print("PASS")

#!/usr/bin/env python3
"""E2E test: real Anthropic driver + hook-logger with full system prompt.

Starts kernel with driver + hook-logger, sends a greeting,
collects response, prints hook log.

Requires ANTHROPIC_API_KEY in environment.
"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
from contextlib import closing
from pathlib import Path

import websocket as ws_client


ROOT = Path(__file__).resolve().parents[1]


def get_free_port() -> int:
    with closing(__import__("socket").socket()) as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def setup_test_home(port: int) -> tuple[str, str]:
    home = tempfile.mkdtemp(prefix="tabula-hook-real-")
    shutil.copytree(ROOT / "skills", Path(home) / "skills")
    shutil.copytree(ROOT / ".venv", Path(home) / ".venv", dirs_exist_ok=True)

    log_file = os.path.join(home, "hooks.jsonl")
    venv_py = os.path.join(home, ".venv", "bin", "python3")

    (Path(home) / "tabula.yaml").write_text("boot: python3 boot.py\n")
    (Path(home) / "boot.py").write_text(
        "import json, sys\n"
        "json.dump({\n"
        f"  'url': 'ws://127.0.0.1:{port}/ws',\n"
        "  'system_prompt': 'You are Tabula, an AI agent. You have kernel tools: EXEC, SPAWN, KILL, LIST. Be helpful, concise, and respond in Russian.',\n"
        "  'spawn': [\n"
        f"    '{venv_py} skills/drivers/driver-anthropic/run.py',\n"
        f"    '{venv_py} skills/hook-logger/run.py --log-file {log_file}',\n"
        "  ]\n"
        "}, sys.stdout)\n"
    )
    return home, log_file


def start_kernel(home: str, port: int) -> subprocess.Popen:
    env = os.environ.copy()
    env["TABULA_HOME"] = home
    env["TABULA_URL"] = f"ws://127.0.0.1:{port}/ws"
    env["TABULA_PROVIDER"] = "anthropic"

    proc = subprocess.Popen(
        ["go", "run", "./cmd/tabula"],
        cwd=ROOT,
        env=env,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    deadline = time.time() + 15
    while time.time() < deadline:
        try:
            conn = ws_client.create_connection(env["TABULA_URL"], timeout=1)
            conn.close()
            return proc
        except Exception:
            time.sleep(0.3)
    proc.terminate()
    raise RuntimeError("kernel did not start")


def main():
    if not os.environ.get("ANTHROPIC_API_KEY"):
        print("SKIP: ANTHROPIC_API_KEY not set")
        return

    port = get_free_port()
    home, log_file = setup_test_home(port)
    proc = None
    conn = None

    try:
        print(f"Starting kernel on port {port}...")
        proc = start_kernel(home, port)
        time.sleep(2)

        # Connect as gateway
        url = f"ws://127.0.0.1:{port}/ws"
        conn = ws_client.create_connection(url, timeout=10)
        conn.send(json.dumps({
            "type": "connect",
            "name": "test-gw",
            "sends": ["message"],
            "receives": ["stream_start", "stream_delta", "stream_end", "done", "error"],
        }))
        resp = json.loads(conn.recv())
        assert resp["type"] == "connected"

        conn.send(json.dumps({"type": "join", "session": "main"}))
        resp = json.loads(conn.recv())
        assert resp["type"] == "joined"

        # Drain member_joined etc
        conn.settimeout(2)
        try:
            while True:
                conn.recv()
        except Exception:
            pass

        # Send greeting
        print("Sending: 'Привет!'")
        conn.settimeout(60)
        conn.send(json.dumps({"type": "message", "text": "Привет!"}))

        # Collect all done messages (count them)
        chunks = []
        done_count = 0
        msg_types = []
        deadline = time.time() + 60
        while time.time() < deadline:
            try:
                conn.settimeout(max(1, deadline - time.time()))
                msg = json.loads(conn.recv())
            except Exception:
                break
            t = msg.get("type")
            msg_types.append(t)
            if t == "stream_delta":
                chunks.append(msg.get("text", ""))
            elif t == "done":
                done_count += 1
                break  # stop after first done
            elif t == "error":
                print(f"ERROR: {msg}")
                break

        response = "".join(chunks)
        print(f"Response: {response[:200]}")
        print(f"Done count: {done_count}")
        print(f"Message types seen: {msg_types}")

        # Wait for hook-logger to flush
        time.sleep(1.5)

        # Check hook log
        print(f"\n--- Hook log ---")
        if os.path.isfile(log_file):
            with open(log_file) as f:
                for line in f:
                    entry = json.loads(line.strip())
                    ts = time.strftime("%H:%M:%S", time.localtime(entry["ts"]))
                    event = entry["event"]
                    payload = entry.get("payload", {})
                    print(f"  [{ts}] {event}: {json.dumps(payload, ensure_ascii=False)}")
        else:
            print("  (no log file)")

    finally:
        if conn:
            conn.close()
        if proc:
            proc.terminate()
            proc.wait(timeout=10)
        shutil.rmtree(home, ignore_errors=True)


if __name__ == "__main__":
    main()

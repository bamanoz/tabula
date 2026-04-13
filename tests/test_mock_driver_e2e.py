#!/usr/bin/env python3
"""E2E tests for the deterministic mock driver + mock subagents."""

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


def setup_test_home(tabula_port: int) -> str:
    home = tempfile.mkdtemp(prefix="tabula-mock-e2e-")
    shutil.copytree(ROOT / "skills", Path(home) / "skills")
    shutil.copytree(ROOT / ".venv", Path(home) / ".venv", dirs_exist_ok=True)
    (Path(home) / "tabula.yaml").write_text("boot: python3 boot.py\n")
    (Path(home) / "boot.py").write_text(
        "import json, sys\n"
        "json.dump({\n"
        f"  'url': 'ws://127.0.0.1:{tabula_port}/ws',\n"
        "  'system_prompt': 'mock test prompt',\n"
        "  'spawn': ['.venv/bin/python3 skills/drivers/driver-mock/run.py']\n"
        "}, sys.stdout)\n"
    )
    return home


def start_kernel(home: str, tabula_port: int, subagents: int = 4, turns: int = 6, sleep_ms: int = 10) -> subprocess.Popen:
    env = os.environ.copy()
    env["TABULA_HOME"] = home
    env["TABULA_URL"] = f"ws://127.0.0.1:{tabula_port}/ws"
    env["TABULA_PROVIDER"] = "mock"
    env["TABULA_VERBOSE"] = "1"
    env["TABULA_MOCK_SUBAGENTS"] = str(subagents)
    env["TABULA_MOCK_TURNS"] = str(turns)
    env["TABULA_MOCK_SLEEP_MS"] = str(sleep_ms)
    env["TABULA_MOCK_WAIT_SEC"] = "10"

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
                "name": "test-gateway",
                "sends": ["message"],
                "receives": ["stream_start", "stream_delta", "stream_end", "done", "error", "init"],
            }
        )
    )
    json.loads(conn.recv())
    conn.send(json.dumps({"type": "join", "session": "main"}))
    json.loads(conn.recv())
    init_msg = json.loads(conn.recv())
    assert init_msg["type"] == "init"
    return conn


def wait_for_driver_ready():
    time.sleep(0.75)


def collect_turn(conn, timeout: float = 10.0) -> str:
    conn.settimeout(timeout)
    chunks: list[str] = []
    saw_done = False
    while not saw_done:
        msg = json.loads(conn.recv())
        msg_type = msg.get("type")
        if msg_type == "stream_delta":
            chunks.append(msg.get("text", ""))
        elif msg_type == "error":
            raise AssertionError(msg)
        elif msg_type == "done":
            saw_done = True
    return "".join(chunks)


def test_mock_driver_spawns_and_aggregates():
    tabula_port = get_free_port()
    home = setup_test_home(tabula_port)
    proc = None
    conn = None
    try:
        proc = start_kernel(home, tabula_port, subagents=4, turns=6, sleep_ms=10)
        conn = connect_client(tabula_port)
        wait_for_driver_ready()
        started = time.time()
        conn.send(json.dumps({"type": "message", "text": "test mock orchestration"}))
        output = collect_turn(conn, timeout=10)
        elapsed = time.time() - started

        assert "spawning wave 1/1 with 4 mock subagents" in output
        assert "aggregated subagent results" in output
        assert "waves: 1" in output
        assert "received: 4/4" in output
        for idx in range(1, 5):
            assert f"mock_1_{idx}" in output
            assert "turns=6" in output
        assert elapsed < 5
    finally:
        if conn is not None:
            conn.close()
        if proc is not None:
            proc.terminate()
            proc.wait(timeout=10)
        shutil.rmtree(home, ignore_errors=True)


def test_mock_driver_handles_second_request():
    tabula_port = get_free_port()
    home = setup_test_home(tabula_port)
    proc = None
    conn = None
    try:
        proc = start_kernel(home, tabula_port, subagents=3, turns=5, sleep_ms=5)
        conn = connect_client(tabula_port)
        wait_for_driver_ready()

        conn.send(json.dumps({"type": "message", "text": "first request"}))
        out1 = collect_turn(conn, timeout=10)
        assert "request: first request" in out1
        assert "mock_1_1" in out1
        assert "mock_1_3" in out1

        conn.close()
        conn = connect_client(tabula_port)
        wait_for_driver_ready()

        conn.send(json.dumps({"type": "message", "text": "second request"}))
        out2 = collect_turn(conn, timeout=10)
        assert "request: second request" in out2
        assert "mock_2_1" in out2
        assert "mock_2_3" in out2
        assert "waves: 1" in out2
    finally:
        if conn is not None:
            conn.close()
        if proc is not None:
            proc.terminate()
            proc.wait(timeout=10)
        shutil.rmtree(home, ignore_errors=True)


def test_mock_driver_multiple_waves():
    tabula_port = get_free_port()
    home = setup_test_home(tabula_port)
    proc = None
    conn = None
    try:
        proc = start_kernel(home, tabula_port, subagents=2, turns=4, sleep_ms=5)
        conn = connect_client(tabula_port)
        wait_for_driver_ready()

        conn.send(json.dumps({"type": "message", "text": "research topic waves=3 subagents=2"}))
        output = collect_turn(conn, timeout=10)

        assert "spawning wave 1/3 with 2 mock subagents" in output
        assert "wave 1 complete, launching next wave" in output
        assert "spawning wave 2/3 with 2 mock subagents" in output
        assert "wave 2 complete, launching next wave" in output
        assert "spawning wave 3/3 with 2 mock subagents" in output
        assert "waves: 3" in output
        assert "received: 6/6" in output
        assert "mock_1_w1_1" in output
        assert "mock_1_w1_2" in output
        assert "mock_1_w2_1" in output
        assert "mock_1_w2_2" in output
        assert "mock_1_w3_1" in output
        assert "mock_1_w3_2" in output
    finally:
        if conn is not None:
            conn.close()
        if proc is not None:
            proc.terminate()
            proc.wait(timeout=10)
        shutil.rmtree(home, ignore_errors=True)


def test_mock_driver_variable_wave_fanouts():
    tabula_port = get_free_port()
    home = setup_test_home(tabula_port)
    proc = None
    conn = None
    try:
        proc = start_kernel(home, tabula_port, subagents=3, turns=4, sleep_ms=5)
        conn = connect_client(tabula_port)
        wait_for_driver_ready()

        conn.send(json.dumps({"type": "message", "text": "plan execution fanouts=2,4,1"}))
        output = collect_turn(conn, timeout=10)

        assert "spawning wave 1/3 with 2 mock subagents" in output
        assert "spawning wave 2/3 with 4 mock subagents" in output
        assert "spawning wave 3/3 with 1 mock subagents" in output
        assert "waves: 3" in output
        assert "received: 7/7" in output
        assert "mock_1_w1_1" in output
        assert "mock_1_w1_2" in output
        assert "mock_1_w2_1" in output
        assert "mock_1_w2_4" in output
        assert "mock_1_w3_1" in output
    finally:
        if conn is not None:
            conn.close()
        if proc is not None:
            proc.terminate()
            proc.wait(timeout=10)
        shutil.rmtree(home, ignore_errors=True)


def test_mock_driver_partial_spawn_failure_is_aggregated():
    tabula_port = get_free_port()
    home = setup_test_home(tabula_port)
    proc = None
    conn = None
    try:
        proc = start_kernel(home, tabula_port, subagents=3, turns=3, sleep_ms=5)
        conn = connect_client(tabula_port)
        wait_for_driver_ready()

        conn.send(json.dumps({"type": "message", "text": "stress fanouts=6"}))
        output = collect_turn(conn, timeout=10)

        assert "spawning wave 1/1 with 6 mock subagents" in output
        assert "received: 6/6" in output
        assert "spawn failed via" in output
        assert "too many active subagents" in output
    finally:
        if conn is not None:
            conn.close()
        if proc is not None:
            proc.terminate()
            proc.wait(timeout=10)
        shutil.rmtree(home, ignore_errors=True)


def test_mock_driver_ignores_concurrent_user_message():
    tabula_port = get_free_port()
    home = setup_test_home(tabula_port)
    proc = None
    conn = None
    try:
        proc = start_kernel(home, tabula_port, subagents=4, turns=5, sleep_ms=10)
        conn = connect_client(tabula_port)
        wait_for_driver_ready()

        conn.send(json.dumps({"type": "message", "text": "primary request"}))
        conn.send(json.dumps({"type": "message", "text": "secondary request"}))
        output = collect_turn(conn, timeout=10)

        assert "request: primary request" in output
        assert "busy, ignoring concurrent user message" in output
        assert "secondary request" not in output
    finally:
        if conn is not None:
            conn.close()
        if proc is not None:
            proc.terminate()
            proc.wait(timeout=10)
        shutil.rmtree(home, ignore_errors=True)


def test_mock_driver_timeout_aggregates_missing_subagent_results():
    tabula_port = get_free_port()
    home = setup_test_home(tabula_port)
    proc = None
    conn = None
    try:
        proc = start_kernel(home, tabula_port, subagents=3, turns=2, sleep_ms=5)
        conn = connect_client(tabula_port)
        wait_for_driver_ready()

        conn.send(json.dumps({"type": "message", "text": "timeout fanouts=6"}))
        output = collect_turn(conn, timeout=10)

        assert "received: 6/6" in output
        assert "spawn failed via" in output
        assert "too many active subagents" in output
    finally:
        if conn is not None:
            conn.close()
        if proc is not None:
            proc.terminate()
            proc.wait(timeout=10)
        shutil.rmtree(home, ignore_errors=True)


if __name__ == "__main__":
    try:
        test_mock_driver_spawns_and_aggregates()
        test_mock_driver_handles_second_request()
        test_mock_driver_multiple_waves()
        test_mock_driver_variable_wave_fanouts()
        test_mock_driver_partial_spawn_failure_is_aggregated()
        test_mock_driver_ignores_concurrent_user_message()
    except Exception as exc:
        print(f"FAIL: {exc}", file=sys.stderr)
        sys.exit(1)
    print("PASS")

#!/usr/bin/env python3
"""E2E tests for the kernel hook system."""

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
    home = tempfile.mkdtemp(prefix="tabula-hook-e2e-")
    shutil.copytree(ROOT / "skills", Path(home) / "skills")
    shutil.copytree(ROOT / ".venv", Path(home) / ".venv", dirs_exist_ok=True)
    (Path(home) / "tabula.yaml").write_text("boot: python3 boot.py\n")
    (Path(home) / "boot.py").write_text(
        "import json, sys\n"
        "json.dump({\n"
        f"  'url': 'ws://127.0.0.1:{tabula_port}/ws',\n"
        "  'system_prompt': 'hook test prompt',\n"
        "  'spawn': []\n"
        "}, sys.stdout)\n"
    )
    return home


def start_kernel(home: str, tabula_port: int) -> subprocess.Popen:
    env = os.environ.copy()
    env["TABULA_HOME"] = home
    env["TABULA_URL"] = f"ws://127.0.0.1:{tabula_port}/ws"
    env["TABULA_PROVIDER"] = "mock"

    proc = subprocess.Popen(
        ["go", "run", "./cmd/tabula"],
        cwd=ROOT,
        env=env,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
    )
    deadline = time.time() + 10
    while time.time() < deadline:
        try:
            conn = ws_client.create_connection(env["TABULA_URL"], timeout=1)
            conn.close()
            return proc
        except Exception:
            time.sleep(0.2)
    proc.terminate()
    stderr = proc.stderr.read().decode("utf-8", errors="replace") if proc.stderr else ""
    raise RuntimeError(f"kernel did not start\n{stderr}")


def connect_gateway(tabula_port: int):
    """Connect a gateway client that sends messages and receives done/error."""
    conn = ws_client.create_connection(f"ws://127.0.0.1:{tabula_port}/ws", timeout=5)
    conn.send(json.dumps({
        "type": "connect",
        "name": "test-gw",
        "sends": ["message", "done"],
        "receives": ["message", "done", "error"],
    }))
    resp = json.loads(conn.recv())
    assert resp["type"] == "connected"
    conn.send(json.dumps({"type": "join", "session": "s1"}))
    resp = json.loads(conn.recv())
    assert resp["type"] == "joined"
    return conn


def connect_driver(tabula_port: int):
    """Connect a driver client that receives messages and sends done."""
    conn = ws_client.create_connection(f"ws://127.0.0.1:{tabula_port}/ws", timeout=5)
    conn.send(json.dumps({
        "type": "connect",
        "name": "test-drv",
        "sends": ["done"],
        "receives": ["message"],
    }))
    resp = json.loads(conn.recv())
    assert resp["type"] == "connected"
    conn.send(json.dumps({"type": "join", "session": "s1"}))
    resp = json.loads(conn.recv())
    assert resp["type"] == "joined"
    # drain member_joined / init if any
    conn.settimeout(0.5)
    try:
        while True:
            conn.recv()
    except Exception:
        pass
    conn.settimeout(5)
    return conn


def connect_hook(tabula_port: int, name: str, hooks: list[dict]):
    """Connect a hook subscriber (global, no session join)."""
    conn = ws_client.create_connection(f"ws://127.0.0.1:{tabula_port}/ws", timeout=5)
    conn.send(json.dumps({
        "type": "connect",
        "name": name,
        "sends": ["hook_result"],
        "receives": ["hook"],
        "hooks": hooks,
    }))
    resp = json.loads(conn.recv())
    assert resp["type"] == "connected"
    return conn


def recv_msg(conn, timeout=3):
    conn.settimeout(timeout)
    try:
        return json.loads(conn.recv())
    except Exception:
        return None


# --- Tests ---


def test_void_hook_fires():
    """after_message hook fires when 'done' is sent."""
    port = get_free_port()
    home = setup_test_home(port)
    proc = None
    try:
        proc = start_kernel(home, port)
        hook = connect_hook(port, "logger", [{"event": "after_message", "priority": 0}])
        gw = connect_gateway(port)
        drv = connect_driver(port)

        # gw sends message, drv receives it
        gw.send(json.dumps({"type": "message", "text": "hello"}))
        msg = recv_msg(drv)
        assert msg is not None and msg["type"] == "message"

        # drv sends done → triggers after_message hook
        drv.send(json.dumps({"type": "done"}))
        hook_msg = recv_msg(hook, timeout=3)
        assert hook_msg is not None, "hook subscriber did not receive after_message"
        assert hook_msg["type"] == "hook"
        assert hook_msg["name"] == "after_message"
        print("  PASS: test_void_hook_fires")
    finally:
        if proc:
            proc.terminate()
            proc.wait(timeout=5)
        shutil.rmtree(home, ignore_errors=True)


def test_modifying_hook_pass():
    """before_message hook with 'pass' action delivers original message."""
    port = get_free_port()
    home = setup_test_home(port)
    proc = None
    try:
        proc = start_kernel(home, port)
        hook = connect_hook(port, "filter", [{"event": "before_message", "priority": 10}])
        gw = connect_gateway(port)
        drv = connect_driver(port)

        gw.send(json.dumps({"type": "message", "text": "hello"}))

        # hook receives before_message
        hook_msg = recv_msg(hook, timeout=3)
        assert hook_msg is not None and hook_msg["type"] == "hook"
        assert hook_msg["name"] == "before_message"

        # respond with pass
        hook.send(json.dumps({
            "type": "hook_result",
            "id": hook_msg["id"],
            "action": "pass",
        }))

        # driver should receive original message
        msg = recv_msg(drv, timeout=3)
        assert msg is not None and msg["type"] == "message"
        assert msg["text"] == "hello"
        print("  PASS: test_modifying_hook_pass")
    finally:
        if proc:
            proc.terminate()
            proc.wait(timeout=5)
        shutil.rmtree(home, ignore_errors=True)


def test_modifying_hook_modify():
    """before_message hook with 'modify' action changes the message text."""
    port = get_free_port()
    home = setup_test_home(port)
    proc = None
    try:
        proc = start_kernel(home, port)
        hook = connect_hook(port, "filter", [{"event": "before_message", "priority": 10}])
        gw = connect_gateway(port)
        drv = connect_driver(port)

        gw.send(json.dumps({"type": "message", "text": "bad word"}))

        hook_msg = recv_msg(hook, timeout=3)
        assert hook_msg is not None and hook_msg["type"] == "hook"

        hook.send(json.dumps({
            "type": "hook_result",
            "id": hook_msg["id"],
            "action": "modify",
            "payload": {"text": "[censored]"},
        }))

        msg = recv_msg(drv, timeout=3)
        assert msg is not None and msg["type"] == "message"
        assert msg["text"] == "[censored]", f"expected [censored], got {msg['text']}"
        print("  PASS: test_modifying_hook_modify")
    finally:
        if proc:
            proc.terminate()
            proc.wait(timeout=5)
        shutil.rmtree(home, ignore_errors=True)


def test_modifying_hook_block():
    """before_message hook with 'block' action prevents message delivery."""
    port = get_free_port()
    home = setup_test_home(port)
    proc = None
    try:
        proc = start_kernel(home, port)
        hook = connect_hook(port, "filter", [{"event": "before_message", "priority": 10}])
        gw = connect_gateway(port)
        drv = connect_driver(port)

        gw.send(json.dumps({"type": "message", "text": "evil"}))

        hook_msg = recv_msg(hook, timeout=3)
        assert hook_msg is not None and hook_msg["type"] == "hook"

        hook.send(json.dumps({
            "type": "hook_result",
            "id": hook_msg["id"],
            "action": "block",
            "reason": "dangerous",
        }))

        # gateway should receive error
        err = recv_msg(gw, timeout=3)
        assert err is not None and err["type"] == "error"

        # driver should NOT receive message
        no_msg = recv_msg(drv, timeout=1)
        assert no_msg is None, f"driver should not receive message, got {no_msg}"
        print("  PASS: test_modifying_hook_block")
    finally:
        if proc:
            proc.terminate()
            proc.wait(timeout=5)
        shutil.rmtree(home, ignore_errors=True)


def test_session_start_hook():
    """session_start void hook fires when a client joins."""
    port = get_free_port()
    home = setup_test_home(port)
    proc = None
    try:
        proc = start_kernel(home, port)
        hook = connect_hook(port, "logger", [{"event": "session_start", "priority": 0}])

        # joining a session should fire session_start
        _ = connect_gateway(port)

        hook_msg = recv_msg(hook, timeout=3)
        assert hook_msg is not None, "hook subscriber did not receive session_start"
        assert hook_msg["type"] == "hook"
        assert hook_msg["name"] == "session_start"
        payload = hook_msg.get("payload", {})
        if isinstance(payload, str):
            payload = json.loads(payload)
        assert payload.get("session") == "s1"
        print("  PASS: test_session_start_hook")
    finally:
        if proc:
            proc.terminate()
            proc.wait(timeout=5)
        shutil.rmtree(home, ignore_errors=True)


def test_hook_logger_skill():
    """hook-logger skill writes events to JSONL file."""
    port = get_free_port()
    home = setup_test_home(port)
    proc = None
    try:
        # Rewrite boot.py to spawn hook-logger
        log_file = os.path.join(home, "hooks.jsonl")
        (Path(home) / "boot.py").write_text(
            "import json, sys\n"
            "json.dump({\n"
            f"  'url': 'ws://127.0.0.1:{port}/ws',\n"
            "  'system_prompt': 'hook test prompt',\n"
            f"  'spawn': ['.venv/bin/python3 skills/hook-logger/run.py --log-file {log_file}']\n"
            "}, sys.stdout)\n"
        )
        proc = start_kernel(home, port)
        time.sleep(1)  # let hook-logger connect

        gw = connect_gateway(port)
        drv = connect_driver(port)

        gw.send(json.dumps({"type": "message", "text": "log this"}))
        msg = recv_msg(drv, timeout=3)
        assert msg is not None

        drv.send(json.dumps({"type": "done"}))
        # drain done from gw
        recv_msg(gw, timeout=2)

        # Give hook-logger time to write
        time.sleep(1)

        assert os.path.isfile(log_file), f"log file not created: {log_file}"
        with open(log_file) as f:
            lines = [line.strip() for line in f if line.strip()]
        assert len(lines) >= 1, f"expected at least 1 log entry, got {len(lines)}"

        events = [json.loads(line)["event"] for line in lines]
        # Should have session_start and/or after_message
        assert any(e in ("session_start", "after_message") for e in events), \
            f"expected session_start or after_message in events, got {events}"
        print("  PASS: test_hook_logger_skill")
    finally:
        if proc:
            proc.terminate()
            proc.wait(timeout=5)
        shutil.rmtree(home, ignore_errors=True)


if __name__ == "__main__":
    tests = [
        test_void_hook_fires,
        test_modifying_hook_pass,
        test_modifying_hook_modify,
        test_modifying_hook_block,
        test_session_start_hook,
        test_hook_logger_skill,
    ]
    failed = 0
    for test in tests:
        try:
            test()
        except Exception as exc:
            print(f"  FAIL: {test.__name__}: {exc}", file=sys.stderr)
            failed += 1
    if failed:
        print(f"\n{failed}/{len(tests)} tests failed")
        sys.exit(1)
    print(f"\nAll {len(tests)} tests passed")

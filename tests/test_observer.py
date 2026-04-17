#!/usr/bin/env python3
"""E2E tests for the observer skill — metrics collection via hooks."""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.request
from contextlib import closing
from pathlib import Path

import websocket as ws_client


ROOT = Path(__file__).resolve().parents[1]
OBSERVER_SCRIPT = ROOT / "skills" / "observer" / "run.py"
MODIFYING_HOOK_NAMES = {
    "before_message",
    "before_tool_call",
    "session_start",
    "before_spawn",
}


def get_free_port() -> int:
    with closing(__import__("socket").socket()) as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def setup_test_home(tabula_port: int, observer_port: int) -> str:
    home = tempfile.mkdtemp(prefix="tabula-observer-")
    shutil.copytree(ROOT / "skills", Path(home) / "skills")
    shutil.copytree(ROOT / ".venv", Path(home) / ".venv", dirs_exist_ok=True)
    boot_script = Path(home) / "boot.py"
    boot_script.write_text(
        "import json, sys\n"
        "json.dump({\n"
        f"  'url': 'ws://127.0.0.1:{tabula_port}/ws',\n"
        "  'system_prompt': 'observer test prompt',\n"
        "  'spawn': []\n"
        "}, sys.stdout)\n"
    )
    return home


def start_kernel(home: str, tabula_port: int) -> subprocess.Popen:
    env = os.environ.copy()
    env["TABULA_HOME"] = home
    env["TABULA_URL"] = f"ws://127.0.0.1:{tabula_port}/ws"
    env["TABULA_BOOT"] = f"python3 {home}/boot.py"
    env["TABULA_PROVIDER"] = "mock"

    proc = subprocess.Popen(
        ["go", "run", "./cmd/tabula", "serve"],
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


def start_observer(home: str, tabula_port: int, observer_port: int) -> subprocess.Popen:
    """Start the observer skill as a subprocess."""
    env = os.environ.copy()
    env["TABULA_HOME"] = home
    proc = subprocess.Popen(
        [sys.executable, str(OBSERVER_SCRIPT), "--port", str(observer_port), "--url", f"ws://127.0.0.1:{tabula_port}/ws"],
        cwd=ROOT,
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    deadline = time.time() + 10
    while time.time() < deadline:
        try:
            resp = urllib.request.urlopen(f"http://127.0.0.1:{observer_port}/metrics", timeout=1)
            if resp.status == 200:
                return proc
        except Exception:
            time.sleep(0.3)
    proc.terminate()
    raise RuntimeError("observer did not start")


def get_metrics(observer_port: int) -> dict:
    resp = urllib.request.urlopen(f"http://127.0.0.1:{observer_port}/metrics", timeout=5)
    return json.loads(resp.read())


def connect_gateway(tabula_port: int, session: str = "s1"):
    conn = ws_client.create_connection(f"ws://127.0.0.1:{tabula_port}/ws", timeout=10)
    conn.send(json.dumps({
        "type": "connect",
        "name": "test-gw",
        "sends": ["message", "done"],
        "receives": ["message", "done", "error"],
    }))
    resp = json.loads(conn.recv())
    assert resp["type"] == "connected"
    conn.send(json.dumps({"type": "join", "session": session}))
    conn.settimeout(10)
    resp = json.loads(conn.recv())
    assert resp["type"] == "joined"
    return conn


def connect_driver(tabula_port: int, session: str = "s1"):
    conn = ws_client.create_connection(f"ws://127.0.0.1:{tabula_port}/ws", timeout=5)
    conn.send(json.dumps({
        "type": "connect",
        "name": "test-drv",
        "sends": ["done"],
        "receives": ["message"],
    }))
    resp = json.loads(conn.recv())
    assert resp["type"] == "connected"
    conn.send(json.dumps({"type": "join", "session": session}))
    resp = json.loads(conn.recv())
    assert resp["type"] == "joined"
    conn.settimeout(0.5)
    try:
        while True:
            conn.recv()
    except Exception:
        pass
    conn.settimeout(5)
    return conn


def recv_msg(conn, timeout=3):
    conn.settimeout(timeout)
    try:
        return json.loads(conn.recv())
    except Exception:
        return None


def wait_for(predicate, timeout=5.0, interval=0.1):
    deadline = time.time() + timeout
    while time.time() < deadline:
        value = predicate()
        if value:
            return value
        time.sleep(interval)
    return None


def metrics_when_tool_calls(observer_port: int, tool_name: str, calls: int) -> dict | None:
    metrics = get_metrics(observer_port)
    tool_metrics = metrics.get("tools", {}).get(tool_name, {})
    if tool_metrics.get("calls") == calls:
        return metrics
    return None


def metrics_when_spawns_exist(observer_port: int) -> dict | None:
    metrics = get_metrics(observer_port)
    if metrics.get("spawns"):
        return metrics
    return None


# --- Tests ---


def test_observer_subscribes_only_to_observability_hooks():
    """Observer must not be a participant in policy/modifying hooks."""
    import importlib.util

    spec = importlib.util.spec_from_file_location("observer_run", OBSERVER_SCRIPT)
    observer = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(observer)

    events = {entry["event"] for entry in observer.HOOK_EVENTS}
    assert events == {"after_message", "after_tool_call", "session_end", "after_spawn"}
    assert events.isdisjoint(MODIFYING_HOOK_NAMES)


def test_metrics_endpoint_returns_json():
    """Observer /metrics returns valid JSON with expected top-level keys."""
    port = get_free_port()
    obs_port = get_free_port()
    home = setup_test_home(port, obs_port)
    kernel_proc = None
    obs_proc = None
    try:
        kernel_proc = start_kernel(home, port)
        obs_proc = start_observer(home, port, obs_port)
        time.sleep(1)  # let observer connect and subscribe to hooks

        metrics = get_metrics(obs_port)
        assert "uptime_sec" in metrics
        assert "sessions" in metrics
        assert "tools" in metrics
        assert "spawns" in metrics
        assert isinstance(metrics["uptime_sec"], (int, float))
        print("  PASS: test_metrics_endpoint_returns_json")
    finally:
        if obs_proc:
            obs_proc.terminate()
            obs_proc.wait(timeout=5)
        if kernel_proc:
            kernel_proc.terminate()
            kernel_proc.wait(timeout=5)
        shutil.rmtree(home, ignore_errors=True)


def test_session_message_count():
    """Observer tracks message count per session."""
    port = get_free_port()
    obs_port = get_free_port()
    home = setup_test_home(port, obs_port)
    kernel_proc = None
    obs_proc = None
    try:
        kernel_proc = start_kernel(home, port)
        obs_proc = start_observer(home, port, obs_port)
        time.sleep(1)  # let observer connect and subscribe to hooks

        gw = connect_gateway(port)
        drv = connect_driver(port)

        # Send two messages
        for text in ["hello", "world"]:
            gw.send(json.dumps({"type": "message", "text": text}))
            msg = recv_msg(drv)
            assert msg is not None
            drv.send(json.dumps({"type": "done"}))
            recv_msg(gw, timeout=2)

        time.sleep(0.5)  # let observer process hooks

        metrics = get_metrics(obs_port)
        session = metrics["sessions"].get("s1", {})
        assert session.get("message_count") == 2, \
            f"expected message_count=2, got {session.get('message_count')}"
        print("  PASS: test_session_message_count")
    finally:
        if obs_proc:
            obs_proc.terminate()
            obs_proc.wait(timeout=5)
        if kernel_proc:
            kernel_proc.terminate()
            kernel_proc.wait(timeout=5)
        shutil.rmtree(home, ignore_errors=True)


def test_session_latency_tracked():
    """Observer tracks message activity without entering before_message path."""
    port = get_free_port()
    obs_port = get_free_port()
    home = setup_test_home(port, obs_port)
    kernel_proc = None
    obs_proc = None
    try:
        kernel_proc = start_kernel(home, port)
        obs_proc = start_observer(home, port, obs_port)
        time.sleep(1)  # let observer connect and subscribe to hooks

        gw = connect_gateway(port)
        drv = connect_driver(port)

        gw.send(json.dumps({"type": "message", "text": "ping"}))
        msg = recv_msg(drv)
        assert msg is not None
        drv.send(json.dumps({"type": "done"}))
        recv_msg(gw, timeout=2)

        time.sleep(0.5)

        metrics = get_metrics(obs_port)
        session = metrics["sessions"].get("s1", {})
        assert "last_message_at" in session, "last_message_at not tracked"
        assert "avg_latency_ms" not in session, "observer should not depend on before_message latency hooks"
        print("  PASS: test_session_activity_tracked_without_latency_hook")
    finally:
        if obs_proc:
            obs_proc.terminate()
            obs_proc.wait(timeout=5)
        if kernel_proc:
            kernel_proc.terminate()
            kernel_proc.wait(timeout=5)
        shutil.rmtree(home, ignore_errors=True)


def test_session_clients_tracked():
    """Observer tracks which clients connected to a session."""
    port = get_free_port()
    obs_port = get_free_port()
    home = setup_test_home(port, obs_port)
    kernel_proc = None
    obs_proc = None
    try:
        kernel_proc = start_kernel(home, port)
        obs_proc = start_observer(home, port, obs_port)
        time.sleep(1)  # let observer connect and subscribe to hooks

        gw = connect_gateway(port)
        drv = connect_driver(port)

        gw.send(json.dumps({"type": "message", "text": "hi"}))
        drv_msg = recv_msg(drv)
        if drv_msg:
            drv.send(json.dumps({"type": "done"}))
            recv_msg(gw, timeout=2)

        time.sleep(0.5)

        metrics = get_metrics(obs_port)
        session = metrics["sessions"].get("s1", {})
        clients = session.get("clients", [])
        assert "test-gw" in clients, f"expected test-gw in clients, got {clients}"
        assert "test-drv" in clients, f"expected test-drv in clients, got {clients}"
        print("  PASS: test_session_clients_tracked")
    finally:
        if obs_proc:
            obs_proc.terminate()
            obs_proc.wait(timeout=5)
        if kernel_proc:
            kernel_proc.terminate()
            kernel_proc.wait(timeout=5)
        shutil.rmtree(home, ignore_errors=True)


def test_tool_call_count_tracked():
    """Observer tracks tool completions from after_tool_call only."""
    port = get_free_port()
    obs_port = get_free_port()
    home = setup_test_home(port, obs_port)
    kernel_proc = None
    obs_proc = None
    conn = None
    try:
        kernel_proc = start_kernel(home, port)
        obs_proc = start_observer(home, port, obs_port)
        time.sleep(1)  # let observer connect and subscribe to hooks

        conn = ws_client.create_connection(f"ws://127.0.0.1:{port}/ws", timeout=5)
        conn.send(json.dumps({
            "type": "connect",
            "name": "tool-client",
            "sends": ["tool_use"],
            "receives": ["tool_result", "init"],
        }))
        assert json.loads(conn.recv())["type"] == "connected"
        conn.send(json.dumps({"type": "join", "session": "main"}))
        assert json.loads(conn.recv())["type"] == "joined"
        assert json.loads(conn.recv())["type"] == "init"

        conn.send(json.dumps({
            "type": "tool_use",
            "id": "exec-1",
            "name": "EXEC",
            "input": {"command": "printf observer-tool"},
        }))
        result = recv_msg(conn, timeout=10)
        assert result is not None and result["type"] == "tool_result", f"unexpected tool result: {result}"

        metrics = wait_for(lambda: metrics_when_tool_calls(obs_port, "EXEC", 1), timeout=5)
        assert metrics is not None, "observer did not record EXEC completion"
        exec_metrics = metrics["tools"]["EXEC"]
        assert exec_metrics["calls"] == 1
        assert exec_metrics["errors"] == 0
        print("  PASS: test_tool_call_count_tracked")
    finally:
        if conn:
            conn.close()
        if obs_proc:
            obs_proc.terminate()
            obs_proc.wait(timeout=5)
        if kernel_proc:
            kernel_proc.terminate()
            kernel_proc.wait(timeout=5)
        shutil.rmtree(home, ignore_errors=True)


def test_spawn_tracking():
    """Observer tracks spawn events from after_spawn plus snapshot reconciliation.

    The mock driver uses SPAWN tool, which fires after_spawn; /sessions fills alive state.
    We boot with the mock driver spawned, then send it a message to trigger spawning.
    """
    port = get_free_port()
    obs_port = get_free_port()
    home = setup_test_home(port, obs_port)
    # Boot script spawns mock driver which will use SPAWN tool.
    (Path(home) / "boot.py").write_text(
        "import json, sys\n"
        "json.dump({\n"
        f"  'url': 'ws://127.0.0.1:{port}/ws',\n"
        "  'system_prompt': 'spawn test',\n"
        f"  'spawn': ['.venv/bin/python3 skills/driver-mock/run.py --session main']\n"
        "}, sys.stdout)\n"
    )
    kernel_proc = None
    obs_proc = None
    try:
        kernel_proc = start_kernel(home, port)
        obs_proc = start_observer(home, port, obs_port)
        time.sleep(1)  # let observer connect and subscribe to hooks

        # Connect a gateway to send a message to the mock driver.
        gw = connect_gateway(port, "main")
        gw.send(json.dumps({"type": "message", "text": "spawn subagents"}))
        # Drain done/error
        recv_msg(gw, timeout=10)

        metrics = wait_for(lambda: metrics_when_spawns_exist(obs_port), timeout=8, interval=0.2)
        assert metrics is not None, "observer did not record spawn metrics"
        # Find any spawn entry — the mock driver spawns subagent-mock commands
        assert len(metrics["spawns"]) > 0, \
            f"no spawns tracked, metrics: {json.dumps(metrics, indent=2)}"
        assert any(info.get("alive") for info in metrics["spawns"].values())
        print("  PASS: test_spawn_tracking")
    finally:
        if obs_proc:
            obs_proc.terminate()
            obs_proc.wait(timeout=5)
        if kernel_proc:
            kernel_proc.terminate()
            kernel_proc.wait(timeout=5)
        shutil.rmtree(home, ignore_errors=True)


def test_uptime_increases():
    """Observer uptime_sec increases over time."""
    port = get_free_port()
    obs_port = get_free_port()
    home = setup_test_home(port, obs_port)
    kernel_proc = None
    obs_proc = None
    try:
        kernel_proc = start_kernel(home, port)
        obs_proc = start_observer(home, port, obs_port)
        time.sleep(1)  # let observer connect and subscribe to hooks

        m1 = get_metrics(obs_port)
        time.sleep(1.5)
        m2 = get_metrics(obs_port)

        assert m2["uptime_sec"] > m1["uptime_sec"], \
            f"uptime should increase: {m1['uptime_sec']} -> {m2['uptime_sec']}"
        print("  PASS: test_uptime_increases")
    finally:
        if obs_proc:
            obs_proc.terminate()
            obs_proc.wait(timeout=5)
        if kernel_proc:
            kernel_proc.terminate()
            kernel_proc.wait(timeout=5)
        shutil.rmtree(home, ignore_errors=True)


def test_multiple_sessions():
    """Observer tracks multiple sessions independently."""
    port = get_free_port()
    obs_port = get_free_port()
    home = setup_test_home(port, obs_port)
    kernel_proc = None
    obs_proc = None
    try:
        kernel_proc = start_kernel(home, port)
        obs_proc = start_observer(home, port, obs_port)
        time.sleep(1)  # let observer connect and subscribe to hooks

        gw1 = connect_gateway(port, "session-a")
        drv1 = connect_driver(port, "session-a")
        gw2 = connect_gateway(port, "session-b")
        drv2 = connect_driver(port, "session-b")

        # Send one message to session-a
        gw1.send(json.dumps({"type": "message", "text": "msg-a"}))
        recv_msg(drv1)
        drv1.send(json.dumps({"type": "done"}))
        recv_msg(gw1, timeout=2)

        # Send two messages to session-b
        for text in ["msg-b1", "msg-b2"]:
            gw2.send(json.dumps({"type": "message", "text": text}))
            recv_msg(drv2)
            drv2.send(json.dumps({"type": "done"}))
            recv_msg(gw2, timeout=2)

        time.sleep(0.5)

        metrics = get_metrics(obs_port)
        sa = metrics["sessions"].get("session-a", {})
        sb = metrics["sessions"].get("session-b", {})
        assert sa.get("message_count") == 1, f"session-a count: {sa.get('message_count')}"
        assert sb.get("message_count") == 2, f"session-b count: {sb.get('message_count')}"
        print("  PASS: test_multiple_sessions")
    finally:
        if obs_proc:
            obs_proc.terminate()
            obs_proc.wait(timeout=5)
        if kernel_proc:
            kernel_proc.terminate()
            kernel_proc.wait(timeout=5)
        shutil.rmtree(home, ignore_errors=True)


if __name__ == "__main__":
    tests = [
        test_metrics_endpoint_returns_json,
        test_session_message_count,
        test_session_latency_tracked,
        test_session_clients_tracked,
        test_tool_call_count_tracked,
        test_spawn_tracking,
        test_uptime_increases,
        test_multiple_sessions,
    ]
    failed = 0
    for test in tests:
        try:
            test()
        except Exception as exc:
            print(f"  FAIL: {test.__name__}: {exc}", file=sys.stderr)
            import traceback
            traceback.print_exc(file=sys.stderr)
            failed += 1
    if failed:
        print(f"\n{failed}/{len(tests)} tests failed")
        sys.exit(1)
    print(f"\nAll {len(tests)} tests passed")

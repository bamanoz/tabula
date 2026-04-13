#!/usr/bin/env python3
"""E2E tests for the MCP bridge skill."""

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


# ---------------------------------------------------------------------------
# Mock MCP server (stdio) — a minimal JSON-RPC server over stdin/stdout
# ---------------------------------------------------------------------------

MOCK_MCP_SERVER_SCRIPT = r'''#!/usr/bin/env python3
"""Minimal MCP server over stdio for testing."""
import json, sys

def respond(req_id, result):
    msg = {"jsonrpc": "2.0", "id": req_id, "result": result}
    sys.stdout.write(json.dumps(msg) + "\n")
    sys.stdout.flush()

TOOLS = [
    {
        "name": "echo",
        "description": "Echo back the input text",
        "inputSchema": {
            "type": "object",
            "properties": {"text": {"type": "string"}},
            "required": ["text"],
        },
    },
    {
        "name": "add",
        "description": "Add two numbers",
        "inputSchema": {
            "type": "object",
            "properties": {
                "a": {"type": "number"},
                "b": {"type": "number"},
            },
            "required": ["a", "b"],
        },
    },
]

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    msg = json.loads(line)
    method = msg.get("method", "")
    req_id = msg.get("id")

    if method == "initialize":
        respond(req_id, {
            "protocolVersion": "2025-06-18",
            "capabilities": {"tools": {}},
            "serverInfo": {"name": "mock-mcp", "version": "1.0.0"},
        })
    elif method == "notifications/initialized":
        pass  # no response for notifications
    elif method == "tools/list":
        respond(req_id, {"tools": TOOLS})
    elif method == "tools/call":
        params = msg.get("params", {})
        name = params.get("name", "")
        args = params.get("arguments", {})
        if name == "echo":
            respond(req_id, {"content": [{"type": "text", "text": args.get("text", "")}]})
        elif name == "add":
            result = args.get("a", 0) + args.get("b", 0)
            respond(req_id, {"content": [{"type": "text", "text": str(result)}]})
        else:
            respond(req_id, {"content": [{"type": "text", "text": f"unknown tool: {name}"}]})
    elif req_id is not None:
        sys.stdout.write(json.dumps({
            "jsonrpc": "2.0", "id": req_id,
            "error": {"code": -32601, "message": f"unknown method: {method}"},
        }) + "\n")
        sys.stdout.flush()
'''


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def get_free_port() -> int:
    with closing(__import__("socket").socket()) as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def create_mock_mcp_home(tabula_port: int | None = None) -> str:
    """Create a temp TABULA_HOME with MCP config pointing to a mock server."""
    home = tempfile.mkdtemp(prefix="tabula-mcp-e2e-")
    hp = Path(home)

    # Copy skills
    shutil.copytree(ROOT / "skills", hp / "skills")
    shutil.copytree(ROOT / ".venv", hp / ".venv", dirs_exist_ok=True)

    # Write mock MCP server script
    mock_server = hp / "mock_mcp_server.py"
    mock_server.write_text(MOCK_MCP_SERVER_SCRIPT)

    # MCP config
    mcp_dir = hp / "mcp"
    mcp_dir.mkdir()
    (mcp_dir / "servers.json").write_text(json.dumps({
        "servers": {
            "mock": {
                "transport": "stdio",
                "command": [sys.executable, str(mock_server)],
            },
        },
    }))

    if tabula_port is not None:
        (hp / "tabula.yaml").write_text("boot: python3 boot.py\n")
        (hp / "boot.py").write_text(
            "import json, sys\n"
            "json.dump({\n"
            f"  'url': 'ws://127.0.0.1:{tabula_port}/ws',\n"
            "  'system_prompt': 'mcp test prompt',\n"
            "  'spawn': ['.venv/bin/python3 skills/driver-mock/run.py']\n"
            "}, sys.stdout)\n"
        )

    return home


def run_mcp_cli(home: str, *args: str, timeout: int = 15) -> subprocess.CompletedProcess:
    """Run skills/mcp/run.py with the given arguments."""
    env = os.environ.copy()
    env["TABULA_HOME"] = home
    return subprocess.run(
        [sys.executable, str(ROOT / "skills" / "mcp" / "run.py"), *args],
        cwd=ROOT,
        env=env,
        capture_output=True,
        text=True,
        timeout=timeout,
    )


# ---------------------------------------------------------------------------
# Tests — MCP client layer (no kernel needed)
# ---------------------------------------------------------------------------

def test_discover():
    """discover should list all tools from mock server."""
    home = create_mock_mcp_home()
    try:
        result = run_mcp_cli(home, "discover")
        assert result.returncode == 0, f"discover failed: {result.stderr}"
        data = json.loads(result.stdout)
        assert "mock" in data
        tools = data["mock"]
        names = [t["name"] for t in tools]
        assert "echo" in names
        assert "add" in names
    finally:
        shutil.rmtree(home, ignore_errors=True)


def test_list():
    """list should show tools for a specific server."""
    home = create_mock_mcp_home()
    try:
        result = run_mcp_cli(home, "list", "mock")
        assert result.returncode == 0, f"list failed: {result.stderr}"
        assert "echo" in result.stdout
        assert "add" in result.stdout
    finally:
        shutil.rmtree(home, ignore_errors=True)


def test_call_echo():
    """call echo tool should return the input text."""
    home = create_mock_mcp_home()
    try:
        result = run_mcp_cli(home, "call", "mock", "echo", '{"text": "hello mcp"}')
        assert result.returncode == 0, f"call failed: {result.stderr}"
        assert "hello mcp" in result.stdout
    finally:
        shutil.rmtree(home, ignore_errors=True)


def test_call_add():
    """call add tool should return the sum."""
    home = create_mock_mcp_home()
    try:
        result = run_mcp_cli(home, "call", "mock", "add", '{"a": 17, "b": 25}')
        assert result.returncode == 0, f"call failed: {result.stderr}"
        assert "42" in result.stdout
    finally:
        shutil.rmtree(home, ignore_errors=True)


def test_call_unknown_server():
    """call with unknown server should fail gracefully."""
    home = create_mock_mcp_home()
    try:
        result = run_mcp_cli(home, "call", "nonexistent", "echo", '{"text": "hi"}')
        assert result.returncode != 0
    finally:
        shutil.rmtree(home, ignore_errors=True)


def test_no_config():
    """discover with no config should return empty."""
    home = tempfile.mkdtemp(prefix="tabula-mcp-noconfig-")
    try:
        env = os.environ.copy()
        env["TABULA_HOME"] = home
        result = subprocess.run(
            [sys.executable, str(ROOT / "skills" / "mcp" / "run.py"), "discover"],
            cwd=ROOT,
            env=env,
            capture_output=True,
            text=True,
            timeout=10,
        )
        assert result.returncode == 0
        data = json.loads(result.stdout)
        assert data == {}
    finally:
        shutil.rmtree(home, ignore_errors=True)


# ---------------------------------------------------------------------------
# Tests — E2E with kernel (EXEC tool calls MCP bridge)
# ---------------------------------------------------------------------------

def start_kernel(home: str, tabula_port: int) -> subprocess.Popen:
    env = os.environ.copy()
    env["TABULA_HOME"] = home
    env["TABULA_URL"] = f"ws://127.0.0.1:{tabula_port}/ws"
    env["TABULA_PROVIDER"] = "mock"
    env["TABULA_VERBOSE"] = "1"
    env["TABULA_MOCK_SUBAGENTS"] = "0"
    env["TABULA_MOCK_TURNS"] = "1"
    env["TABULA_MOCK_SLEEP_MS"] = "5"

    proc = subprocess.Popen(
        ["go", "run", "./cmd/tabula"],
        cwd=ROOT,
        env=env,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
    )
    deadline = time.time() + 15
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


def connect_tool_client(tabula_port: int):
    conn = ws_client.create_connection(f"ws://127.0.0.1:{tabula_port}/ws", timeout=5)
    conn.send(
        json.dumps({
            "type": "connect",
            "name": "test-mcp-client",
            "sends": ["tool_use"],
            "receives": ["tool_result", "init"],
        })
    )
    json.loads(conn.recv())  # connected
    conn.send(json.dumps({"type": "join", "session": "main"}))
    json.loads(conn.recv())  # joined
    init_msg = json.loads(conn.recv())
    assert init_msg["type"] == "init"
    return conn


def recv_json(conn, timeout=10):
    conn.settimeout(timeout)
    return json.loads(conn.recv())


def test_exec_mcp_call():
    """EXEC tool should be able to call MCP bridge and get results."""
    tabula_port = get_free_port()
    home = create_mock_mcp_home(tabula_port)
    proc = None
    conn = None
    try:
        proc = start_kernel(home, tabula_port)
        conn = connect_tool_client(tabula_port)
        time.sleep(0.5)

        # Use EXEC to call MCP bridge
        mcp_cmd = f"{sys.executable} skills/mcp/run.py call mock echo " + """'{"text": "kernel e2e test"}'"""
        conn.send(json.dumps({
            "type": "tool_use",
            "id": "exec-mcp-1",
            "name": "EXEC",
            "input": {"command": mcp_cmd},
        }))

        result = recv_json(conn, timeout=15)
        assert result["type"] == "tool_result", f"unexpected: {result}"
        assert result["id"] == "exec-mcp-1"
        assert "kernel e2e test" in result["output"]
    finally:
        if conn is not None:
            conn.close()
        if proc is not None:
            proc.terminate()
            proc.wait(timeout=10)
        shutil.rmtree(home, ignore_errors=True)


def test_exec_mcp_add():
    """EXEC MCP add tool should return correct result."""
    tabula_port = get_free_port()
    home = create_mock_mcp_home(tabula_port)
    proc = None
    conn = None
    try:
        proc = start_kernel(home, tabula_port)
        conn = connect_tool_client(tabula_port)
        time.sleep(0.5)

        mcp_cmd = f"{sys.executable} skills/mcp/run.py call mock add " + """'{"a": 100, "b": 23}'"""
        conn.send(json.dumps({
            "type": "tool_use",
            "id": "exec-mcp-2",
            "name": "EXEC",
            "input": {"command": mcp_cmd},
        }))

        result = recv_json(conn, timeout=15)
        assert result["type"] == "tool_result"
        assert result["id"] == "exec-mcp-2"
        assert "123" in result["output"]
    finally:
        if conn is not None:
            conn.close()
        if proc is not None:
            proc.terminate()
            proc.wait(timeout=10)
        shutil.rmtree(home, ignore_errors=True)


# ---------------------------------------------------------------------------
# Tests — Pool daemon (persistent MCP servers)
# ---------------------------------------------------------------------------

def start_pool_daemon(home: str) -> subprocess.Popen:
    """Start the MCP pool daemon as a subprocess."""
    env = os.environ.copy()
    env["TABULA_HOME"] = home
    proc = subprocess.Popen(
        [sys.executable, str(ROOT / "skills" / "mcp" / "run.py"), "pool"],
        cwd=ROOT,
        env=env,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
    )
    # Wait for URL file to appear
    url_file = os.path.join(home, "mcp", "pool.url")
    deadline = time.time() + 5
    while time.time() < deadline:
        if os.path.exists(url_file):
            return proc
        time.sleep(0.1)
    proc.terminate()
    proc.wait()
    raise RuntimeError("pool daemon did not start")


def test_pool_discover():
    """discover via pool daemon should return tools."""
    home = create_mock_mcp_home()
    pool_proc = None
    try:
        pool_proc = start_pool_daemon(home)
        result = run_mcp_cli(home, "discover")
        assert result.returncode == 0, f"discover failed: {result.stderr}"
        data = json.loads(result.stdout)
        assert "mock" in data
        names = [t["name"] for t in data["mock"]]
        assert "echo" in names
    finally:
        if pool_proc is not None:
            pool_proc.terminate()
            pool_proc.wait(timeout=5)
        shutil.rmtree(home, ignore_errors=True)


def test_pool_call():
    """call via pool daemon should work and be fast on second call."""
    home = create_mock_mcp_home()
    pool_proc = None
    try:
        pool_proc = start_pool_daemon(home)

        # First call (cold — server starts)
        result1 = run_mcp_cli(home, "call", "mock", "echo", '{"text": "first"}')
        assert result1.returncode == 0, f"first call failed: {result1.stderr}"
        assert "first" in result1.stdout

        # Second call (warm — server already running)
        result2 = run_mcp_cli(home, "call", "mock", "echo", '{"text": "second"}')
        assert result2.returncode == 0, f"second call failed: {result2.stderr}"
        assert "second" in result2.stdout

        # Third call — different tool
        result3 = run_mcp_cli(home, "call", "mock", "add", '{"a": 10, "b": 32}')
        assert result3.returncode == 0, f"add call failed: {result3.stderr}"
        assert "42" in result3.stdout
    finally:
        if pool_proc is not None:
            pool_proc.terminate()
            pool_proc.wait(timeout=5)
        shutil.rmtree(home, ignore_errors=True)


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

if __name__ == "__main__":
    tests = [
        # Client-level tests (fast, no kernel)
        test_discover,
        test_list,
        test_call_echo,
        test_call_add,
        test_call_unknown_server,
        test_no_config,
        # E2E with kernel
        test_exec_mcp_call,
        test_exec_mcp_add,
        # Pool daemon tests
        test_pool_discover,
        test_pool_call,
    ]
    failed = 0
    for test in tests:
        name = test.__name__
        try:
            test()
            print(f"  PASS  {name}")
        except Exception as exc:
            print(f"  FAIL  {name}: {exc}", file=sys.stderr)
            failed += 1
    if failed:
        print(f"\n{failed}/{len(tests)} tests failed", file=sys.stderr)
        sys.exit(1)
    print(f"\nPASS ({len(tests)} tests)")

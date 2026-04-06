#!/usr/bin/env python3
"""
E2E test for subagent: kernel + subagent-anthropic + cross-session routing.

Starts kernel, connects a fake parent client, spawns subagent,
verifies result arrives via cross-session message routing.

Uses a local mock Anthropic API to avoid real API calls.
"""

import json
import os
import signal
import socket
import subprocess
import sys
import threading
import time
from http.server import HTTPServer, BaseHTTPRequestHandler

SOCKET = "/tmp/tabula-e2e-test.sock"
TEST_HOME = "/tmp/tabula-e2e-home"
MOCK_API_PORT = 18932
TABULA_BIN = os.path.expanduser("~/.tabula/bin/tabula")


# --- Mock Anthropic API ---

class MockAnthropicHandler(BaseHTTPRequestHandler):
    """Returns a simple text response, no tool calls."""

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(length)) if length else {}

        messages = body.get("messages", [])
        last_user = ""
        for m in reversed(messages):
            if m.get("role") == "user":
                if isinstance(m["content"], str):
                    last_user = m["content"]
                break

        response = {
            "id": "mock-1",
            "type": "message",
            "role": "assistant",
            "content": [
                {"type": "text", "text": f"mock result for: {last_user[:80]}"}
            ],
            "model": "mock",
            "stop_reason": "end_turn",
            "usage": {"input_tokens": 10, "output_tokens": 10},
        }
        payload = json.dumps(response).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, *args):
        pass  # silence logs


def start_mock_api():
    server = HTTPServer(("127.0.0.1", MOCK_API_PORT), MockAnthropicHandler)
    t = threading.Thread(target=server.serve_forever, daemon=True)
    t.start()
    time.sleep(0.3)  # wait for server to be ready
    return server


# --- Helpers ---

def send_msg(s, msg):
    s.sendall((json.dumps(msg) + "\n").encode())


def recv_msg(s, timeout=5):
    s.settimeout(timeout)
    buf = b""
    while True:
        chunk = s.recv(4096)
        if not chunk:
            raise ConnectionError("disconnected")
        buf += chunk
        nl = buf.find(b"\n")
        if nl >= 0:
            return json.loads(buf[:nl])


def setup_test_home():
    os.makedirs(TEST_HOME, exist_ok=True)
    os.makedirs(os.path.join(TEST_HOME, "skills", "subagent-anthropic"), exist_ok=True)

    # boot.py — no auto-spawn, just kernel config
    with open(os.path.join(TEST_HOME, "boot.py"), "w") as f:
        f.write(f"""import json, sys
json.dump({{"socket": "{SOCKET}", "system_prompt": "test", "spawn": []}}, sys.stdout)
""")

    with open(os.path.join(TEST_HOME, "tabula.yaml"), "w") as f:
        f.write("boot: python3 boot.py\n")

    # Copy subagent skill
    src_skill = os.path.join(os.path.dirname(__file__), "..", "skills", "subagent-anthropic", "run.py")
    src_skill = os.path.normpath(src_skill)
    dst_skill = os.path.join(TEST_HOME, "skills", "subagent-anthropic", "run.py")
    with open(src_skill) as f:
        content = f.read()
    with open(dst_skill, "w") as f:
        f.write(content)


def wait_for_socket(path, timeout=5):
    deadline = time.time() + timeout
    while time.time() < deadline:
        if os.path.exists(path):
            return True
        time.sleep(0.1)
    return False


# --- Tests ---

def test_subagent_initial_task():
    """Spawn subagent with a task, verify result arrives in parent session."""
    print("TEST: subagent initial task... ", end="", flush=True)

    # Connect as parent client, join "main" session
    parent = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    parent.connect(SOCKET)
    send_msg(parent, {"type": "connect", "name": "test-parent",
                       "sends": ["message", "tool_use"], "receives": ["message", "tool_result", "init"]})
    recv_msg(parent)  # connected
    send_msg(parent, {"type": "join", "session": "main"})
    recv_msg(parent)  # joined
    # drain init message
    try:
        recv_msg(parent, timeout=1)
    except socket.timeout:
        pass

    # Spawn subagent via kernel SPAWN tool
    send_msg(parent, {
        "type": "tool_use",
        "id": "spawn-1",
        "name": "SPAWN",
        "input": {
            "command": f"python3 skills/subagent-anthropic/run.py --id task_42 --parent-session main --task 'List files in /tmp' --timeout 5"
        },
    })
    spawn_result = recv_msg(parent, timeout=5)
    assert spawn_result.get("type") == "tool_result", f"expected tool_result, got: {spawn_result}"
    pid = None
    output = spawn_result.get("output", "")
    if "pid" in output.lower():
        # extract pid from output like "pid: 12345" or similar
        for word in output.split():
            if word.isdigit():
                pid = int(word)
                break
    print(f"(spawned pid={pid}) ", end="", flush=True)

    # Wait for subagent result message
    try:
        msg = recv_msg(parent, timeout=10)
        assert msg.get("id") == "task_42", f"wrong id: {msg}"
        assert "mock result for:" in msg.get("text", ""), f"unexpected text: {msg}"
        print("PASS")
    except socket.timeout:
        print("FAIL (timeout waiting for subagent result)")
        parent.close()
        return False
    except Exception as e:
        print(f"FAIL ({e})")
        parent.close()
        return False

    parent.close()
    return True


def test_subagent_followup():
    """Spawn subagent, get initial result, send follow-up, get second result."""
    print("TEST: subagent follow-up... ", end="", flush=True)

    parent = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    parent.connect(SOCKET)
    send_msg(parent, {"type": "connect", "name": "test-parent2",
                       "sends": ["message", "tool_use"], "receives": ["message", "tool_result", "init"]})
    recv_msg(parent)
    send_msg(parent, {"type": "join", "session": "main"})
    recv_msg(parent)
    try:
        recv_msg(parent, timeout=1)
    except socket.timeout:
        pass

    # Spawn subagent
    send_msg(parent, {
        "type": "tool_use",
        "id": "spawn-2",
        "name": "SPAWN",
        "input": {
            "command": f"python3 skills/subagent-anthropic/run.py --id followup_1 --parent-session main --task 'Initial task' --timeout 10"
        },
    })
    recv_msg(parent, timeout=5)  # tool_result

    # Wait for initial result
    try:
        msg = recv_msg(parent, timeout=10)
        assert msg.get("id") == "followup_1", f"wrong id: {msg}"
        print(f"(initial ok) ", end="", flush=True)
    except socket.timeout:
        print("FAIL (timeout on initial)")
        parent.close()
        return False

    # Send follow-up to subagent session
    send_msg(parent, {
        "type": "message",
        "session": "subagent-followup_1",
        "text": "Now do a second thing",
    })

    # Wait for follow-up result
    try:
        msg2 = recv_msg(parent, timeout=10)
        assert msg2.get("id") == "followup_1", f"wrong id on followup: {msg2}"
        assert "mock result for:" in msg2.get("text", ""), f"unexpected followup text: {msg2}"
        print("PASS")
    except socket.timeout:
        print("FAIL (timeout on followup)")
        parent.close()
        return False
    except Exception as e:
        print(f"FAIL ({e})")
        parent.close()
        return False

    parent.close()
    return True


def test_subagent_idle_timeout():
    """Spawn subagent with short timeout, verify it exits."""
    print("TEST: subagent idle timeout... ", end="", flush=True)

    parent = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    parent.connect(SOCKET)
    send_msg(parent, {"type": "connect", "name": "test-parent3",
                       "sends": ["message", "tool_use"], "receives": ["message", "tool_result", "init"]})
    recv_msg(parent)
    send_msg(parent, {"type": "join", "session": "main"})
    recv_msg(parent)
    try:
        recv_msg(parent, timeout=1)
    except socket.timeout:
        pass

    # Spawn with 3s timeout
    send_msg(parent, {
        "type": "tool_use",
        "id": "spawn-3",
        "name": "SPAWN",
        "input": {
            "command": f"python3 skills/subagent-anthropic/run.py --id timeout_1 --parent-session main --task 'Quick task' --timeout 3"
        },
    })
    spawn_result = recv_msg(parent, timeout=5)
    pid = None
    output = spawn_result.get("output", "")
    for word in output.split():
        if word.isdigit():
            pid = int(word)
            break

    # Wait for initial result
    try:
        recv_msg(parent, timeout=10)
    except socket.timeout:
        print("FAIL (no initial result)")
        parent.close()
        return False

    # Wait for timeout + buffer
    time.sleep(5)

    # Check process is dead via LIST
    send_msg(parent, {
        "type": "tool_use",
        "id": "list-1",
        "name": "LIST",
        "input": {},
    })
    list_result = recv_msg(parent, timeout=5)
    output = list_result.get("output", "")

    # The subagent process should be dead (not alive)
    if pid and f"pid={pid}" in output and "alive=true" in output:
        print(f"FAIL (pid {pid} still alive)")
        parent.close()
        return False

    print("PASS")
    parent.close()
    return True


def test_parallel_subagents():
    """Spawn two subagents in parallel, verify both results arrive with correct ids."""
    print("TEST: parallel subagents... ", end="", flush=True)

    parent = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    parent.connect(SOCKET)
    send_msg(parent, {"type": "connect", "name": "test-parent4",
                       "sends": ["message", "tool_use"], "receives": ["message", "tool_result", "init"]})
    recv_msg(parent)
    send_msg(parent, {"type": "join", "session": "main"})
    recv_msg(parent)
    try:
        recv_msg(parent, timeout=1)
    except socket.timeout:
        pass

    # Spawn two subagents
    for agent_id in ["par_a", "par_b"]:
        send_msg(parent, {
            "type": "tool_use",
            "id": f"spawn-{agent_id}",
            "name": "SPAWN",
            "input": {
                "command": f"python3 skills/subagent-anthropic/run.py --id {agent_id} --parent-session main --task 'Task for {agent_id}' --timeout 5"
            },
        })
        recv_msg(parent, timeout=5)  # tool_result

    # Collect both results
    results = {}
    try:
        for _ in range(2):
            msg = recv_msg(parent, timeout=15)
            results[msg.get("id")] = msg.get("text", "")
    except socket.timeout:
        print(f"FAIL (only got {len(results)} results)")
        parent.close()
        return False

    if "par_a" not in results or "par_b" not in results:
        print(f"FAIL (missing ids, got: {list(results.keys())})")
        parent.close()
        return False

    assert "par_a" in results["par_a"], f"par_a result doesn't mention task: {results['par_a']}"
    assert "par_b" in results["par_b"], f"par_b result doesn't mention task: {results['par_b']}"

    print("PASS")
    parent.close()
    return True


def main():
    # Cleanup
    try:
        os.unlink(SOCKET)
    except FileNotFoundError:
        pass

    # Setup
    setup_test_home()
    mock_server = start_mock_api()

    # Start kernel
    env = os.environ.copy()
    env["TABULA_HOME"] = TEST_HOME
    env["TABULA_SOCKET"] = SOCKET
    env["ANTHROPIC_BASE_URL"] = f"http://127.0.0.1:{MOCK_API_PORT}"
    env["ANTHROPIC_API_KEY"] = "mock-key"
    env["no_proxy"] = "*"  # bypass macOS system proxy for local mock

    proc = subprocess.Popen(
        [TABULA_BIN, "-v"],
        env=env,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
    )

    if not wait_for_socket(SOCKET):
        print("FAIL: kernel socket not created")
        proc.terminate()
        return 1

    print(f"Kernel started (pid={proc.pid}), running tests...\n")

    passed = 0
    failed = 0
    tests = [
        test_subagent_initial_task,
        test_subagent_followup,
        test_subagent_idle_timeout,
        test_parallel_subagents,
    ]

    for test in tests:
        try:
            if test():
                passed += 1
            else:
                failed += 1
        except Exception as e:
            print(f"FAIL (exception: {e})")
            failed += 1

    print(f"\n{passed} passed, {failed} failed")

    # Dump kernel stderr for debugging
    proc.terminate()
    proc.wait(timeout=5)
    stderr_out = proc.stderr.read().decode(errors="replace") if proc.stderr else ""
    if stderr_out:
        print("\n--- kernel stderr (last 4000 chars) ---")
        print(stderr_out[-4000:])
        print("--- end ---")
    mock_server.shutdown()
    try:
        os.unlink(SOCKET)
    except FileNotFoundError:
        pass

    return 0 if failed == 0 else 1


if __name__ == "__main__":
    sys.exit(main())

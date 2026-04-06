#!/usr/bin/env python3
"""
Diagnostic test: simulate full subagent flow with verbose logging.

Runs kernel + mock LLM driver + mock gateway to observe message flow
when LLM spawns multiple subagents (like "проведи исследование").
"""

import json
import os
import socket
import subprocess
import sys
import threading
import time
from http.server import HTTPServer, BaseHTTPRequestHandler

SOCKET = "/tmp/tabula-diag.sock"
TEST_HOME = "/tmp/tabula-diag-home"
MOCK_API_PORT = 18940
TABULA_BIN = os.path.expanduser("~/.tabula/bin/tabula")

# Colors
CYAN = "\033[36m"
GREEN = "\033[32m"
YELLOW = "\033[33m"
RED = "\033[31m"
DIM = "\033[2m"
BOLD = "\033[1m"
RESET = "\033[0m"

T0 = time.time()


def ts():
    return f"{DIM}{time.time() - T0:6.2f}s{RESET}"


def log(source, color, msg):
    print(f"  {ts()} {color}{BOLD}[{source}]{RESET} {color}{msg}{RESET}")


# --- Mock Anthropic API ---

call_count = 0
call_lock = threading.Lock()


class MockAnthropicHandler(BaseHTTPRequestHandler):
    """Simulates LLM that spawns subagents on first call, then summarizes."""

    def do_POST(self):
        global call_count
        length = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(length)) if length else {}
        messages = body.get("messages", [])

        with call_lock:
            call_count += 1
            n = call_count

        # Find last user message
        last_user = ""
        for m in reversed(messages):
            if m.get("role") == "user":
                c = m["content"]
                if isinstance(c, str):
                    last_user = c
                elif isinstance(c, list):
                    for b in c:
                        if b.get("type") == "tool_result":
                            last_user = f"[tool_result: {b.get('content', '')[:60]}]"
                        elif b.get("type") == "text":
                            last_user = b["text"]
                break

        log("mock-api", DIM, f"call #{n}, last_user: {last_user[:80]}")

        # Decide response based on conversation state
        has_tool_results = any(
            isinstance(m.get("content"), list) and
            any(b.get("type") == "tool_result" for b in m["content"])
            for m in messages if m.get("role") == "user"
        )

        has_spawn_in_history = any(
            isinstance(m.get("content"), list) and
            any(b.get("type") == "tool_use" and b.get("name") == "SPAWN" for b in m["content"])
            for m in messages if m.get("role") == "assistant"
        )

        # Check if this is a subagent (subagent connects with session name starting with "subagent-")
        # We detect by checking if "subagent" appears in system prompt or by message count
        is_subagent = "subagent" not in body.get("system", "") and len(body.get("system", "")) < 100
        # Actually, detect by checking the user's first message
        first_msg = messages[0]["content"] if messages else ""
        is_subagent = isinstance(first_msg, str) and ("What is OpenClaw" in first_msg or "OpenClaw architecture" in first_msg)

        if is_subagent:
            # Subagent: just return a result
            response = self._text_response(f"Subagent result: I researched '{last_user[:50]}'. OpenClaw is an AI agent framework.")
        elif not has_spawn_in_history:
            # First call: spawn 2 subagents
            response = self._tool_response([
                {"id": "t1", "type": "tool_use", "name": "SPAWN",
                 "input": {"command": "python3 skills/subagent-anthropic/run.py --id research_1 --parent-session main --task 'What is OpenClaw?' --timeout 30"}},
                {"id": "t2", "type": "tool_use", "name": "SPAWN",
                 "input": {"command": "python3 skills/subagent-anthropic/run.py --id research_2 --parent-session main --task 'OpenClaw architecture' --timeout 30"}},
            ], prefix="Launching 2 subagents to research OpenClaw...")
        elif has_tool_results and not any("Subagent result" in str(m) for m in messages):
            # Got SPAWN tool results (PIDs), waiting for subagent messages
            response = self._text_response("Subagents launched! Waiting for results...")
        else:
            # Got subagent results, summarize
            response = self._text_response(
                "## Research Results\n\n"
                "Based on subagent findings:\n\n"
                "**OpenClaw** is an AI agent framework that provides:\n"
                "- Multi-agent orchestration\n"
                "- Tool execution\n"
                "- Streaming responses\n\n"
                "Research complete!"
            )

        payload = json.dumps(response).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def _text_response(self, text):
        return {
            "id": "mock", "type": "message", "role": "assistant",
            "content": [{"type": "text", "text": text}],
            "model": "mock", "stop_reason": "end_turn",
            "usage": {"input_tokens": 10, "output_tokens": 10},
        }

    def _tool_response(self, tools, prefix=""):
        content = []
        if prefix:
            content.append({"type": "text", "text": prefix})
        content.extend(tools)
        return {
            "id": "mock", "type": "message", "role": "assistant",
            "content": content,
            "model": "mock", "stop_reason": "tool_use",
            "usage": {"input_tokens": 10, "output_tokens": 10},
        }

    def log_message(self, *args):
        pass


# --- Helpers ---

def send_msg(s, msg):
    s.sendall((json.dumps(msg) + "\n").encode())


def recv_msg(s, timeout=30):
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


def setup():
    os.makedirs(TEST_HOME, exist_ok=True)
    os.makedirs(os.path.join(TEST_HOME, "skills", "subagent-anthropic"), exist_ok=True)

    with open(os.path.join(TEST_HOME, "boot.py"), "w") as f:
        f.write(f"""import json, sys
json.dump({{"socket": "{SOCKET}", "system_prompt": "You are a helpful assistant. Use subagents for research tasks.", "spawn": []}}, sys.stdout)
""")

    with open(os.path.join(TEST_HOME, "tabula.yaml"), "w") as f:
        f.write("boot: python3 boot.py\n")

    # Copy subagent skill
    src = os.path.normpath(os.path.join(os.path.dirname(__file__), "..", "skills", "subagent-anthropic", "run.py"))
    dst = os.path.join(TEST_HOME, "skills", "subagent-anthropic", "run.py")
    with open(src) as f:
        content = f.read()
    with open(dst, "w") as f:
        f.write(content)


def main():
    try:
        os.unlink(SOCKET)
    except FileNotFoundError:
        pass

    setup()

    # Start mock API
    mock = HTTPServer(("127.0.0.1", MOCK_API_PORT), MockAnthropicHandler)
    threading.Thread(target=mock.serve_forever, daemon=True).start()
    time.sleep(0.3)

    # Start kernel
    env = os.environ.copy()
    env["TABULA_HOME"] = TEST_HOME
    env["TABULA_SOCKET"] = SOCKET
    env["ANTHROPIC_BASE_URL"] = f"http://127.0.0.1:{MOCK_API_PORT}"
    env["ANTHROPIC_API_KEY"] = "mock-key"
    env["no_proxy"] = "*"

    proc = subprocess.Popen([TABULA_BIN, "-v"], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)

    deadline = time.time() + 5
    while time.time() < deadline:
        if os.path.exists(SOCKET):
            break
        time.sleep(0.1)
    else:
        print("FAIL: kernel socket not created")
        proc.terminate()
        return

    log("test", CYAN, "kernel started")

    # --- Connect as LLM driver ---
    drv = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    drv.connect(SOCKET)
    send_msg(drv, {"type": "connect", "name": "driver",
                    "sends": ["stream_start", "stream_delta", "stream_end", "tool_use", "done"],
                    "receives": ["message", "tool_result", "init"]})
    recv_msg(drv)
    send_msg(drv, {"type": "join", "session": "main"})
    recv_msg(drv)
    init_msg = recv_msg(drv)
    log("driver", GREEN, f"connected, got init ({len(init_msg.get('prompt', ''))} bytes prompt)")

    # --- Connect as gateway ---
    gw = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    gw.connect(SOCKET)
    send_msg(gw, {"type": "connect", "name": "gateway",
                   "sends": ["message"],
                   "receives": ["stream_start", "stream_delta", "stream_end", "done", "error"]})
    recv_msg(gw)
    send_msg(gw, {"type": "join", "session": "main"})
    recv_msg(gw)
    log("gateway", YELLOW, "connected")

    # --- Gateway receiver thread ---
    gw_messages = []
    gw_done = threading.Event()

    def gw_receiver():
        buf = b""
        gw.settimeout(60)
        while True:
            try:
                chunk = gw.recv(4096)
                if not chunk:
                    break
                buf += chunk
                while b"\n" in buf:
                    nl = buf.index(b"\n")
                    line = buf[:nl]
                    buf = buf[nl + 1:]
                    if line.strip():
                        msg = json.loads(line)
                        mt = msg.get("type", "?")
                        if mt == "stream_delta":
                            text = msg.get("text", "")
                            log("gateway", YELLOW, f"<< stream_delta: {text[:60]}{'...' if len(text) > 60 else ''}")
                        elif mt == "stream_start":
                            log("gateway", YELLOW, "<< stream_start")
                        elif mt == "stream_end":
                            log("gateway", YELLOW, "<< stream_end")
                        elif mt == "done":
                            log("gateway", YELLOW, "<< done")
                            gw_done.set()
                        else:
                            log("gateway", YELLOW, f"<< {mt}: {json.dumps(msg)[:80]}")
                        gw_messages.append(msg)
            except socket.timeout:
                break
            except Exception as e:
                log("gateway", RED, f"recv error: {e}")
                break

    threading.Thread(target=gw_receiver, daemon=True).start()

    # --- Simulate: user sends message ---
    log("test", CYAN, "user sends: 'проведи исследование, что такое openclaw'")
    send_msg(gw, {"type": "message", "text": "проведи исследование, что такое openclaw"})

    # --- Driver loop: handle messages like real driver ---
    messages_history = []
    system_prompt = init_msg.get("prompt", "")

    def call_llm(msgs):
        import http.client
        body = json.dumps({
            "model": "mock", "max_tokens": 4096,
            "system": system_prompt,
            "messages": msgs,
        }).encode()
        conn = http.client.HTTPConnection("127.0.0.1", MOCK_API_PORT, timeout=10)
        conn.request("POST", "/v1/messages", body=body, headers={
            "Content-Type": "application/json",
            "x-api-key": "mock-key",
            "anthropic-version": "2023-06-01",
        })
        resp = conn.getresponse()
        data = json.loads(resp.read())
        conn.close()
        return data

    def driver_process_turn():
        log("driver", GREEN, f"calling LLM ({len(messages_history)} messages in history)")
        resp = call_llm(messages_history)
        content = resp.get("content", [])
        messages_history.append({"role": "assistant", "content": content})

        # Stream text blocks
        text_parts = [b for b in content if b.get("type") == "text"]
        tool_parts = [b for b in content if b.get("type") == "tool_use"]

        if text_parts:
            text = "\n".join(b["text"] for b in text_parts)
            log("driver", GREEN, f">> stream: {text[:80]}{'...' if len(text) > 80 else ''}")
            send_msg(drv, {"type": "stream_start"})
            send_msg(drv, {"type": "stream_delta", "text": text})
            send_msg(drv, {"type": "stream_end"})

        for tool in tool_parts:
            log("driver", GREEN, f">> tool_use: {tool['name']}({json.dumps(tool['input'])[:60]})")
            send_msg(drv, {"type": "tool_use", "id": tool["id"], "name": tool["name"], "input": tool["input"]})

        if not tool_parts:
            log("driver", GREEN, ">> done")
            send_msg(drv, {"type": "done"})

        return tool_parts

    def driver_wait_tool_results(tools):
        results = []
        for tool in tools:
            msg = recv_msg(drv, timeout=10)
            log("driver", GREEN, f"<< tool_result: id={msg.get('id')}, output={msg.get('output', '')[:60]}")
            results.append(msg)
            messages_history.append({
                "role": "user",
                "content": [{"type": "tool_result", "tool_use_id": msg.get("id"), "content": msg.get("output", "")}],
            })
        return results

    # --- Main driver loop ---
    # 1. Receive user message
    msg = recv_msg(drv, timeout=5)
    log("driver", GREEN, f"<< message: {msg.get('text', '')[:60]}")
    messages_history.append({"role": "user", "content": msg.get("text", "")})

    # 2. Process turn (may involve tool calls)
    tools = driver_process_turn()
    while tools:
        driver_wait_tool_results(tools)
        tools = driver_process_turn()

    # 3. Wait for subagent results (they come as messages)
    log("test", CYAN, "waiting for subagent results...")
    received_results = 0
    expected_results = 2

    while received_results < expected_results:
        try:
            msg = recv_msg(drv, timeout=30)
            mt = msg.get("type", "")
            if mt == "message":
                log("driver", GREEN, f"<< subagent message: id={msg.get('id')}, text={msg.get('text', '')[:60]}")
                messages_history.append({"role": "user", "content": msg.get("text", "")})
                received_results += 1

                # Process this message
                tools = driver_process_turn()
                while tools:
                    driver_wait_tool_results(tools)
                    tools = driver_process_turn()
            else:
                log("driver", GREEN, f"<< unexpected: {mt}")
        except socket.timeout:
            log("test", RED, f"timeout waiting for subagent result ({received_results}/{expected_results} received)")
            break

    # --- Wait for gateway to settle ---
    time.sleep(1)

    # --- Summary ---
    print(f"\n{BOLD}=== Summary ==={RESET}")
    done_count = sum(1 for m in gw_messages if m.get("type") == "done")
    stream_start_count = sum(1 for m in gw_messages if m.get("type") == "stream_start")
    stream_end_count = sum(1 for m in gw_messages if m.get("type") == "stream_end")
    print(f"  Gateway received: {stream_start_count} stream_start, {stream_end_count} stream_end, {done_count} done")
    print(f"  Subagent results: {received_results}/{expected_results}")
    print(f"  Total time: {time.time() - T0:.1f}s")

    # Cleanup
    drv.close()
    gw.close()
    proc.terminate()
    proc.wait(timeout=5)
    mock.shutdown()
    try:
        os.unlink(SOCKET)
    except FileNotFoundError:
        pass


if __name__ == "__main__":
    main()

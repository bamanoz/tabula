#!/usr/bin/env python3
"""
Tabula Subagent (Anthropic) — autonomous LLM agent in its own session.

Connects to kernel, joins its own session, executes tasks via LLM.
Sends results to parent session. Stays alive for follow-up messages.
Exits after idle timeout.
"""

import argparse
import json
import os
import select
import socket
import sys
import time
import urllib.request
import urllib.error

BASE_URL = os.environ.get("ANTHROPIC_BASE_URL", "https://api.anthropic.com")
API_URL = f"{BASE_URL}/v1/messages"
API_KEY = os.environ.get("ANTHROPIC_API_KEY", "")
SOCKET_PATH = os.environ.get("TABULA_SOCKET", "/tmp/tabula.sock")
DEFAULT_IDLE_TIMEOUT = 120  # seconds


def log(msg: str):
    sys.stderr.write(f"[subagent] {msg}\n")
    sys.stderr.flush()


class KernelConnection:
    def __init__(self, path: str):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.connect(path)
        self.buf = b""

    def send(self, msg: dict):
        data = json.dumps(msg, ensure_ascii=False) + "\n"
        self.sock.sendall(data.encode())

    def recv(self, timeout: float | None = None) -> dict | None:
        """Receive a message. Returns None on disconnect, raises TimeoutError on timeout."""
        while True:
            nl = self.buf.find(b"\n")
            if nl >= 0:
                line = self.buf[:nl]
                self.buf = self.buf[nl + 1:]
                if line.strip():
                    return json.loads(line)
                continue

            if timeout is not None:
                ready, _, _ = select.select([self.sock], [], [], timeout)
                if not ready:
                    raise TimeoutError()

            chunk = self.sock.recv(65536)
            if not chunk:
                return None
            self.buf += chunk

    def close(self):
        self.sock.close()


def kernel_to_anthropic_tools(kernel_tools: list[dict]) -> list[dict]:
    result = []
    for tool in kernel_tools:
        result.append({
            "name": tool["name"],
            "description": tool["description"],
            "input_schema": {
                "type": "object",
                "properties": {
                    k: {"type": v["type"], "description": v["description"]}
                    for k, v in tool.get("params", {}).items()
                },
                "required": tool.get("required", []),
            },
        })
    return result


def call_api(model: str, system_prompt: str, tools: list[dict], messages: list[dict]) -> dict:
    body = json.dumps({
        "model": model,
        "max_tokens": 4096,
        "system": system_prompt,
        "messages": messages,
        "tools": tools if tools else None,
    }).encode()

    req = urllib.request.Request(
        API_URL,
        data=body,
        headers={
            "Content-Type": "application/json",
            "x-api-key": API_KEY,
            "anthropic-version": "2023-06-01",
        },
    )

    resp = urllib.request.urlopen(req)
    return json.loads(resp.read())


def extract_text(content: list[dict]) -> str:
    return "\n".join(b["text"] for b in content if b.get("type") == "text")


def extract_tool_uses(content: list[dict]) -> list[dict]:
    return [b for b in content if b.get("type") == "tool_use"]


def process_turn(conn, model, system_prompt, tools, messages, parent_session, agent_id):
    """Run LLM turns until a text response (no tool calls). Returns the result text."""
    max_turns = 20

    for _ in range(max_turns):
        try:
            response = call_api(model, system_prompt, tools, messages)
        except Exception as e:
            log(f"API error: {e}")
            return f"[subagent error: {e}]"

        content = response.get("content", [])
        messages.append({"role": "assistant", "content": content})

        tool_uses = extract_tool_uses(content)

        if not tool_uses:
            return extract_text(content)

        # Execute tools via kernel
        tool_results = []
        for tool in tool_uses:
            conn.send({
                "type": "tool_use",
                "id": tool["id"],
                "name": tool["name"],
                "input": tool["input"],
            })
            result_msg = conn.recv()
            if result_msg is None:
                return "[kernel disconnected]"
            tool_results.append({
                "type": "tool_result",
                "tool_use_id": tool["id"],
                "content": result_msg.get("output", ""),
            })

        messages.append({"role": "user", "content": tool_results})

    return "[subagent: max turns reached]"


def main():
    parser = argparse.ArgumentParser(description="Tabula subagent (Anthropic)")
    parser.add_argument("--id", required=True, help="Correlation ID")
    parser.add_argument("--parent-session", required=True, help="Session to send results to")
    parser.add_argument("--task", required=True, help="Initial task")
    parser.add_argument("--model", default=os.environ.get("ANTHROPIC_MODEL", "claude-sonnet-4-6"))
    parser.add_argument("--timeout", type=int, default=DEFAULT_IDLE_TIMEOUT,
                        help=f"Idle timeout in seconds (default: {DEFAULT_IDLE_TIMEOUT})")
    args = parser.parse_args()

    if not API_KEY:
        log("ERROR: ANTHROPIC_API_KEY not set")
        sys.exit(1)

    session_name = f"subagent-{args.id}"

    # Connect to kernel
    conn = KernelConnection(SOCKET_PATH)
    conn.send({
        "type": "connect",
        "name": session_name,
        "sends": ["message", "tool_use", "done"],
        "receives": ["message", "tool_result", "init"],
    })
    conn.recv()  # connected

    conn.send({"type": "join", "session": session_name})
    conn.recv()  # joined

    # Wait for init
    init_msg = conn.recv()
    if init_msg is None or init_msg.get("type") != "init":
        log("did not receive init")
        conn.close()
        sys.exit(1)

    system_prompt = init_msg.get("prompt", "")
    kernel_tools = init_msg.get("tools", [])
    tools = kernel_to_anthropic_tools(kernel_tools)

    log(f"started: id={args.id}, timeout={args.timeout}s")

    # Process initial task
    messages = [{"role": "user", "content": args.task}]
    result = process_turn(conn, args.model, system_prompt, tools, messages, args.parent_session, args.id)

    conn.send({
        "type": "message",
        "session": args.parent_session,
        "id": args.id,
        "text": result,
    })
    log(f"initial task done: {len(result)} bytes")

    # Stay alive — wait for follow-up messages
    while True:
        try:
            msg = conn.recv(timeout=args.timeout)
        except TimeoutError:
            log(f"idle timeout ({args.timeout}s), exiting")
            break

        if msg is None:
            log("kernel disconnected")
            break

        if msg.get("type") != "message":
            continue

        text = msg.get("text", "")
        if not text.strip():
            continue

        log(f"follow-up: {text[:80]}")
        messages.append({"role": "user", "content": text})
        result = process_turn(conn, args.model, system_prompt, tools, messages, args.parent_session, args.id)

        conn.send({
            "type": "message",
            "session": args.parent_session,
            "id": args.id,
            "text": result,
        })
        log(f"follow-up done: {len(result)} bytes")

    conn.close()


if __name__ == "__main__":
    main()

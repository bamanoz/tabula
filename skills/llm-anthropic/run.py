#!/usr/bin/env python3
"""
Tabula LLM Driver — Anthropic adapter.

Connects to kernel via Unix socket (TABULA_SOCKET env var).
Translates between Anthropic streaming API and kernel protocol.
"""

import json
import os
import signal
import socket
import sys
import urllib.request
import urllib.error

BASE_URL = os.environ.get("ANTHROPIC_BASE_URL", "https://api.anthropic.com")
API_URL = f"{BASE_URL}/v1/messages"
API_KEY = os.environ.get("ANTHROPIC_API_KEY", "")
MODEL = os.environ.get("ANTHROPIC_MODEL", "claude-sonnet-4-6")
SOCKET_PATH = os.environ.get("TABULA_SOCKET", "/tmp/tabula.sock")


def log(msg: str):
    sys.stderr.write(f"[driver] {msg}\n")
    sys.stderr.flush()


class KernelConnection:
    """JSON lines protocol over Unix socket."""

    def __init__(self, path: str):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.connect(path)
        self.buf = b""

    def send(self, msg: dict):
        data = json.dumps(msg, ensure_ascii=False) + "\n"
        self.sock.sendall(data.encode())

    def recv(self) -> dict | None:
        while True:
            nl = self.buf.find(b"\n")
            if nl >= 0:
                line = self.buf[:nl]
                self.buf = self.buf[nl + 1:]
                if line.strip():
                    return json.loads(line)
                continue

            chunk = self.sock.recv(65536)
            if not chunk:
                return None
            self.buf += chunk

    def close(self):
        self.sock.close()


def kernel_to_anthropic_tools(kernel_tools: list[dict]) -> list[dict]:
    """Convert kernel-neutral tool definitions to Anthropic API format."""
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


class Driver:
    def __init__(self):
        self.conn = KernelConnection(SOCKET_PATH)
        self.system_prompt = ""
        self.tools = []
        self.messages: list[dict] = []
        self.aborted = False

    def connect(self):
        self.conn.send({
            "type": "connect",
            "name": "anthropic",
            "sends": ["stream_start", "stream_delta", "stream_end", "tool_use", "done"],
            "receives": ["message", "tool_result", "init"],
        })
        resp = self.conn.recv()
        log(f"connected: {resp}")

        self.conn.send({"type": "join", "session": "main"})
        resp = self.conn.recv()
        log(f"joined: {resp}")

    def call_api_streaming(self):
        """Call Claude streaming API. Streams text deltas to kernel,
        accumulates tool_use blocks. Returns assistant content blocks."""

        body = json.dumps({
            "model": MODEL,
            "max_tokens": 4096,
            "system": self.system_prompt,
            "messages": self.messages,
            "tools": self.tools if self.tools else None,
            "stream": True,
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

        content_blocks = []  # for history
        tool_uses = []  # tool_use blocks to send to kernel
        current_text = ""
        current_tool = None
        stream_started = False

        try:
            for raw_line in resp:
                if self.aborted:
                    break

                line = raw_line.decode().strip()
                if not line.startswith("data: "):
                    continue

                data = json.loads(line[6:])
                event_type = data.get("type")

                if event_type == "content_block_start":
                    block = data["content_block"]
                    if block["type"] == "text":
                        current_text = ""
                    elif block["type"] == "tool_use":
                        current_tool = {
                            "id": block["id"],
                            "name": block["name"],
                            "input_json": "",
                        }

                elif event_type == "content_block_delta":
                    delta = data["delta"]
                    if delta["type"] == "text_delta":
                        text = delta["text"]
                        current_text += text
                        if not stream_started:
                            self.conn.send({"type": "stream_start"})
                            stream_started = True
                        self.conn.send({"type": "stream_delta", "text": text})
                    elif delta["type"] == "input_json_delta" and current_tool:
                        current_tool["input_json"] += delta["partial_json"]

                elif event_type == "content_block_stop":
                    if current_tool:
                        try:
                            input_data = json.loads(current_tool["input_json"]) if current_tool["input_json"] else {}
                        except json.JSONDecodeError:
                            input_data = {}
                        tool_block = {
                            "type": "tool_use",
                            "id": current_tool["id"],
                            "name": current_tool["name"],
                            "input": input_data,
                        }
                        content_blocks.append(tool_block)
                        tool_uses.append(tool_block)
                        current_tool = None
                    elif current_text:
                        content_blocks.append({"type": "text", "text": current_text})
                        current_text = ""

                elif event_type == "message_delta":
                    pass  # stop_reason handled implicitly

        finally:
            resp.close()

        if stream_started:
            self.conn.send({"type": "stream_end"})

        # Send tool_use messages to kernel
        for tool in tool_uses:
            self.conn.send({
                "type": "tool_use",
                "id": tool["id"],
                "name": tool["name"],
                "input": tool["input"],
            })

        # Only send done if no tool calls — otherwise the turn continues
        if not tool_uses:
            self.conn.send({"type": "done"})

        return content_blocks, tool_uses

    def process_turn(self):
        """Run one LLM turn: call API, stream response, handle tool loop."""
        self.aborted = False

        try:
            content_blocks, tool_uses = self.call_api_streaming()
        except Exception as e:
            log(f"API error: {e}")
            self.conn.send({"type": "stream_start"})
            self.conn.send({"type": "stream_delta", "text": f"[API Error: {e}]"})
            self.conn.send({"type": "stream_end"})
            self.conn.send({"type": "done"})
            return

        # Add assistant message to history
        self.messages.append({"role": "assistant", "content": content_blocks})

        # If there were tool uses, wait for tool_results
        # (kernel will send them, and our main loop dispatches handle_tool_result)

    def handle_init(self, msg: dict):
        self.system_prompt = msg.get("prompt", "")
        kernel_tools = msg.get("tools", [])
        self.tools = kernel_to_anthropic_tools(kernel_tools)
        log(f"init: prompt={len(self.system_prompt)} chars, tools={len(self.tools)}")

    def handle_message(self, msg: dict):
        text = msg.get("text", "")
        log(f"message: {text[:200]}")
        self.messages.append({"role": "user", "content": text})
        self.process_turn()

    def handle_tool_result(self, msg: dict):
        tool_id = msg.get("id", "")
        output = msg.get("output", "")
        log(f"tool_result: id={tool_id}")

        self.messages.append({
            "role": "user",
            "content": [{
                "type": "tool_result",
                "tool_use_id": tool_id,
                "content": output,
            }],
        })
        self.process_turn()

    def run(self):
        while True:
            msg = self.conn.recv()
            if msg is None:
                log("connection closed")
                break

            msg_type = msg.get("type")
            if msg_type == "init":
                self.handle_init(msg)
            elif msg_type == "message":
                self.handle_message(msg)
            elif msg_type == "tool_result":
                self.handle_tool_result(msg)
            else:
                log(f"ignoring: {msg_type}")


def main():
    if not API_KEY:
        log("ERROR: ANTHROPIC_API_KEY not set")
        sys.exit(1)

    driver = Driver()

    def handle_sigint(sig, frame):
        log("SIGINT — aborting stream")
        driver.aborted = True

    signal.signal(signal.SIGINT, handle_sigint)

    log(f"connecting to {SOCKET_PATH}")
    driver.connect()
    driver.run()


if __name__ == "__main__":
    main()

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
import sys
import time
import urllib.request
import urllib.error

import websocket as ws_client

BASE_URL = os.environ.get("ANTHROPIC_BASE_URL", "https://api.anthropic.com")
API_URL = f"{BASE_URL}/v1/messages"
API_KEY = os.environ.get("ANTHROPIC_API_KEY", "")
TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
DEFAULT_IDLE_TIMEOUT = 0  # 0 = oneshot (exit after task), >0 = wait for follow-ups


VERBOSE = os.environ.get("TABULA_VERBOSE", "") == "1"
LOG_FILE = os.path.join(os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula")), "subagent.log")

def log(msg: str):
    if VERBOSE:
        sys.stderr.write(f"[subagent] {msg}\n")
        sys.stderr.flush()
        try:
            with open(LOG_FILE, "a") as f:
                f.write(f"[{time.time():.1f}] {msg}\n")
        except Exception:
            pass


class KernelConnection:
    """WebSocket connection to kernel."""

    def __init__(self, url: str):
        self.ws = ws_client.create_connection(url)

    def send(self, msg: dict):
        self.ws.send(json.dumps(msg, ensure_ascii=False))

    def recv(self, timeout: float | None = None) -> dict | None:
        """Receive one message. Returns None on disconnect, raises TimeoutError on timeout."""
        if timeout is not None:
            self.ws.settimeout(timeout)
        else:
            self.ws.settimeout(None)
        try:
            data = self.ws.recv()
            if not data:
                return None
            return json.loads(data)
        except ws_client.WebSocketTimeoutException:
            raise TimeoutError()
        except (ws_client.WebSocketConnectionClosedException, ConnectionError):
            return None

    def close(self):
        try:
            self.ws.close()
        except Exception:
            pass


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
    payload = {
        "model": model,
        "max_tokens": 4096,
        "system": system_prompt,
        "messages": messages,
    }
    if tools:
        payload["tools"] = tools
    body = json.dumps(payload).encode()

    req = urllib.request.Request(
        API_URL,
        data=body,
        headers={
            "Content-Type": "application/json",
            "x-api-key": API_KEY,
            "anthropic-version": "2023-06-01",
        },
    )

    try:
        resp = urllib.request.urlopen(req)
        return json.loads(resp.read())
    except urllib.error.HTTPError as e:
        error_body = e.read().decode()[:500]
        log(f"API HTTP {e.code}: {error_body}")
        raise


def extract_text(content: list[dict]) -> str:
    return "\n".join(b["text"] for b in content if b.get("type") == "text")


def extract_tool_uses(content: list[dict]) -> list[dict]:
    return [b for b in content if b.get("type") == "tool_use"]


def process_turn(conn, model, system_prompt, tools, messages, parent_session, agent_id):
    """Run LLM turns until a text response (no tool calls). Returns the result text."""
    max_turns = 20

    for _ in range(max_turns):
        log(f"calling API: model={model}, messages={len(messages)}, tools={len(tools)}")
        try:
            response = call_api(model, system_prompt, tools, messages)
        except Exception as e:
            log(f"API error: {e}")
            return f"[subagent error: {e}]"

        content = response.get("content", [])
        log(f"API response: {len(content)} blocks, stop={response.get('stop_reason')}")
        messages.append({"role": "assistant", "content": content})

        tool_uses = extract_tool_uses(content)

        if not tool_uses:
            return extract_text(content)

        # Execute tools via kernel
        tool_results = []
        for i, tool in enumerate(tool_uses):
            log(f"sending tool_use {i+1}/{len(tool_uses)}: {tool['name']} id={tool['id']}")
            conn.send({
                "type": "tool_use",
                "id": tool["id"],
                "name": tool["name"],
                "input": tool["input"],
            })
            log(f"waiting for tool_result {i+1}/{len(tool_uses)}...")
            result_msg = conn.recv()
            if result_msg is None:
                log(f"kernel disconnected while waiting for tool_result {i+1}")
                return "[kernel disconnected]"
            log(f"got response type={result_msg.get('type')} id={result_msg.get('id', 'none')} output_len={len(result_msg.get('output', ''))}")
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

    log(f"main() starting: id={args.id}, API_KEY={'set' if API_KEY else 'EMPTY'}, URL={TABULA_URL}")

    if not API_KEY:
        log("ERROR: ANTHROPIC_API_KEY not set")
        sys.exit(1)

    session_name = f"subagent-{args.id}"

    # Connect to kernel
    conn = KernelConnection(TABULA_URL)
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
    # Subagents only get EXEC — no SPAWN/KILL/LIST to prevent recursive spawning
    kernel_tools = [t for t in kernel_tools if t.get("name") == "EXEC"]
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

    # Oneshot mode: exit immediately unless timeout > 0
    if args.timeout <= 0:
        log("oneshot mode, exiting")
        conn.close()
        sys.exit(0)

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

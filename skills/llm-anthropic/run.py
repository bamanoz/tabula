#!/usr/bin/env python3
"""
LLM skill for Tabula — Anthropic provider with tool use.

Boot protocol:
1. First line from stdin = JSON with kernel tools definition
2. Lines until empty line = system prompt (genesis)
3. Then: tool results and user messages

Stdout: kernel commands (SPAWN, KILL, SEND), one per line, empty line = end of turn
"""

import sys
import os
import json
import urllib.request
import urllib.error

BASE_URL = os.environ.get("ANTHROPIC_BASE_URL", "https://api.anthropic.com")
API_URL = f"{BASE_URL}/v1/messages"
API_KEY = os.environ.get("ANTHROPIC_API_KEY", "")
MODEL = os.environ.get("ANTHROPIC_MODEL", "claude-sonnet-4-6")

# Commands that don't produce kernel responses (fire-and-forget)
NO_RESPONSE_COMMANDS = {"SEND"}


def kernel_to_anthropic_tools(kernel_tools: list[dict]) -> list[dict]:
    """Convert kernel tool definitions to Anthropic API format."""
    anthropic_tools = []
    for tool in kernel_tools:
        anthropic_tool = {
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
        }
        anthropic_tools.append(anthropic_tool)
    return anthropic_tools


def tool_call_to_command(name: str, input_data: dict) -> str:
    """Convert a tool call to a kernel command string."""
    if name == "SPAWN":
        cmd = input_data['command']
        if '\n' in cmd:
            # Multiline: wrap in sh -c with base64 encoding
            import base64
            encoded = base64.b64encode(cmd.encode()).decode()
            return f"SPAWN sh -c 'echo {encoded} | base64 -d | sh'"
        return f"SPAWN {cmd}"
    elif name == "EXEC":
        cmd = input_data['command']
        if '\n' in cmd:
            import base64
            encoded = base64.b64encode(cmd.encode()).decode()
            return f"EXEC sh -c 'echo {encoded} | base64 -d | sh'"
        return f"EXEC {cmd}"
    elif name == "KILL":
        return f"KILL {input_data['pid']}"
    elif name == "SEND":
        text = input_data['text'].replace('\n', '\\n')
        return f"SEND {input_data['pid']} {text}"
    return f"UNKNOWN {name}"


def call_llm(system: str, messages: list[dict], tools: list[dict]) -> dict:
    """Call Claude API with tools. Returns full response."""
    body = json.dumps({
        "model": MODEL,
        "max_tokens": 4096,
        "system": system,
        "messages": messages,
        "tools": tools,
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

    try:
        with urllib.request.urlopen(req) as resp:
            return json.loads(resp.read())
    except urllib.error.HTTPError as e:
        error_body = e.read().decode()
        return {"error": f"{e.code} {error_body}"}
    except Exception as e:
        return {"error": str(e)}


def read_message() -> str | None:
    """Read lines until empty line or EOF."""
    lines = []
    for line in sys.stdin:
        stripped = line.rstrip("\n")
        if stripped == "":
            if lines:
                return "\n".join(lines)
            continue
        lines.append(stripped)
    if lines:
        return "\n".join(lines)
    return None


def send_command(cmd: str):
    print(cmd, flush=True)


def end_turn():
    print("", flush=True)


def execute_tool(name: str, input_data: dict) -> str:
    """Send tool call as kernel command and read result."""
    cmd = tool_call_to_command(name, input_data)
    send_command(cmd)
    end_turn()

    if name in NO_RESPONSE_COMMANDS:
        return "OK"

    result = read_message()
    return result or "ERROR: no response"


def log(msg: str):
    sys.stderr.write(f"[llm] {msg}\n")
    sys.stderr.flush()


def main():
    if not API_KEY:
        print("LLM_ERROR: ANTHROPIC_API_KEY not set", flush=True)
        return

    # 1. Read kernel tools definition (first line)
    tools_line = sys.stdin.readline().strip()
    if not tools_line:
        log("ERROR: no tools definition received")
        return

    try:
        kernel_def = json.loads(tools_line)
        kernel_tools = kernel_def.get("tools", [])
    except json.JSONDecodeError as e:
        log(f"ERROR: invalid tools JSON: {e}")
        return

    tools = kernel_to_anthropic_tools(kernel_tools)
    log(f"loaded {len(tools)} kernel tools: {[t['name'] for t in tools]}")

    # 2. Read system prompt (genesis)
    system = read_message()
    if not system:
        log("ERROR: no system prompt received")
        return

    log(f"system prompt loaded ({len(system)} chars)")

    # 3. Main loop
    messages: list[dict] = [{"role": "user", "content": "BEGIN"}]

    while True:
        response = call_llm(system, messages, tools)

        if "error" in response:
            log(f"API error: {response['error']}")
            break

        content = response.get("content", [])
        messages.append({"role": "assistant", "content": content})

        tool_results = []
        has_tool_use = False

        for block in content:
            if block["type"] == "text" and block.get("text", "").strip():
                log(f"text: {block['text'][:200]}")
            elif block["type"] == "tool_use":
                has_tool_use = True
                tool_name = block["name"]
                tool_input = block["input"]
                tool_id = block["id"]

                log(f"tool: {tool_name}({json.dumps(tool_input, ensure_ascii=False)})")
                result = execute_tool(tool_name, tool_input)
                log(f"result: {result}")

                tool_results.append({
                    "type": "tool_result",
                    "tool_use_id": tool_id,
                    "content": result,
                })

        if has_tool_use:
            messages.append({"role": "user", "content": tool_results})
            continue

        # No tool use — wait for external input
        log("waiting for input...")
        msg = read_message()
        if msg is None:
            break
        log(f"received: {msg[:200]}")
        messages.append({"role": "user", "content": msg})


if __name__ == "__main__":
    main()

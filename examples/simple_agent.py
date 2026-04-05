#!/usr/bin/env python3
"""Minimal agent with streaming + tool calls using Anthropic API."""

import json
import os
import subprocess
from urllib.request import Request, urlopen

API_KEY = os.environ["ANTHROPIC_API_KEY"]
API_URL = "https://api.anthropic.com/v1/messages"

TOOLS = [
    {
        "name": "read_file",
        "description": "Read a file and return its contents",
        "input_schema": {
            "type": "object",
            "properties": {"path": {"type": "string"}},
            "required": ["path"],
        },
    },
    {
        "name": "run_command",
        "description": "Run a shell command and return stdout",
        "input_schema": {
            "type": "object",
            "properties": {"command": {"type": "string"}},
            "required": ["command"],
        },
    },
]


def execute_tool(name, input):
    if name == "read_file":
        with open(input["path"]) as f:
            return f.read()
    elif name == "run_command":
        result = subprocess.run(input["command"], shell=True, capture_output=True, text=True)
        return result.stdout or result.stderr


def stream_response(messages):
    """Call Claude API with streaming. Yields events."""
    body = json.dumps({
        "model": "claude-sonnet-4-20250514",
        "max_tokens": 4096,
        "stream": True,
        "tools": TOOLS,
        "messages": messages,
    }).encode()

    req = Request(API_URL, data=body, headers={
        "x-api-key": API_KEY,
        "anthropic-version": "2023-06-01",
        "content-type": "application/json",
    })

    with urlopen(req) as resp:
        buf = b""
        for chunk in resp:
            buf += chunk
            while b"\n\n" in buf:
                frame, buf = buf.split(b"\n\n", 1)
                for line in frame.split(b"\n"):
                    if line.startswith(b"data: "):
                        data = json.loads(line[6:])
                        yield data


def agent_loop():
    messages = []
    print("Agent ready. Type a message (Ctrl+C to quit).\n")

    while True:
        try:
            user_input = input("You: ")
        except (KeyboardInterrupt, EOFError):
            print("\nBye!")
            break

        if not user_input.strip():
            continue

        messages.append({"role": "user", "content": user_input})

        # Conversation loop (may need multiple API calls for tool use)
        while True:
            content_blocks = []
            current_tool = None
            current_json = ""

            print("Assistant: ", end="", flush=True)

            for event in stream_response(messages):
                t = event.get("type")

                if t == "content_block_start":
                    block = event["content_block"]
                    if block["type"] == "text":
                        pass  # text streaming handled in delta
                    elif block["type"] == "tool_use":
                        current_tool = {"id": block["id"], "name": block["name"]}
                        current_json = ""

                elif t == "content_block_delta":
                    delta = event["delta"]
                    if delta["type"] == "text_delta":
                        # Stream text to terminal
                        print(delta["text"], end="", flush=True)
                    elif delta["type"] == "input_json_delta":
                        current_json += delta["partial_json"]

                elif t == "content_block_stop":
                    if current_tool:
                        input_data = json.loads(current_json) if current_json else {}
                        content_blocks.append({
                            "type": "tool_use",
                            "id": current_tool["id"],
                            "name": current_tool["name"],
                            "input": input_data,
                        })
                        print(f"\n[tool: {current_tool['name']}({input_data})]", flush=True)
                        current_tool = None
                        current_json = ""

                elif t == "message_stop":
                    print()  # newline after response

            # Save assistant message
            # Collect text blocks from what was streamed
            if not content_blocks:
                # Text-only response — reconstruct from what was printed
                # (in a real agent you'd accumulate text in the loop)
                break

            messages.append({"role": "assistant", "content": content_blocks})

            # Execute tools and continue
            tool_results = []
            has_tool_use = False
            for block in content_blocks:
                if block["type"] == "tool_use":
                    has_tool_use = True
                    result = execute_tool(block["name"], block["input"])
                    tool_results.append({
                        "type": "tool_result",
                        "tool_use_id": block["id"],
                        "content": result or "",
                    })

            if not has_tool_use:
                break

            messages.append({"role": "user", "content": tool_results})
            # Loop back to call API again with tool results


if __name__ == "__main__":
    agent_loop()

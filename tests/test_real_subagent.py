#!/usr/bin/env python3
"""
E2E test with real LLM: start kernel + real driver, send a prompt
that triggers subagent spawning, wait for final response.
Prints all protocol messages and kernel log.

Usage: ANTHROPIC_API_KEY=... python3 tests/test_real_subagent.py
"""

import json
import os
import signal
import subprocess
import sys
import threading
import time

import websocket as ws_client

TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:18089/ws")
TABULA_BIN = os.path.expanduser("~/.tabula/bin/tabula")
TABULA_HOME = os.path.expanduser("~/.tabula")
LOG_FILE = os.path.join(TABULA_HOME, "kernel.log")
TIMEOUT = 360  # max seconds to wait

# Colors
C = "\033[36m"
G = "\033[32m"
Y = "\033[33m"
R = "\033[31m"
D = "\033[2m"
B = "\033[1m"
RST = "\033[0m"

T0 = time.time()


def ts():
    return f"{D}{time.time() - T0:6.1f}s{RST}"


def log(src, color, msg):
    print(f"  {ts()} {color}{B}[{src}]{RST} {color}{msg}{RST}")


def main():
    if not os.environ.get("ANTHROPIC_API_KEY"):
        print("ERROR: set ANTHROPIC_API_KEY")
        sys.exit(1)

    # Truncate log file
    open(LOG_FILE, "w").close()

    # Start kernel with verbose mode
    env = os.environ.copy()
    env["TABULA_URL"] = TABULA_URL
    proc = subprocess.Popen(
        [TABULA_BIN, "-v"],
        env=env,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
    )

    # Wait for server to be ready
    time.sleep(3)
    log("test", C, "kernel ready")

    # Connect as test gateway via WebSocket
    try:
        gw = ws_client.create_connection(TABULA_URL, timeout=30)
    except Exception as e:
        log("test", R, f"cannot connect: {e}")
        proc.terminate()
        return

    gw.send(json.dumps({
        "type": "connect",
        "name": "test-gateway",
        "sends": ["message"],
        "receives": ["stream_start", "stream_delta", "stream_end", "done", "error",
                     "tool_use", "tool_result", "message"],
    }))
    json.loads(gw.recv())  # connected

    gw.send(json.dumps({"type": "join", "session": "main"}))
    json.loads(gw.recv())  # joined

    log("test", C, "connected as test-gateway")

    # Send test prompt
    prompt = "проведи исследование, что такое openclaw"
    log("test", C, f"sending: {prompt}")
    gw.send(json.dumps({"type": "message", "text": prompt}))

    # Collect messages
    messages = []
    full_text = ""
    done_count = 0
    last_activity = time.time()
    start = time.time()
    IDLE_EXIT = 30

    gw.settimeout(2)
    while time.time() - start < TIMEOUT:
        if done_count >= 2 and time.time() - last_activity > IDLE_EXIT:
            log("test", C, f"idle for {IDLE_EXIT}s after done #{done_count}, finishing")
            break

        try:
            data = gw.recv()
            if not data:
                log("test", R, "disconnected")
                break
        except ws_client.WebSocketTimeoutException:
            continue
        except Exception:
            continue

        try:
            msg = json.loads(data)
        except json.JSONDecodeError:
            continue

        mt = msg.get("type", "?")
        messages.append(msg)
        last_activity = time.time()

        if mt == "stream_start":
            log("gw", G, "< stream_start")
        elif mt == "stream_delta":
            text = msg.get("text", "")
            full_text += text
            preview = text[:80].replace("\n", "\\n")
            log("gw", D, f"< delta: {preview}")
        elif mt == "stream_end":
            log("gw", G, "< stream_end")
        elif mt == "done":
            done_count += 1
            log("gw", G, f"< done (#{done_count})")
        elif mt == "tool_use":
            name = msg.get("name", "?")
            inp = json.dumps(msg.get("input", {}))[:80]
            log("gw", Y, f"< tool_use: {name}({inp})")
        elif mt == "tool_result":
            out = msg.get("output", "")[:80]
            log("gw", Y, f"< tool_result: {out}")
        elif mt == "error":
            log("gw", R, f"< error: {msg.get('text', '')}")
        elif mt == "message":
            mid = msg.get("id", "")
            text = msg.get("text", "")[:80]
            log("gw", G, f"< message: id={mid} {text}")
        else:
            log("gw", D, f"< {mt}")

    # Summary
    elapsed = time.time() - T0
    print(f"\n{B}{'='*60}{RST}")
    print(f"{B}Summary{RST} ({elapsed:.1f}s)")
    print(f"{'='*60}")
    print(f"  Messages received: {len(messages)}")
    print(f"  stream_start: {sum(1 for m in messages if m.get('type')=='stream_start')}")
    print(f"  stream_end:   {sum(1 for m in messages if m.get('type')=='stream_end')}")
    print(f"  done:         {sum(1 for m in messages if m.get('type')=='done')}")
    print(f"  tool_use:     {sum(1 for m in messages if m.get('type')=='tool_use')}")
    print(f"  tool_result:  {sum(1 for m in messages if m.get('type')=='tool_result')}")
    print(f"  error:        {sum(1 for m in messages if m.get('type')=='error')}")
    print(f"  message:      {sum(1 for m in messages if m.get('type')=='message')}")

    print(f"\n{B}Full response text:{RST}")
    print(f"  {full_text[:500]}")

    # Show kernel log
    print(f"\n{B}Kernel log:{RST}")
    try:
        with open(LOG_FILE) as f:
            for line in f:
                print(f"  {D}{line.rstrip()}{RST}")
    except FileNotFoundError:
        print("  (no log file)")

    # Cleanup
    gw.close()
    proc.terminate()
    proc.wait(timeout=10)


if __name__ == "__main__":
    main()

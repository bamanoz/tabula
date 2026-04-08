#!/usr/bin/env python3
"""
Tabula Timer Skill — send a message to the session after a delay.
Lightweight alternative to cron for short timers (seconds).
Connects directly to the kernel via WebSocket — no LLM/subagent needed.
"""

import argparse
import os
import sys
import time

# Resolve skills/ directory relative to this script's location
SKILL_DIR = os.path.dirname(os.path.abspath(__file__))
SKILLS_ROOT = os.path.dirname(SKILL_DIR)
if SKILLS_ROOT not in sys.path:
    sys.path.insert(0, SKILLS_ROOT)

from lib.kernel_client import KernelConnection


def main():
    parser = argparse.ArgumentParser(prog="timer", description="Send a message after a delay")
    parser.add_argument("--seconds", "-s", type=int, required=True, help="Delay in seconds")
    parser.add_argument("--message", "-m", type=str, required=True, help="Message to send")
    parser.add_argument("--session", type=str, default="main", help="Target session (default: main)")
    args = parser.parse_args()

    # Sleep first
    time.sleep(args.seconds)

    # Then connect to kernel and send the message
    url = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
    conn = KernelConnection(url)
    conn.send({
        "type": "connect",
        "name": "timer",
        "sends": ["message"],
        "receives": [],
    })
    conn.recv(timeout=5)
    conn.send({"type": "join", "session": args.session})
    conn.recv(timeout=5)
    conn.send({
        "type": "message",
        "session": args.session,
        "id": f"timer-{int(time.time())}",
        "text": args.message,
    })
    conn.close()
    print(f"Timer fired: {args.message}")


if __name__ == "__main__":
    main()

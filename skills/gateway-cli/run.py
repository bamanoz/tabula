#!/usr/bin/env python3
"""
CLI Gateway skill for Tabula.

stdin  ← receives responses from kernel (via SEND)
stdout → sends user input to kernel (auto-piped to LLM)
/dev/tty → direct terminal access for user I/O
"""

import sys
import os
import threading


def read_responses():
    """Read responses from stdin (← kernel SEND) and display on terminal."""
    for line in sys.stdin:
        text = line.rstrip("\n")
        if text:
            tty.write(f"\ntabula: {text}\n> ")
            tty.flush()


def read_user_input():
    """Read user input from terminal and send to stdout (→ kernel → LLM)."""
    tty.write("> ")
    tty.flush()
    try:
        while True:
            line = tty_in.readline()
            if not line:
                break
            text = line.strip()
            if text:
                # Send to stdout → kernel → LLM
                print(text, flush=True)
                # Empty line as delimiter for LLM skill
                print("", flush=True)
    except (EOFError, KeyboardInterrupt):
        pass


# Open terminal directly (bypasses pipe redirection)
tty = open("/dev/tty", "w")
tty_in = open("/dev/tty", "r")

reader = threading.Thread(target=read_responses, daemon=True)
reader.start()

read_user_input()

tty.close()
tty_in.close()

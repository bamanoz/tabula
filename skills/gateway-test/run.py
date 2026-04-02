#!/usr/bin/env python3
"""Test gateway: sends a message, prints responses, then waits."""

import sys
import threading
import time


def read_responses():
    for line in sys.stdin:
        text = line.rstrip("\n")
        if text:
            sys.stderr.write(f"[gateway] <<< {text}\n")
            sys.stderr.flush()


reader = threading.Thread(target=read_responses, daemon=True)
reader.start()

time.sleep(2)
sys.stderr.write("[gateway] >>> Прочитай файл tabula.yaml\n")
sys.stderr.flush()
print("Прочитай файл tabula.yaml", flush=True)
print("", flush=True)

time.sleep(60)

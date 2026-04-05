#!/usr/bin/env python3
"""
Tabula CLI Gateway.

Connects to kernel via Unix socket. Simple terminal I/O —
reads from /dev/tty, writes streaming responses to stdout.
"""

import json
import os
import signal
import socket
import sys
import threading

SOCKET_PATH = os.environ.get("TABULA_SOCKET", "/tmp/tabula.sock")


def log(msg: str):
    sys.stderr.write(f"[gateway] {msg}\n")
    sys.stderr.flush()


class KernelConnection:
    """JSON lines protocol over Unix socket."""

    def __init__(self, path: str):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.connect(path)
        self.buf = b""
        self._lock = threading.Lock()

    def send(self, msg: dict):
        data = json.dumps(msg, ensure_ascii=False) + "\n"
        with self._lock:
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

            try:
                chunk = self.sock.recv(65536)
            except OSError:
                return None
            if not chunk:
                return None
            self.buf += chunk

    def close(self):
        try:
            self.sock.shutdown(socket.SHUT_RDWR)
        except OSError:
            pass
        self.sock.close()


class Gateway:
    def __init__(self):
        self.conn = KernelConnection(SOCKET_PATH)
        self.streaming = False
        self.alive = True
        self.tty = None

    def connect(self):
        self.conn.send({
            "type": "connect",
            "name": "cli",
            "sends": ["message", "cancel"],
            "receives": ["stream_start", "stream_delta", "stream_end", "done", "error"],
        })
        resp = self.conn.recv()
        log(f"connected: {resp}")

        self.conn.send({"type": "join", "session": "main"})
        resp = self.conn.recv()
        log(f"joined: {resp}")

    def run(self):
        # Open /dev/tty for input — works regardless of how stdin was set up
        try:
            self.tty = open("/dev/tty", "r")
        except OSError:
            log("ERROR: cannot open /dev/tty — no terminal available")
            sys.exit(1)

        done_event = threading.Event()
        response_text = []

        def receiver():
            while self.alive:
                msg = self.conn.recv()
                if msg is None:
                    log("connection closed")
                    self.alive = False
                    done_event.set()
                    break

                msg_type = msg.get("type")

                if msg_type == "stream_start":
                    self.streaming = True
                    response_text.clear()

                elif msg_type == "stream_delta":
                    text = msg.get("text", "")
                    response_text.append(text)
                    sys.stdout.write(text)
                    sys.stdout.flush()

                elif msg_type == "stream_end":
                    self.streaming = False
                    full_text = "".join(response_text)
                    if full_text and not full_text.endswith("\n"):
                        sys.stdout.write("\n")
                        sys.stdout.flush()
                    response_text.clear()

                elif msg_type == "done":
                    done_event.set()

                elif msg_type == "error":
                    text = msg.get("text", "unknown error")
                    sys.stdout.write(f"\033[31mError: {text}\033[0m\n")
                    sys.stdout.flush()

        recv_thread = threading.Thread(target=receiver, daemon=True)
        recv_thread.start()

        # Ctrl+C sends cancel during streaming, exits at prompt
        def handle_sigint(sig, frame):
            if self.streaming:
                self.conn.send({"type": "cancel"})
                log("cancel sent")
            else:
                self.alive = False
                done_event.set()
                sys.stdout.write("\n")
                sys.stdout.flush()

        signal.signal(signal.SIGINT, handle_sigint)

        sys.stdout.write("\033[1mTabula\033[0m ready.\n\n")
        sys.stdout.flush()

        while self.alive:
            try:
                sys.stdout.write("› ")
                sys.stdout.flush()

                line = self.tty.readline()
                if not line:
                    # EOF (Ctrl+D)
                    break

                user_input = line.rstrip("\n")
                if not user_input.strip():
                    continue

                done_event.clear()
                self.conn.send({"type": "message", "text": user_input})

                # Wait for done — short timeout loop so signals work
                while not done_event.is_set() and self.alive:
                    done_event.wait(timeout=0.3)

            except EOFError:
                break

        self.alive = False
        self.conn.close()
        if self.tty:
            self.tty.close()


def main():
    log(f"connecting to {SOCKET_PATH}")
    gw = Gateway()
    gw.connect()
    gw.run()


if __name__ == "__main__":
    main()

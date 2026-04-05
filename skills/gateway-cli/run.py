#!/usr/bin/env python3
"""
Tabula CLI Gateway.

Connects to kernel via Unix socket. Rich terminal UI inspired by openclaw:
- Streaming markdown rendering via rich.Live
- Shimmer spinner with fun phrases
- Ctrl+C to exit
"""

import json
import os
import random
import socket
import sys
import threading
import time

SOCKET_PATH = os.environ.get("TABULA_SOCKET", "/tmp/tabula.sock")

# --- Theme ---

ACCENT = "#F6C453"
ACCENT_SOFT = "#F2A65A"
DIM = "#7B7F87"
USER_TEXT = "#F3EEE0"
ERROR_COLOR = "#F97066"
LINK_COLOR = "#7DD3A5"
CODE_COLOR = "#F0C987"

WAITING_PHRASES = [
    "pondering", "conjuring", "noodling", "moseying",
    "kerfuffling", "dillydallying", "bamboozling",
    "hobnobbing", "flibbertigibbeting", "twiddling thumbs",
    "ruminating", "percolating", "cogitating", "gallivanting",
]

GREETING_PHRASES = [
    "ready to mass-produce miracles",
    "all systems nominal, awaiting orders",
    "kernel is humming, skills are loaded",
    "here to automate the boring stuff",
    "standing by for world domination",
    "neurons warmed up, let's go",
    "one socket to rule them all",
    "the microkernel awakens",
    "your personal agent, at your service",
    "skills loaded, imagination required",
    "built different, thinks different",
]

SPINNER_FRAMES = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"

LOGO_LINES = [
    "████████╗ █████╗ ██████╗ ██╗   ██╗██╗      █████╗ ",
    "╚══██╔══╝██╔══██╗██╔══██╗██║   ██║██║     ██╔══██╗",
    "   ██║   ███████║██████╔╝██║   ██║██║     ███████║",
    "   ██║   ██╔══██║██╔══██╗██║   ██║██║     ██╔══██║",
    "   ██║   ██║  ██║██████╔╝╚██████╔╝███████╗██║  ██║",
    "   ╚═╝   ╚═╝  ╚═╝╚═════╝  ╚═════╝ ╚══════╝╚═╝  ╚═╝",
]

MOVE_UP = "\033[A"
CLEAR_LINE = "\033[2K\r"


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
        self.waiting = False
        self.alive = True
        self.turn_start = 0.0
        self.stream_text = ""
        self._waiting_phrase = ""
        self._error_text = ""
        self._tty = None

    def connect(self):
        self.conn.send({
            "type": "connect",
            "name": "cli",
            "sends": ["message"],
            "receives": ["stream_start", "stream_delta", "stream_end", "done", "error"],
        })
        self.conn.recv()
        self.conn.send({"type": "join", "session": "main"})
        self.conn.recv()

    def _pick_phrase(self):
        self._waiting_phrase = random.choice(WAITING_PHRASES)

    def run(self):
        from rich.console import Console
        from rich.markdown import Markdown
        from rich.text import Text
        from rich.live import Live
        from rich.theme import Theme
        from rich.padding import Padding

        custom_theme = Theme({
            "markdown.heading": f"bold {ACCENT}",
            "markdown.link": LINK_COLOR,
            "markdown.link_url": DIM,
            "markdown.code": CODE_COLOR,
            "markdown.item.bullet": ACCENT_SOFT,
        })

        console = Console(theme=custom_theme)

        try:
            self._tty = open("/dev/tty", "r")
        except OSError:
            sys.exit(1)

        done_event = threading.Event()
        stream_lock = threading.Lock()

        def render_stream():
            with stream_lock:
                text = self.stream_text
            if not text:
                return Text("")
            try:
                return Padding(Markdown(text), (0, 0, 0, 4))
            except Exception:
                return Padding(Text(text), (0, 0, 0, 4))

        def render_spinner():
            phrase = self._waiting_phrase
            elapsed = time.time() - self.turn_start
            tick = int(elapsed * 10)
            frame = SPINNER_FRAMES[tick % len(SPINNER_FRAMES)]
            window = 6
            pos = tick % (len(phrase) + window)
            parts = [(f"  {frame} ", f"bold {ACCENT}")]
            for i, ch in enumerate(phrase):
                if pos - window <= i < pos:
                    parts.append((ch, f"bold {ACCENT}"))
                else:
                    parts.append((ch, ACCENT_SOFT))
            parts.append(("…", ACCENT_SOFT))
            parts.append((f"  {elapsed:.0f}s", DIM))
            return Text.assemble(*parts)

        def receiver():
            while self.alive:
                msg = self.conn.recv()
                if msg is None:
                    self.alive = False
                    done_event.set()
                    break

                msg_type = msg.get("type")

                if msg_type == "stream_start":
                    self.streaming = True
                    self.waiting = False
                    with stream_lock:
                        self.stream_text = ""

                elif msg_type == "stream_delta":
                    text = msg.get("text", "")
                    with stream_lock:
                        self.stream_text += text

                elif msg_type == "stream_end":
                    self.streaming = False

                elif msg_type == "done":
                    self.waiting = False
                    done_event.set()

                elif msg_type == "error":
                    self._error_text = msg.get("text", "unknown error")

        recv_thread = threading.Thread(target=receiver, daemon=True)
        recv_thread.start()

        # --- Startup banner ---
        console.print()
        for line in LOGO_LINES:
            console.print(Text(f"  {line}", style=f"bold {ACCENT}"))

        greeting = random.choice(GREETING_PHRASES)
        console.print()
        console.print(Text.assemble(
            ("  ✦ ", f"bold {ACCENT}"),
            (greeting, f"italic {ACCENT_SOFT}"),
        ))
        console.print()

        while self.alive:
            try:
                # Prompt
                console.print(f"[bold {ACCENT}]  ❯[/] ", end="")

                line = self._tty.readline()
                if not line:
                    break

                user_input = line.rstrip("\n")
                if not user_input.strip():
                    continue

                # Erase prompt + typed text, reprint styled
                sys.stdout.write(MOVE_UP + CLEAR_LINE)
                sys.stdout.flush()
                console.print(
                    Text.assemble(
                        ("  ❯ ", DIM),
                        (user_input, USER_TEXT),
                    )
                )
                console.print()

                # Start turn
                done_event.clear()
                self.turn_start = time.time()
                self.waiting = True
                self.stream_text = ""
                self._error_text = ""
                self._pick_phrase()
                self.conn.send({"type": "message", "text": user_input})

                # Live rendering — main thread drives all updates
                with Live(
                    render_spinner(),
                    console=console,
                    refresh_per_second=12,
                    transient=True,
                ) as live:
                    # Spinner while waiting for stream_start
                    while self.waiting and self.alive and not done_event.is_set():
                        live.update(render_spinner())
                        done_event.wait(timeout=0.08)

                    # Streaming — main thread polls and re-renders
                    last_len = 0
                    while not done_event.is_set() and self.alive:
                        with stream_lock:
                            cur_len = len(self.stream_text)
                        if cur_len != last_len:
                            live.update(render_stream())
                            last_len = cur_len
                        if self._error_text:
                            live.update(
                                Text(f"  error: {self._error_text}",
                                     style=f"bold {ERROR_COLOR}")
                            )
                            self._error_text = ""
                        done_event.wait(timeout=0.08)

                # Final markdown
                with stream_lock:
                    final_text = self.stream_text

                if final_text.strip():
                    console.print(Padding(Markdown(final_text), (0, 0, 0, 4)))

                elapsed = time.time() - self.turn_start
                console.print()
                console.print(Text(f"    {elapsed:.1f}s", style=DIM))
                console.print()

            except (EOFError, KeyboardInterrupt):
                break

        self.alive = False
        self.conn.close()
        if self._tty:
            self._tty.close()


def main():
    gw = Gateway()
    gw.connect()
    gw.run()


if __name__ == "__main__":
    main()

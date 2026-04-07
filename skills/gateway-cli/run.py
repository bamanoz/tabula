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
import queue
import random
import sys
import threading
import time

import websocket as ws_client

TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")

# --- Theme ---

ACCENT = "#F6C453"
ACCENT_SOFT = "#F2A65A"
DIM = "#7B7F87"
USER_TEXT = "#F3EEE0"
ERROR_COLOR = "#F97066"
LINK_COLOR = "#7DD3A5"
CODE_COLOR = "#F0C987"
TOOL_COLOR = "#7DD3A5"

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
    """WebSocket connection to kernel."""

    def __init__(self, url: str):
        self.ws = ws_client.create_connection(url)
        self._lock = threading.Lock()

    def send(self, msg: dict):
        data = json.dumps(msg, ensure_ascii=False)
        with self._lock:
            self.ws.send(data)

    def recv(self) -> dict | None:
        try:
            data = self.ws.recv()
            if not data:
                return None
            return json.loads(data)
        except (ws_client.WebSocketConnectionClosedException, ConnectionError):
            return None
        except OSError:
            return None

    def close(self):
        try:
            self.ws.close()
        except Exception:
            pass


class Gateway:
    def __init__(self):
        self.conn = KernelConnection(TABULA_URL)
        self.streaming = False
        self.waiting = False
        self.alive = True
        self.turn_start = 0.0
        self.stream_text = ""
        self._waiting_phrase = ""
        self._error_text = ""
        self._tty = None
        self._input_queue: queue.Queue[str | None] = queue.Queue()
        self._stream_started = threading.Event()
        self._finished_streams: queue.Queue[str] = queue.Queue()

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
                        if self.stream_text.strip():
                            self._finished_streams.put(self.stream_text)
                        self.stream_text = ""
                    self._stream_started.set()

                elif msg_type == "stream_delta":
                    text = msg.get("text", "")
                    with stream_lock:
                        self.stream_text += text

                elif msg_type == "stream_end":
                    self.streaming = False
                    with stream_lock:
                        if self.stream_text.strip():
                            self._finished_streams.put(self.stream_text)
                        self.stream_text = ""

                elif msg_type == "done":
                    self.waiting = False
                    done_event.set()

                elif msg_type == "error":
                    self._error_text = msg.get("text", "unknown error")

        recv_thread = threading.Thread(target=receiver, daemon=True)
        recv_thread.start()

        # Input reader thread — non-blocking readline via queue
        def input_reader():
            try:
                while self.alive:
                    line = self._tty.readline()
                    if not line:
                        self._input_queue.put(None)
                        break
                    self._input_queue.put(line)
            except (OSError, ValueError):
                self._input_queue.put(None)

        input_thread = threading.Thread(target=input_reader, daemon=True)
        input_thread.start()

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
                # === IDLE: show prompt, poll for input or unsolicited stream ===
                console.print(f"[bold {ACCENT}]  ❯[/] ", end="")
                sys.stdout.flush()

                self._stream_started.clear()
                user_input = None

                while self.alive:
                    if self._stream_started.is_set():
                        sys.stdout.write(CLEAR_LINE)
                        sys.stdout.flush()
                        break

                    try:
                        raw = self._input_queue.get(timeout=0.1)
                        if raw is None:
                            self.alive = False
                            break
                        user_input = raw.rstrip("\n")
                        break
                    except queue.Empty:
                        continue

                if not self.alive:
                    break

                # === ACTIVE: handle whichever event arrived ===
                if user_input is not None:
                    if not user_input.strip():
                        continue

                    sys.stdout.write(MOVE_UP + CLEAR_LINE)
                    sys.stdout.flush()
                    console.print(
                        Text.assemble(
                            ("  ❯ ", DIM),
                            (user_input, USER_TEXT),
                        )
                    )
                    console.print()

                    done_event.clear()
                    self._stream_started.clear()
                    self.turn_start = time.time()
                    self.waiting = True
                    self.stream_text = ""
                    self._error_text = ""
                    self._pick_phrase()
                    self.conn.send({"type": "message", "text": user_input})
                else:
                    done_event.clear()
                    self.turn_start = time.time()
                    self._error_text = ""
                    self._pick_phrase()

                # === RENDER: spinner + streaming ===
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

                    # Streaming — may alternate with spinner between streams
                    last_render_text = ""
                    last_render_time = 0.0
                    while not done_event.is_set() and self.alive:
                        # Flush finished streams
                        while not self._finished_streams.empty():
                            try:
                                finished = self._finished_streams.get_nowait()
                                if finished.strip():
                                    live.update(Text(""))
                                    console.print(Padding(Markdown(finished), (0, 0, 0, 4)))
                                    console.print()
                            except queue.Empty:
                                break

                        if not self.streaming and not self.waiting:
                            live.update(render_spinner())
                            done_event.wait(timeout=0.08)
                            continue

                        with stream_lock:
                            cur_text = self.stream_text
                        if cur_text and cur_text != last_render_text:
                            now = time.time()
                            # Throttle markdown rendering: at most every 0.3s for large text
                            if len(cur_text) < 2000 or (now - last_render_time) >= 0.3:
                                try:
                                    live.update(Padding(Markdown(cur_text), (0, 0, 0, 4)))
                                except Exception:
                                    live.update(Padding(Text(cur_text), (0, 0, 0, 4)))
                                last_render_text = cur_text
                                last_render_time = now
                        if self._error_text:
                            live.update(
                                Text(f"  error: {self._error_text}",
                                     style=f"bold {ERROR_COLOR}")
                            )
                            self._error_text = ""
                        done_event.wait(timeout=0.08)

                # Flush remaining finished streams
                while not self._finished_streams.empty():
                    try:
                        finished = self._finished_streams.get_nowait()
                        if finished.strip():
                            console.print(Padding(Markdown(finished), (0, 0, 0, 4)))
                            console.print()
                    except queue.Empty:
                        break

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

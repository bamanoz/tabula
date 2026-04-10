#!/usr/bin/env python3
"""Interactive CLI gateway for Tabula."""

from __future__ import annotations

import argparse
import json
import os
import queue
import random
import re
import signal
import sys
import threading
import time
from dataclasses import dataclass
from uuid import uuid4

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from rich.console import Console, Group
from rich.live import Live
from rich.markdown import Markdown
from rich.padding import Padding
from rich.text import Text
from rich.theme import Theme

from skills.lib.kernel_client import KernelConnection


TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")

ACCENT = "#F6C453"
ACCENT_SOFT = "#F2A65A"
DIM = "#7B7F87"
USER_TEXT = "#F3EEE0"
ERROR_COLOR = "#F97066"
LINK_COLOR = "#7DD3A5"
CODE_COLOR = "#F0C987"

# ANSI 256-color approximation of ACCENT (#F6C453) for raw terminal output
ANSI_ACCENT = "\033[38;2;246;196;83m"
ANSI_RESET = "\033[0m"

WAITING_PHRASES = [
    "pondering",
    "conjuring",
    "noodling",
    "moseying",
    "kerfuffling",
    "dillydallying",
    "ruminating",
    "percolating",
    "cogitating",
]

GREETING_PHRASES = [
    "ready to mass-produce miracles",
    "kernel is humming, skills are loaded",
    "one socket to rule them all",
    "the microkernel awakens",
    "skills loaded, imagination required",
]

ELAPSED_PHRASES = [
    "the runes have spoken",
    "the vision is clear",
    "the stars aligned",
    "the circle is complete",
    "the spell is cast",
    "the sigil burns bright",
    "the ink has dried",
    "the tablet is carved",
]

SPINNER_FRAMES = "🌑🌒🌓🌔🌕🌖🌗🌘"
MOVE_UP = "\033[A"
CLEAR_LINE = "\033[2K\r"


@dataclass
class TurnState:
    waiting: bool = False
    streaming: bool = False
    started_at: float = 0.0
    current_text: str = ""
    rendered_text: str = ""
    waiting_phrase: str = ""
    error_text: str = ""
    response_started: bool = False
    finished_blocks: list[str] | None = None

    def __post_init__(self):
        if self.finished_blocks is None:
            self.finished_blocks = []

    def reset(self):
        self.waiting = False
        self.streaming = False
        self.started_at = 0.0
        self.current_text = ""
        self.rendered_text = ""
        self.waiting_phrase = ""
        self.error_text = ""
        self.response_started = False
        self.finished_blocks = []


class Gateway:
    def __init__(self, driver_cmd: str | None = None, resume_session: str | None = None):
        self.conn = KernelConnection(TABULA_URL)
        self.driver_cmd = driver_cmd
        self.session_id = resume_session or f"sess-{uuid4().hex[:8]}"
        self.driver_pid: int | None = None
        self.console = Console(
            theme=Theme(
                {
                    "markdown.heading": f"bold {ACCENT}",
                    "markdown.link": LINK_COLOR,
                    "markdown.link_url": DIM,
                    "markdown.code": CODE_COLOR,
                    "markdown.item.bullet": ACCENT_SOFT,
                }
            )
        )
        self.state = TurnState()
        self.alive = True
        self._tty = None
        self._events: queue.Queue[tuple[str, str]] = queue.Queue()
        self._inputs: queue.Queue[str | None] = queue.Queue()

    def connect(self):
        self.conn.send(
            {
                "type": "connect",
                "name": f"cli-{self.session_id}",
                "sends": ["message", "cancel", "tool_use"],
                "receives": ["stream_start", "stream_delta", "stream_end", "done", "error", "tool_result", "status"],
            }
        )
        self.conn.recv()
        self.conn.send({"type": "join", "session": self.session_id})
        self.conn.recv()

        if self.driver_cmd:
            self._spawn_driver()

    def _spawn_driver(self):
        """SPAWN a dedicated LLM driver for this session."""
        spawn_cmd = f"{self.driver_cmd} --session {self.session_id}"
        self.conn.send({
            "type": "tool_use",
            "id": "spawn-driver",
            "name": "SPAWN",
            "input": {"command": spawn_cmd},
        })
        # Wait for tool_result
        deadline = time.time() + 15
        while time.time() < deadline:
            msg = self.conn.recv(timeout=15)
            if msg is None:
                raise RuntimeError("lost connection while spawning driver")
            if msg.get("type") == "tool_result" and msg.get("id") == "spawn-driver":
                output = msg.get("output", "")
                m = re.match(r"PID (\d+)", output)
                if m:
                    self.driver_pid = int(m.group(1))
                    return
                raise RuntimeError(f"driver spawn failed: {output}")
        raise RuntimeError("timeout waiting for driver spawn")

    def _kill_driver(self):
        """KILL the driver process if we spawned one."""
        if self.driver_pid is None:
            return
        try:
            self.conn.send({
                "type": "tool_use",
                "id": "kill-driver",
                "name": "KILL",
                "input": {"pid": self.driver_pid},
            })
        except Exception:
            pass

    # ── UI helpers ──────────────────────────────────────────────

    def _rule_with_session(self):
        width = self.console.width or 80
        label = f" {self.session_id} "
        left = max(1, width - len(label) - 2)
        line = Text()
        line.append("─" * left, style=ACCENT)
        line.append(label, style=f"bold #3D3220 on {ACCENT}")
        line.append("──", style=ACCENT)
        self.console.print(line)

    def _rule(self):
        width = self.console.width or 80
        self.console.print(Text("─" * width, style=ACCENT))

    def _pick_phrase(self) -> str:
        return random.choice(WAITING_PHRASES)

    def _render_spinner(self) -> Text:
        elapsed = max(time.time() - self.state.started_at, 0.0)
        tick = int(elapsed * 2)
        frame = SPINNER_FRAMES[tick % len(SPINNER_FRAMES)]
        phrase = self.state.waiting_phrase or "thinking"
        return Text.assemble(
            (f"{frame} ", f"bold {ACCENT}"),
            (phrase, ACCENT_SOFT),
            ("…", ACCENT_SOFT),
            (f"  {elapsed:.0f}s", DIM),
        )

    def _render_markdown(self, text: str):
        try:
            return Padding(Markdown(text), (0, 0, 0, 2))
        except Exception:
            return Padding(Text(text), (0, 0, 0, 2))

    # ── Network threads ────────────────────────────────────────

    def _receiver(self):
        while self.alive:
            msg = self.conn.recv()
            if msg is None:
                self._events.put(("disconnect", ""))
                return

            msg_type = msg.get("type")
            if msg_type == "stream_start":
                self._events.put(("stream_start", ""))
            elif msg_type == "stream_delta":
                self._events.put(("stream_delta", msg.get("text", "")))
            elif msg_type == "stream_end":
                self._events.put(("stream_end", ""))
            elif msg_type == "done":
                self._events.put(("done", ""))
            elif msg_type == "error":
                self._events.put(("error", msg.get("text", "unknown error")))
            elif msg_type == "status":
                self._events.put(("status", msg.get("text", "")))
            # tool_result is handled only during spawn/kill, ignore here

    def _input_reader(self):
        try:
            while self.alive:
                line = self._tty.readline()
                if not line:
                    self._inputs.put(None)
                    return
                self._inputs.put(line.rstrip("\n"))
        except (OSError, ValueError):
            self._inputs.put(None)

    # ── Turn management ────────────────────────────────────────

    def _start_turn(self):
        self.state.waiting = True
        self.state.streaming = False
        self.state.started_at = time.time()
        self.state.current_text = ""
        self.state.rendered_text = ""
        self.state.error_text = ""
        self.state.finished_blocks = []
        self.state.response_started = False
        self.state.waiting_phrase = self._pick_phrase()

    def _apply_event(self, kind: str, payload: str):
        if kind == "stream_start":
            self.state.streaming = True
            self.state.waiting = False
        elif kind == "stream_delta":
            self.state.current_text += payload
        elif kind == "stream_end":
            self.state.streaming = False
            if self.state.current_text.strip():
                self.state.finished_blocks.append(self.state.current_text)
                self.state.current_text = ""
                self.state.rendered_text = ""
        elif kind == "done":
            self.state.waiting = False
            self.state.streaming = False
        elif kind == "error":
            self.state.error_text = payload
        elif kind == "status":
            if payload:
                self.state.waiting_phrase = payload
                self.state.waiting = True
            else:
                self.state.waiting_phrase = self._pick_phrase()
        elif kind == "disconnect":
            self.alive = False

    def _print_finished_blocks(self):
        while self.state.finished_blocks:
            block = self.state.finished_blocks.pop(0)
            if block.strip():
                if not self.state.response_started:
                    self.state.response_started = True
                    lines = block.strip().split("\n", 1)
                    self.console.print(
                        Text.assemble(("✦ ", f"bold {ACCENT}"), (lines[0], ""))
                    )
                    if len(lines) > 1:
                        self.console.print(self._render_markdown(lines[1]))
                else:
                    self.console.print(self._render_markdown(block))
                self.console.print()

    def _render_active(self, live: Live):
        if self.state.error_text:
            live.update(Text(f"error: {self.state.error_text}", style=f"bold {ERROR_COLOR}"))
            self.state.error_text = ""
            return
        if self.state.current_text:
            if self.state.current_text != self.state.rendered_text:
                if not self.state.response_started:
                    lines = self.state.current_text.strip().split("\n", 1)
                    first = Text.assemble(("✦ ", f"bold {ACCENT}"), (lines[0], ""))
                    if len(lines) > 1:
                        rest = self._render_markdown(lines[1])
                        live.update(Group(first, rest))
                    else:
                        live.update(first)
                else:
                    live.update(self._render_markdown(self.state.current_text))
                self.state.rendered_text = self.state.current_text
            return
        live.update(self._render_spinner())

    # ── Main loop ──────────────────────────────────────────────

    def run(self):
        def handle_sigint(sig, frame):
            if self.state.waiting or self.state.streaming:
                self.conn.send({"type": "cancel"})
                return
            self._print_resume_hint()
            self.alive = False

        signal.signal(signal.SIGINT, handle_sigint)

        try:
            if sys.platform == "win32":
                self._tty = sys.stdin
            else:
                self._tty = open("/dev/tty", "r")
        except OSError:
            sys.exit(1)

        recv_thread = threading.Thread(target=self._receiver, daemon=True)
        recv_thread.start()

        input_thread = threading.Thread(target=self._input_reader, daemon=True)
        input_thread.start()

        self.console.print()
        self.console.print(
            Text.assemble(("✦ ", f"bold {ACCENT}"), (random.choice(GREETING_PHRASES), f"italic {ACCENT_SOFT}"))
        )
        self.console.print()

        while self.alive:
            self._rule_with_session()
            # Print prompt, then bottom rule below, cursor back on prompt line
            width = self.console.width or 80
            sys.stdout.write(f"{ANSI_ACCENT}❯{ANSI_RESET} ")
            sys.stdout.write(f"\n{ANSI_ACCENT}{'─' * width}{ANSI_RESET}")
            sys.stdout.write(f"{MOVE_UP}\r\033[2C")  # back to prompt line, col 3
            sys.stdout.flush()

            user_input = None
            while self.alive and user_input is None:
                try:
                    event = self._events.get_nowait()
                    self._apply_event(*event)
                    if event[0] == "stream_start":
                        # Clear prompt + bottom rule + top rule
                        sys.stdout.write(f"{CLEAR_LINE}{MOVE_UP}{CLEAR_LINE}{MOVE_UP}{CLEAR_LINE}")
                        sys.stdout.flush()
                        break
                except queue.Empty:
                    pass

                try:
                    raw = self._inputs.get(timeout=0.1)
                except queue.Empty:
                    continue

                if raw is None:
                    self.alive = False
                    break
                if raw == "\x03":  # Ctrl+C from raw mode
                    if self.state.waiting or self.state.streaming:
                        self.conn.send({"type": "cancel"})
                    else:
                        self._print_resume_hint()
                        self.alive = False
                    break
                user_input = raw

            if not self.alive:
                break

            unsolicited = user_input is None
            if not unsolicited:
                if not user_input.strip():
                    # Clear frame and redraw
                    sys.stdout.write(f"{CLEAR_LINE}{MOVE_UP}{CLEAR_LINE}{MOVE_UP}{CLEAR_LINE}")
                    sys.stdout.flush()
                    continue
                # After enter, cursor is on bottom rule line
                # Clear bottom rule, go up, clear prompt, clear top rule
                sys.stdout.write(f"{CLEAR_LINE}{MOVE_UP}{CLEAR_LINE}")
                sys.stdout.write(f"{MOVE_UP}{CLEAR_LINE}")
                sys.stdout.flush()
                # Print user input with subtle background highlight
                self.console.print()
                width = self.console.width or 80
                padding = " " * max(0, width - 2 - len(user_input))
                prompt_text = Text.assemble(
                    ("❯ ", f"{DIM} on #333333"),
                    (user_input + padding, f"{USER_TEXT} on #333333"),
                )
                self.console.print(prompt_text)
                self.console.print()
                self._start_turn()
                self.conn.send({"type": "message", "text": user_input})
            else:
                self._start_turn()
                self.state.streaming = True
                self.state.waiting = False

            with Live(self._render_spinner(), console=self.console, refresh_per_second=12, transient=True) as live:
                turn_done = False
                while self.alive and not turn_done:
                    self._render_active(live)
                    self._print_finished_blocks()
                    try:
                        event = self._events.get(timeout=0.08)
                    except queue.Empty:
                        continue
                    self._apply_event(*event)
                    if event[0] == "done":
                        turn_done = True
                    elif event[0] == "disconnect":
                        turn_done = True

            self._print_finished_blocks()
            if not self.alive:
                break

            elapsed = time.time() - self.state.started_at if self.state.started_at else 0.0
            if elapsed >= 60:
                mins = int(elapsed // 60)
                secs = int(elapsed % 60)
                elapsed_str = f"{mins}m {secs}s"
            else:
                elapsed_str = f"{elapsed:.1f}s"
            phrase = random.choice(ELAPSED_PHRASES)

            self.console.print(
                Text.assemble(("✧ ", DIM), (f"{phrase} · {elapsed_str}", DIM))
            )
            self.console.print()
            self.state.reset()

        self.alive = False
        self._kill_driver()
        self._print_resume_hint()
        self.conn.close()
        if self._tty:
            self._tty.close()

    def _print_resume_hint(self):
        self.console.print()
        self.console.print(
            Text.assemble(
                ("  --resume ", f"bold {ACCENT_SOFT}"),
                (self.session_id, f"bold {ACCENT}"),
            )
        )
        self.console.print()


def main():
    parser = argparse.ArgumentParser(description="Tabula CLI gateway")
    parser.add_argument("--driver", default=None, help="Driver command to spawn for this session")
    parser.add_argument("--resume", default=None, metavar="SESSION", help="Resume an existing session by ID")
    args = parser.parse_args()

    gateway = Gateway(driver_cmd=args.driver, resume_session=args.resume)
    gateway.connect()
    gateway.run()


if __name__ == "__main__":
    main()

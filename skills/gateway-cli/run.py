#!/usr/bin/env python3
"""Interactive CLI gateway for Tabula — minimal UI with raw input and spinner."""

from __future__ import annotations

import argparse
import os
import queue
import re
import signal
import sys
import threading
import time
from uuid import uuid4

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from skills.lib.kernel_client import KernelConnection

TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")

SPINNER = "|/-\\"
PROMPT = "> "


class RawInput:
    """Character-by-character line editor using raw terminal mode."""

    def __init__(self, fd: int):
        import termios
        import tty
        self._fd = fd
        self._old_attrs = termios.tcgetattr(fd)
        tty.setraw(fd)

    def restore(self):
        import termios
        termios.tcsetattr(self._fd, termios.TCSADRAIN, self._old_attrs)

    def read_char(self) -> str | None:
        """Read a single UTF-8 character. Returns None on EOF.
        Consumes and discards escape sequences (arrows, Shift+Enter, etc.)."""
        b = os.read(self._fd, 1)
        if not b:
            return None
        first = b[0]

        # Escape sequence — consume; may return a mapped key (e.g. Shift+Enter → \n)
        if first == 0x1B:
            return self._drain_escape()

        # Determine how many bytes this UTF-8 character needs
        if first < 0x80:
            return b.decode("utf-8")
        elif first < 0xC0:
            return ""  # unexpected continuation byte
        elif first < 0xE0:
            need = 1
        elif first < 0xF0:
            need = 2
        else:
            need = 3
        rest = os.read(self._fd, need)
        if len(rest) < need:
            return ""
        return (b + rest).decode("utf-8", errors="replace")

    def _drain_escape(self) -> str:
        """Read an escape sequence after ESC byte. Returns a mapped key or ""."""
        import select
        r, _, _ = select.select([self._fd], [], [], 0.05)
        if not r:
            return ""  # bare ESC
        b = os.read(self._fd, 1)
        if not b:
            return ""
        ch = b[0]
        if ch == ord("["):
            # CSI sequence: collect parameter bytes then final byte
            params = bytearray()
            while True:
                r, _, _ = select.select([self._fd], [], [], 0.05)
                if not r:
                    return ""
                b = os.read(self._fd, 1)
                if not b:
                    return ""
                if 0x40 <= b[0] <= 0x7E:
                    # Shift+Enter: ESC[27;2;13~
                    if b[0] == ord("~") and params == bytearray(b"27;2;13"):
                        return "\n"
                    return ""
                params.extend(b)
        elif ch == ord("O"):
            # SS3 sequence: one more byte
            r, _, _ = select.select([self._fd], [], [], 0.05)
            if r:
                os.read(self._fd, 1)
        return ""


class Gateway:
    def __init__(self, driver_cmd: str | None = None, resume_session: str | None = None):
        self.conn = KernelConnection(TABULA_URL)
        self.driver_cmd = driver_cmd
        self.session_id = resume_session or f"sess-{uuid4().hex[:8]}"
        self.driver_pid: int | None = None
        self.alive = True
        self.in_turn = False
        self._raw: RawInput | None = None
        self._tty_fd: int = -1
        self._tty_file = None
        self._events: queue.Queue[tuple[str, str]] = queue.Queue()
        self._resume_printed = False
        self._wake_w: int = -1

    # ── Connection ─────────────────────────────────────────────

    def connect(self):
        self.conn.send(
            {
                "type": "connect",
                "name": f"cli-{self.session_id}",
                "sends": ["message", "cancel", "tool_use"],
                "receives": [
                    "stream_start", "stream_delta", "stream_end",
                    "done", "error", "tool_result", "status",
                ],
            }
        )
        self.conn.recv()
        self.conn.send({"type": "join", "session": self.session_id})
        self.conn.recv()

        if self.driver_cmd:
            self._spawn_driver()

    def _spawn_driver(self):
        spawn_cmd = f"{self.driver_cmd} --session {self.session_id}"
        self.conn.send({
            "type": "tool_use",
            "id": "spawn-driver",
            "name": "SPAWN",
            "input": {"command": spawn_cmd},
        })
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

    # ── Terminal helpers ───────────────────────────────────────

    def _write(self, text: str):
        # In raw mode \n alone doesn't return to column 0; always use \r\n.
        text = text.replace("\r\n", "\n").replace("\n", "\r\n")
        os.write(self._tty_fd, text.encode())

    def _clear_line(self):
        self._write("\r\033[K")

    # ── Receiver thread ───────────────────────────────────────

    def _wake(self):
        if self._wake_w >= 0:
            try:
                os.write(self._wake_w, b"\x00")
            except OSError:
                pass

    def _receiver(self):
        while self.alive:
            msg = self.conn.recv()
            if msg is None:
                self._events.put(("disconnect", ""))
                self._wake()
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
            self._wake()

    # ── Input (raw mode) ──────────────────────────────────────

    def _read_line(self) -> str | None:
        """Read a line in raw mode with basic line editing. Returns None on EOF/exit.
        Returns empty string if an unsolicited server event arrives."""
        import select
        buf: list[str] = []
        # Use a pipe so the receiver thread can wake us from select()
        wake_r, wake_w = os.pipe()
        self._wake_w = wake_w

        self._write(PROMPT)

        try:
            while self.alive:
                # Check for unsolicited server events first
                try:
                    event = self._events.get_nowait()
                    if event[0] == "stream_start":
                        self._clear_line()
                        self._events.put(event)
                        return ""
                    elif event[0] == "disconnect":
                        self.alive = False
                        return None
                    else:
                        self._events.put(event)
                except queue.Empty:
                    pass

                # Wait for tty input or wake signal (from receiver thread putting event)
                r, _, _ = select.select([self._tty_fd, wake_r], [], [], 0.2)

                if wake_r in r:
                    os.read(wake_r, 64)  # drain wake pipe
                    continue  # loop back to check events

                if self._tty_fd not in r:
                    continue

                ch = self._raw.read_char()
                if ch is None:
                    return None
                if ch == "":
                    continue  # consumed escape sequence

                if ch == "\r":
                    self._write("\r\n")
                    return "".join(buf)
                elif ch == "\n":
                    # Shift+Enter: newline within input (multiline)
                    buf.append("\n")
                    self._write("\r\n  ")  # continuation indent
                elif ch == "\x03":  # Ctrl+C
                    if buf:
                        # Clear current input
                        self._clear_line()
                        self._write(PROMPT)
                        buf.clear()
                    else:
                        # Empty prompt — exit
                        self._write("\r\n")
                        self.alive = False
                        return None
                elif ch == "\x04":  # Ctrl+D
                    if not buf:
                        self._write("\r\n")
                        self.alive = False
                        return None
                elif ch == "\x7f" or ch == "\x08":  # Backspace
                    if buf:
                        buf.pop()
                        self._write("\b \b")
                elif ch == "\x15":  # Ctrl+U: clear line
                    self._clear_line()
                    self._write(PROMPT)
                    buf.clear()
                elif ch == "\x17":  # Ctrl+W: delete word
                    while buf and buf[-1] == " ":
                        buf.pop()
                        self._write("\b \b")
                    while buf and buf[-1] != " ":
                        buf.pop()
                        self._write("\b \b")
                elif ch >= " ":  # Printable
                    buf.append(ch)
                    self._write(ch)
        finally:
            os.close(wake_r)
            os.close(wake_w)
            self._wake_w = -1

        return None

    # ── Spinner ───────────────────────────────────────────────

    def _show_spinner(self, stop_event: threading.Event):
        """Show a simple spinner until stop_event is set."""
        tick = 0
        while not stop_event.wait(0.15):
            frame = SPINNER[tick % len(SPINNER)]
            elapsed = time.time() - self._turn_started
            self._clear_line()
            self._write(f"{frame} {elapsed:.0f}s")
            tick += 1
        self._clear_line()

    # ── Turn processing ────────────────────────────────────────

    def _cancel_turn(self):
        """Send cancel and return immediately. Stale events flushed before next turn."""
        self.conn.send({"type": "cancel"})
        self._write("\n[cancelled]\n\n")

    def _flush_events(self):
        """Discard any leftover events from a previous turn."""
        while True:
            try:
                self._events.get_nowait()
            except queue.Empty:
                return

    def _check_cancel(self) -> bool:
        """Non-blocking check if user pressed Ctrl+C or ESC. Returns True if cancelled."""
        import select
        r, _, _ = select.select([self._tty_fd], [], [], 0)
        if not r:
            return False
        b = os.read(self._tty_fd, 1)
        if not b:
            return False
        if b[0] == 0x03:  # Ctrl+C
            return True
        if b[0] == 0x1B:  # ESC — but could be start of escape sequence
            r2, _, _ = select.select([self._tty_fd], [], [], 0.05)
            if not r2:
                return True  # bare ESC
            b2 = os.read(self._tty_fd, 1)
            if b2 and b2[0] == ord("["):
                # CSI sequence — drain it, not a cancel
                while True:
                    r3, _, _ = select.select([self._tty_fd], [], [], 0.05)
                    if not r3:
                        break
                    b3 = os.read(self._tty_fd, 1)
                    if not b3 or (0x40 <= b3[0] <= 0x7E):
                        break
                return False
            # Other ESC+char — treat as cancel
            return True
        return False

    def _process_turn(self):
        self._turn_started = time.time()
        has_output = False
        response_started = False
        waiting = True
        spinner_stop = threading.Event()
        spinner_thread = threading.Thread(target=self._show_spinner, args=(spinner_stop,), daemon=True)
        spinner_thread.start()

        while self.alive:
            # Check for cancel input
            if self._check_cancel():
                if waiting:
                    spinner_stop.set()
                    spinner_thread.join()
                if has_output:
                    self._write("\n")
                self._cancel_turn()
                return

            try:
                kind, payload = self._events.get(timeout=0.05)
            except queue.Empty:
                continue

            if kind == "stream_start":
                if waiting:
                    waiting = False
                    spinner_stop.set()
                    spinner_thread.join()
            elif kind == "stream_delta":
                if waiting:
                    waiting = False
                    spinner_stop.set()
                    spinner_thread.join()
                if not response_started:
                    response_started = True
                    payload = payload.lstrip("\n")
                    self._write("\n---\n\n")
                    if not payload:
                        continue
                self._write(payload)
                has_output = True
            elif kind == "stream_end":
                if has_output:
                    self._write("\n")
                    has_output = False
            elif kind == "done":
                if waiting:
                    spinner_stop.set()
                    spinner_thread.join()
                if has_output:
                    self._write("\n")
                elapsed = time.time() - self._turn_started
                if elapsed >= 60:
                    mins = int(elapsed // 60)
                    secs = int(elapsed % 60)
                    self._write(f"\n[{mins}m {secs}s]\n\n")
                else:
                    self._write(f"\n[{elapsed:.1f}s]\n\n")
                return
            elif kind == "error":
                if waiting:
                    spinner_stop.set()
                    spinner_thread.join()
                    waiting = False
                if has_output:
                    self._write("\n")
                    has_output = False
                self._write(f"error: {payload}\n")
            elif kind == "status":
                pass
            elif kind == "disconnect":
                spinner_stop.set()
                spinner_thread.join()
                if has_output:
                    self._write("\n")
                self._write("disconnected\n")
                self.alive = False
                return

    # ── Main loop ──────────────────────────────────────────────

    def run(self):
        # Ignore SIGINT — we handle Ctrl+C in raw mode
        signal.signal(signal.SIGINT, signal.SIG_IGN)

        try:
            if sys.platform == "win32":
                self._tty_fd = sys.stdin.fileno()
            else:
                self._tty_file = open("/dev/tty", "r+b", buffering=0)
                self._tty_fd = self._tty_file.fileno()
        except OSError:
            sys.exit(1)

        try:
            self._raw = RawInput(self._tty_fd)
        except ImportError:
            # No termios (Windows) — fall back, but this path is basic
            self._raw = None

        recv_thread = threading.Thread(target=self._receiver, daemon=True)
        recv_thread.start()

        self._write(f"tabula [{self.session_id}]\n")

        try:
            while self.alive:
                line = self._read_line()

                if line is None:
                    break

                if line == "":
                    # Unsolicited event or empty input
                    # Check if there's an unsolicited turn waiting
                    try:
                        event = self._events.get_nowait()
                        if event[0] in ("stream_start", "stream_delta"):
                            self.in_turn = True
                            self._events.put(event)
                            self._process_turn()
                            self.in_turn = False
                        else:
                            self._events.put(event)
                    except queue.Empty:
                        pass
                    continue

                if not line.strip():
                    continue

                self.in_turn = True
                self._flush_events()
                self.conn.send({"type": "message", "text": line})
                self._process_turn()
                self.in_turn = False
        finally:
            if self._raw:
                self._raw.restore()
            self._print_resume_hint()
            self._kill_driver()
            self.conn.close()

    def _print_resume_hint(self):
        if self._resume_printed:
            return
        self._resume_printed = True
        # After raw.restore() terminal is in cooked mode, \n works normally.
        # stdout may be /dev/null when spawned by kernel, so write to tty fd.
        os.write(self._tty_fd, f"\n--resume {self.session_id}\n\n".encode())


def main():
    parser = argparse.ArgumentParser(description="Tabula CLI gateway")
    parser.add_argument("--driver", default=None, help="Driver command to spawn")
    parser.add_argument("--resume", default=None, metavar="SESSION", help="Resume session")
    args = parser.parse_args()

    gateway = Gateway(driver_cmd=args.driver, resume_session=args.resume)
    gateway.connect()
    gateway.run()


if __name__ == "__main__":
    main()

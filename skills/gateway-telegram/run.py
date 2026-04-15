#!/usr/bin/env python3
"""Telegram gateway for Tabula.

Polls Telegram Bot API, bridges messages to the kernel.
Each chat_id gets its own session + driver.

Streaming: uses sendMessageDraft for incremental response display,
then final sendMessage to save to chat history.

Pairing flow:
  /start -> generates token -> admin approves via pair.py
"""
from __future__ import annotations

import json
import os
import queue
import re
import sys
import threading
import time
import uuid
from datetime import datetime

ROOT = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from skills.lib import load_env
from skills.lib.kernel_client import KernelConnection
from skills.lib.protocol import (
    MSG_CONNECT, MSG_JOIN, MSG_JOINED, MSG_TOOL_USE, MSG_MESSAGE,
    MSG_TOOL_RESULT, MSG_MEMBER_JOINED, MSG_ERROR,
    MSG_STREAM_START, MSG_STREAM_DELTA, MSG_STREAM_END, MSG_DONE,
    TOOL_SPAWN, TOOL_KILL,
)
from skills.pair.run import is_authorized as _pair_is_authorized
from skills.pair.run import create_token as _pair_create_token

load_env()

import requests

# -- Config --------------------------------------------------------------------

GATEWAY_NAME  = "telegram"
TABULA_URL    = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
TABULA_HOME   = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
PROVIDER      = os.environ.get("TABULA_PROVIDER", "anthropic")

PROVIDER_ALIASES = {"claude": "anthropic", "gpt": "openai", "openclaw": "openai"}
ACTIVE_PROVIDER  = PROVIDER_ALIASES.get(PROVIDER, PROVIDER)

VENV_PYTHON  = os.path.join(TABULA_HOME, ".venv", "bin", "python3")
POLL_TIMEOUT = 30   # long-poll seconds
TOKEN_TTL    = 1800 # pairing token lifetime, seconds
ASK_TIMEOUT  = 300  # max wait for LLM response, seconds
DRAFT_THROTTLE = 0.1  # seconds between sendMessageDraft calls

# -- Logging -------------------------------------------------------------------

def log(msg: str):
    ts = datetime.now().strftime("%H:%M:%S")
    sys.stderr.write(f"[gateway-telegram] {ts} {msg}\n")
    sys.stderr.flush()

# -- Auth (delegates to skills/pair) -------------------------------------------

def is_authorized(chat_id: int) -> bool:
    return _pair_is_authorized(GATEWAY_NAME, chat_id)

def create_pairing_token(chat_id: int, username: str) -> str:
    return _pair_create_token(GATEWAY_NAME, chat_id, username, ttl=TOKEN_TTL)

# -- Markdown converter --------------------------------------------------------

_TGV2_SPECIAL = r'_*[]()~`>#+=|{}.!-'

def escape_tgv2(s: str) -> str:
    """Escape special chars for Telegram MarkdownV2 plain text."""
    return re.sub(r'([' + re.escape(_TGV2_SPECIAL) + r'])', r'\\\1', s)

def md_to_tgv2(text: str) -> str:
    """Convert standard Markdown (from LLM) to Telegram MarkdownV2.

    Strategy:
    - Extract code blocks and inline code first (preserve as-is)
    - Convert bold (**text** -> *text*) and italic (*text* -> _text_)
    - Convert headings (# Heading -> *Heading*)
    - Escape all MarkdownV2 special chars outside formatting
    """
    parts = []
    pattern = re.compile(r'(```[\s\S]*?```|`[^`\n]+`)')
    last = 0
    for m in pattern.finditer(text):
        before = text[last:m.start()]
        parts.append(_convert_markup(before))
        parts.append(m.group(0))
        last = m.end()
    parts.append(_convert_markup(text[last:]))
    return "".join(parts)


def _convert_markup(text: str) -> str:
    """Convert non-code markdown markup to TGv2, escaping plain text."""
    lines = text.split('\n')
    converted_lines = []
    for line in lines:
        m = re.match(r'^(#{1,6})\s+(.*)', line)
        if m:
            heading_text = _convert_inline(m.group(2))
            converted_lines.append(f'*{heading_text}*')
        else:
            converted_lines.append(_convert_inline(line))
    return '\n'.join(converted_lines)


def _convert_inline(text: str) -> str:
    """Convert inline bold/italic, escape plain text segments."""
    result = []
    pattern = re.compile(r'(\*\*(.+?)\*\*|\*(.+?)\*|_(.+?)_)')
    last = 0
    for m in pattern.finditer(text):
        result.append(escape_tgv2(text[last:m.start()]))
        full = m.group(0)
        if full.startswith('**'):
            inner = escape_tgv2(m.group(2))
            result.append(f'*{inner}*')
        elif full.startswith('*'):
            inner = escape_tgv2(m.group(3))
            result.append(f'_{inner}_')
        elif full.startswith('_'):
            inner = escape_tgv2(m.group(4))
            result.append(f'_{inner}_')
        last = m.end()
    result.append(escape_tgv2(text[last:]))
    return ''.join(result)


# -- Kernel session per chat ---------------------------------------------------

class SessionState:
    def __init__(self, session_id: str):
        self.session_id  = session_id
        self.conn        = KernelConnection(TABULA_URL)
        self.driver_pid: int | None = None
        self.events: queue.Queue[tuple[str, str]] = queue.Queue()
        self.alive       = True
        self._thread: threading.Thread | None = None

    def connect(self):
        driver_cmd = f"{VENV_PYTHON} skills/driver-{ACTIVE_PROVIDER}/run.py"
        self.conn.send({
            "type": MSG_CONNECT,
            "name": f"tg-{self.session_id}",
            "sends": [MSG_MESSAGE, MSG_TOOL_USE],
            "receives": [MSG_STREAM_START, MSG_STREAM_DELTA, MSG_STREAM_END, MSG_DONE, MSG_ERROR, MSG_TOOL_RESULT, MSG_MEMBER_JOINED],
        })
        self.conn.recv()  # connected
        self.conn.send({"type": MSG_JOIN, "session": self.session_id})
        self.conn.recv()  # joined

        # Spawn driver
        self.conn.send({
            "type": MSG_TOOL_USE,
            "name": TOOL_SPAWN,
            "id": "spawn-driver",
            "input": {"command": f"{driver_cmd} --session {self.session_id}"},
        })
        deadline = time.time() + 15
        while time.time() < deadline:
            msg = self.conn.recv(timeout=15)
            if msg is None:
                raise RuntimeError("lost connection while spawning driver")
            if msg.get("type") == MSG_TOOL_RESULT and msg.get("id") == "spawn-driver":
                m = re.match(r"PID (\d+)", msg.get("output", ""))
                if m:
                    self.driver_pid = int(m.group(1))
                    break
                raise RuntimeError(f"driver spawn failed: {msg.get('output')}")

        # Wait for driver to join
        deadline = time.time() + 10
        while time.time() < deadline:
            msg = self.conn.recv(timeout=10)
            if msg and msg.get("type") == MSG_MEMBER_JOINED:
                break

        self._thread = threading.Thread(target=self._receiver, daemon=True)
        self._thread.start()

    def _receiver(self):
        while self.alive:
            msg = self.conn.recv()
            if msg is None:
                self.events.put(("disconnect", ""))
                return
            t = msg.get("type")
            if t in (MSG_STREAM_START, MSG_STREAM_DELTA, MSG_STREAM_END, MSG_DONE, MSG_ERROR):
                self.events.put((t, msg.get("text", "")))

    def ask_stream(self, text: str):
        """Send message, yield response chunks as they arrive from kernel."""
        # Drain stale events
        while True:
            try:
                self.events.get_nowait()
            except queue.Empty:
                break

        self.conn.send({"type": MSG_MESSAGE, "text": text})

        while True:
            try:
                kind, payload = self.events.get(timeout=ASK_TIMEOUT)
            except queue.Empty:
                yield "[timeout waiting for response]"
                break
            if kind == "stream_delta":
                yield payload
            elif kind == "done":
                break
            elif kind == "error":
                yield f"\n[error: {payload}]"
                break
            elif kind == "disconnect":
                yield "\n[lost connection to kernel]"
                break

    def close(self):
        self.alive = False
        if self.driver_pid is not None:
            try:
                self.conn.send({"type": MSG_TOOL_USE, "name": TOOL_KILL, "id": "kill-driver",
                                "input": {"pid": self.driver_pid}})
            except Exception:
                pass
        self.conn.close()


# -- Slash command discovery ---------------------------------------------------

def _discover_slash_commands() -> list[dict]:
    """Scan skills/ for user-invocable skills."""
    skills_dir = os.path.join(TABULA_HOME, "skills")
    commands = []
    if not os.path.isdir(skills_dir):
        return commands
    for name in sorted(os.listdir(skills_dir)):
        skill_md = os.path.join(skills_dir, name, "SKILL.md")
        if not os.path.isfile(skill_md):
            continue
        with open(skill_md) as f:
            raw = f.read().strip()
        if not raw.startswith("---"):
            continue
        end = raw.find("---", 3)
        if end == -1:
            continue
        frontmatter = raw[3:end]
        body = raw[end+3:].strip()
        if not re.search(r'user-invocable:\s*true', frontmatter, re.I):
            continue
        m = re.search(r'^name:\s*(.+)$', frontmatter, re.M)
        skill_name = m.group(1).strip() if m else name
        m = re.search(r'^description:\s*(.+)$', frontmatter, re.M)
        description = m.group(1).strip().strip('"') if m else ""
        commands.append({"name": skill_name, "description": description, "body": body})
    return commands


# -- Bot instance (one per token) ----------------------------------------------

class BotInstance:
    """One Telegram bot token = one BotInstance with its own polling loop."""

    def __init__(self, token: str, gateway: "TelegramGateway"):
        self.token = token
        self.gateway = gateway
        self.TG_API = f"https://api.telegram.org/bot{token}"

    def tg(self, method: str, **kwargs) -> dict:
        return requests.post(f"{self.TG_API}/{method}", json=kwargs, timeout=10).json()

    def send_message(self, chat_id: int, text: str, parse_mode: str = ""):
        kwargs: dict = {"chat_id": chat_id, "text": text}
        if parse_mode:
            kwargs["parse_mode"] = parse_mode
        resp = self.tg("sendMessage", **kwargs)
        if not resp.get("ok"):
            log(f"sendMessage failed: {resp}")
            if parse_mode:
                resp2 = self.tg("sendMessage", chat_id=chat_id, text=text)
                if not resp2.get("ok"):
                    log(f"sendMessage retry failed: {resp2}")

    def send_typing(self, chat_id: int):
        self.tg("sendChatAction", chat_id=chat_id, action="typing")

    def send_draft(self, chat_id: int, draft_id: str, text: str):
        """Send a streaming draft via sendMessageDraft."""
        self.tg("sendMessageDraft",
                chat_id=chat_id,
                draft_id=draft_id,
                text=text,
                parse_mode="MarkdownV2")

    def run(self):
        me = self.tg("getMe").get("result", {})
        log(f"bot @{me.get('username', '?')} started, provider={ACTIVE_PROVIDER}")
        offset = 0
        while True:
            try:
                resp = requests.get(
                    f"{self.TG_API}/getUpdates",
                    params={"timeout": POLL_TIMEOUT, "offset": offset},
                    timeout=POLL_TIMEOUT + 5,
                ).json()
                if not resp.get("ok"):
                    log(f"getUpdates error: {resp}")
                    time.sleep(5)
                    continue
                for update in resp.get("result", []):
                    offset = update["update_id"] + 1
                    try:
                        self.gateway.handle_update(update, bot=self)
                    except Exception as e:
                        log(f"update handling error: {e}")
            except requests.RequestException as e:
                log(f"network error: {e}")
                time.sleep(5)


# -- Gateway -------------------------------------------------------------------

class TelegramGateway:
    def __init__(self):
        self.sessions:  dict[int, SessionState] = {}
        self._lock      = threading.Lock()
        self._commands: dict[str, dict] = {}
        self._load_slash_commands()

    def _load_slash_commands(self):
        commands = _discover_slash_commands()
        for cmd in commands:
            tg_name = cmd["name"].replace("-", "_")
            self._commands[tg_name] = cmd
        # Register commands for all bots (done per-bot in BotInstance.run)
        # We'll register once here using the first bot's token later.

    def _get_session(self, chat_id: int) -> SessionState:
        with self._lock:
            if chat_id not in self.sessions:
                sid   = f"tg-{chat_id}"
                state = SessionState(sid)
                state.connect()
                self.sessions[chat_id] = state
                log(f"new session for chat_id={chat_id}: {sid}")
            return self.sessions[chat_id]

    def handle_update(self, update: dict, bot: BotInstance):
        msg = update.get("message") or update.get("edited_message")
        if not msg:
            return

        chat_id  = msg["chat"]["id"]
        text     = msg.get("text", "").strip()
        username = msg.get("from", {}).get("username", str(chat_id))

        if not text:
            return

        # /start -- pairing flow
        if text == "/start":
            if is_authorized(chat_id):
                bot.send_message(chat_id, r"You're already authorized\. Just write me something", parse_mode="MarkdownV2")
            else:
                token = create_pairing_token(chat_id, username)
                bot.send_message(
                    chat_id,
                    r"To get access, you need to go through pairing\." + "\n\n"
                    r"Your token:" + f"\n\n`{token}`\n\n"
                    r"Send it to the admin for approval\.",
                    parse_mode="MarkdownV2",
                )
            return

        # Not authorized
        if not is_authorized(chat_id):
            bot.send_message(
                chat_id,
                r"Access denied\. Send /start to request pairing\.",
                parse_mode="MarkdownV2",
            )
            return

        # Slash commands (user-invocable skills)
        if text.startswith("/"):
            parts = text[1:].split(None, 1)
            cmd_name = parts[0].split("@")[0].lower()
            args = parts[1] if len(parts) > 1 else ""
            cmd = self._commands.get(cmd_name)
            if cmd:
                log(f"slash command /{cmd_name} from {chat_id} args={args!r}")
                full_text = cmd["body"] + (f"\n\nUser request: {args}" if args else "")
                threading.Thread(
                    target=self._process_message,
                    args=(chat_id, full_text, bot),
                    daemon=True,
                ).start()
                return

        # Authorized -- route to kernel
        log(f"message from chat_id={chat_id} (@{username}): {text[:60]}")
        bot.send_typing(chat_id)
        threading.Thread(
            target=self._process_message,
            args=(chat_id, text, bot),
            daemon=True,
        ).start()

    def _process_message(self, chat_id: int, text: str, bot: BotInstance):
        try:
            session = self._get_session(chat_id)
            bot.send_typing(chat_id)

            draft_id = str(uuid.uuid4())[:8]
            full_text = ""
            last_draft = 0.0

            for delta in session.ask_stream(text):
                full_text += delta
                now = time.time()

                if now - last_draft >= DRAFT_THROTTLE:
                    try:
                        bot.send_draft(chat_id, draft_id, md_to_tgv2(full_text))
                    except Exception:
                        pass  # ignore draft failures
                    last_draft = now

            # Final message (saved to chat history)
            if full_text:
                for chunk in _split_text(md_to_tgv2(full_text), 4096):
                    bot.send_message(chat_id, chunk, parse_mode="MarkdownV2")
            else:
                bot.send_message(chat_id, "_(empty response)_", parse_mode="MarkdownV2")
        except Exception as e:
            log(f"error processing message from {chat_id}: {e}")
            bot.send_message(chat_id, f"Internal error: {escape_tgv2(str(e))}")

    def shutdown(self):
        with self._lock:
            for session in self.sessions.values():
                try:
                    session.close()
                except Exception:
                    pass
            self.sessions.clear()


def _split_text(text: str, limit: int) -> list[str]:
    """Split text into chunks, preferring newline boundaries."""
    if len(text) <= limit:
        return [text]
    chunks = []
    while text:
        if len(text) <= limit:
            chunks.append(text)
            break
        cut = text.rfind('\n', 0, limit)
        if cut <= 0:
            cut = text.rfind(' ', 0, limit)
        if cut <= 0:
            cut = limit
        chunks.append(text[:cut])
        text = text[cut:].lstrip('\n')
    return chunks


# -- Token resolution ----------------------------------------------------------

def resolve_bot_tokens() -> list[str]:
    """Resolve bot tokens from TELEGRAM_BOT_TOKENS (comma-separated)."""
    tokens = os.environ.get("TELEGRAM_BOT_TOKENS", "").strip()
    if not tokens:
        return []
    return [t.strip() for t in tokens.split(",") if t.strip()]


# -- Entry point ---------------------------------------------------------------

_PID_FILE = os.path.join(TABULA_HOME, "gateway-telegram.pid")


def _check_pid_file() -> bool:
    """Return True if another instance is already running."""
    if not os.path.isfile(_PID_FILE):
        return False
    try:
        with open(_PID_FILE) as f:
            pid = int(f.read().strip())
        os.kill(pid, 0)  # signal 0 = check if alive
        return True
    except (ValueError, ProcessLookupError, PermissionError):
        # Stale PID file — remove it
        try:
            os.remove(_PID_FILE)
        except OSError:
            pass
        return False


def _write_pid_file():
    with open(_PID_FILE, "w") as f:
        f.write(str(os.getpid()))


def _remove_pid_file():
    try:
        os.remove(_PID_FILE)
    except OSError:
        pass


def main():
    tokens = resolve_bot_tokens()
    if not tokens:
        sys.exit("TELEGRAM_BOT_TOKENS is not set. Add to ~/.tabula/.env")

    if _check_pid_file():
        sys.exit("gateway-telegram is already running. Remove ~/.tabula/gateway-telegram.pid to force start.")

    _write_pid_file()
    try:
        _run_gateway(tokens)
    finally:
        _remove_pid_file()


def _run_gateway(tokens):
    gateway = TelegramGateway()

    # Register slash commands with the first bot
    if gateway._commands:
        first_bot = BotInstance(tokens[0], gateway)
        tg_commands = [
            {"command": name, "description": cmd["description"][:256]}
            for name, cmd in gateway._commands.items()
        ]
        resp = first_bot.tg("setMyCommands", commands=tg_commands)
        if resp.get("ok"):
            log(f"registered {len(tg_commands)} commands with Telegram")
        else:
            log(f"setMyCommands failed: {resp}")

    # Start one polling thread per bot token
    for token in tokens:
        bot = BotInstance(token, gateway)
        threading.Thread(target=bot.run, daemon=True).start()

    # Keep main thread alive
    try:
        while True:
            time.sleep(3600)
    except KeyboardInterrupt:
        gateway.shutdown()


if __name__ == "__main__":
    main()

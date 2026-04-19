#!/usr/bin/env python3
"""Unit tests for the Telegram gateway skill."""

from __future__ import annotations

import json
import os
import queue
import sys
import tempfile
import threading
import time
import types
import unittest
from unittest.mock import MagicMock, patch

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

if "websocket" not in sys.modules:
    fake_websocket = types.ModuleType("websocket")

    class _FakeTimeout(Exception):
        pass

    class _FakeClosed(Exception):
        pass

    def _unexpected_create_connection(_url: str):
        raise RuntimeError("websocket.create_connection should be patched in tests")

    fake_websocket.create_connection = _unexpected_create_connection
    fake_websocket.WebSocketTimeoutException = _FakeTimeout
    fake_websocket.WebSocketConnectionClosedException = _FakeClosed
    sys.modules["websocket"] = fake_websocket

if "requests" not in sys.modules:
    fake_requests = types.ModuleType("requests")

    class _FakeRequestException(Exception):
        pass

    def _unexpected_requests_call(*args, **kwargs):
        raise RuntimeError("requests should be patched in tests")

    fake_requests.get = _unexpected_requests_call
    fake_requests.post = _unexpected_requests_call
    fake_requests.RequestException = _FakeRequestException
    sys.modules["requests"] = fake_requests

# gateway-telegram has a hyphen, so we can't import it as a Python package.
# Load the module directly from its file path.
import importlib.util

def _load_gateway_module():
    gw_path = os.path.join(ROOT, "distrib", "assistant", "skills", "gateway-telegram", "run.py")
    spec = importlib.util.spec_from_file_location("gateway_run", gw_path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def _load_gateway_module_for_home(home: str, extra_env: dict[str, str] | None = None):
    env = {"TABULA_HOME": home}
    if extra_env:
        env.update(extra_env)
    with patch.dict(os.environ, env, clear=True):
        return _load_gateway_module()


def _write_global_config(home: str, provider: str | None = None, extra: str = ""):
    cfg_dir = os.path.join(home, "config")
    os.makedirs(cfg_dir, exist_ok=True)
    parts = []
    if provider:
        parts.append(f'provider = "{provider}"')
    if extra:
        parts.append(extra.strip())
    if parts:
        with open(os.path.join(cfg_dir, "global.toml"), "w") as f:
            f.write("\n\n".join(parts) + "\n")

# Load with repository home so provider resolution sees built-in drivers.
with patch.dict(os.environ, {"TABULA_HOME": ROOT}, clear=True):
    _gw = _load_gateway_module()
    escape_tgv2 = _gw.escape_tgv2
    md_to_tgv2 = _gw.md_to_tgv2
    _split_text = _gw._split_text
    resolve_bot_tokens = _gw.resolve_bot_tokens
    BotInstance = _gw.BotInstance
    TelegramGateway = _gw.TelegramGateway
    SessionState = _gw.SessionState
    DRAFT_THROTTLE = _gw.DRAFT_THROTTLE


# -- resolve_bot_tokens --

class TestResolveBotTokens(unittest.TestCase):
    def test_empty(self):
        with patch.dict(os.environ, {"TELEGRAM_BOT_TOKENS": ""}, clear=False):
            os.environ.pop("TELEGRAM_BOT_TOKENS", None)
            self.assertEqual(resolve_bot_tokens(), [])

    def test_single_token(self):
        with patch.dict(os.environ, {"TELEGRAM_BOT_TOKENS": "123:ABC"}, clear=False):
            self.assertEqual(resolve_bot_tokens(), ["123:ABC"])

    def test_multiple_tokens(self):
        with patch.dict(os.environ, {"TELEGRAM_BOT_TOKENS": "123:ABC,456:DEF"}, clear=False):
            self.assertEqual(resolve_bot_tokens(), ["123:ABC", "456:DEF"])

    def test_strips_whitespace(self):
        with patch.dict(os.environ, {"TELEGRAM_BOT_TOKENS": "  123:ABC  ,  456:DEF  "}, clear=False):
            self.assertEqual(resolve_bot_tokens(), ["123:ABC", "456:DEF"])

    def test_empty_entries_ignored(self):
        with patch.dict(os.environ, {"TELEGRAM_BOT_TOKENS": "123:ABC,,456:DEF,"}, clear=False):
            self.assertEqual(resolve_bot_tokens(), ["123:ABC", "456:DEF"])

    def test_tokens_from_skill_config_and_secret_store(self):
        with tempfile.TemporaryDirectory() as tmp:
            _write_global_config(tmp, extra='[gateway.telegram]\nbot_tokens = { source = "store", id = "gateway-telegram.bot_tokens" }\n')
            with open(os.path.join(tmp, "secrets.json"), "w") as f:
                json.dump({"gateway-telegram.bot_tokens": ["123:ABC", "456:DEF"]}, f)
                f.write("\n")

            with patch.dict(os.environ, {"TABULA_HOME": tmp}, clear=True):
                self.assertEqual(resolve_bot_tokens(), ["123:ABC", "456:DEF"])

    def test_env_override_beats_skill_config_tokens(self):
        with tempfile.TemporaryDirectory() as tmp:
            _write_global_config(tmp, extra='[gateway.telegram]\nbot_tokens = { source = "store", id = "gateway-telegram.bot_tokens" }\n')
            with open(os.path.join(tmp, "secrets.json"), "w") as f:
                json.dump({"gateway-telegram.bot_tokens": ["123:ABC"]}, f)
                f.write("\n")

            with patch.dict(
                os.environ,
                {"TABULA_HOME": tmp, "TABULA_SKILL_GATEWAY_TELEGRAM_BOT_TOKENS": "env:ONE,env:TWO"},
                clear=True,
            ):
                self.assertEqual(resolve_bot_tokens(), ["env:ONE", "env:TWO"])


class TestGatewayConfigImport(unittest.TestCase):
    def test_module_import_reads_skill_config_defaults(self):
        with tempfile.TemporaryDirectory() as tmp:
            driver_dir = os.path.join(tmp, "skills", "driver-openai")
            os.makedirs(driver_dir, exist_ok=True)
            with open(os.path.join(driver_dir, "run.py"), "w") as f:
                f.write("#!/usr/bin/env python3\n")
            with open(os.path.join(driver_dir, "SKILL.config.json"), "w") as f:
                json.dump({"id": "driver-openai", "config": {"entries": [{"key": "api_key", "type": "string", "secret": True, "required": True, "env": "TABULA_SKILL_DRIVER_OPENAI_API_KEY", "env_aliases": ["OPENAI_API_KEY"], "store_id": "driver-openai.api_key"}]}}, f)
            _write_global_config(tmp, "openai", '\n'.join([
                '[gateway.telegram]',
                'api_timeout = 42',
                '',
                '[gateway.telegram.session]',
                'idle_ttl = 111',
                'max_age = 222',
                'cleanup_interval = 7',
                '',
            ]))

            mod = _load_gateway_module_for_home(tmp)

            self.assertEqual(mod.PROVIDER_OVERRIDE, None)
            self.assertEqual(mod.ACTIVE_PROVIDER, "openai")
            self.assertEqual(mod.API_TIMEOUT, 42.0)
            self.assertEqual(mod.SESSION_IDLE_TTL, 111.0)
            self.assertEqual(mod.SESSION_MAX_AGE, 222.0)
            self.assertEqual(mod.SESSION_CLEANUP_INTERVAL, 7.0)

    def test_module_import_env_overrides_skill_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            driver_dir = os.path.join(tmp, "skills", "driver-openai")
            os.makedirs(driver_dir, exist_ok=True)
            with open(os.path.join(driver_dir, "run.py"), "w") as f:
                f.write("#!/usr/bin/env python3\n")
            with open(os.path.join(driver_dir, "SKILL.config.json"), "w") as f:
                json.dump({"id": "driver-openai", "config": {"entries": [{"key": "api_key", "type": "string", "secret": True, "required": True, "env": "TABULA_SKILL_DRIVER_OPENAI_API_KEY", "env_aliases": ["OPENAI_API_KEY"], "store_id": "driver-openai.api_key"}]}}, f)
            _write_global_config(tmp, "anthropic", '[gateway.telegram]\nprovider_override = "openai"\napi_timeout = 42\n')

            mod = _load_gateway_module_for_home(
                tmp,
                {
                    "TABULA_SKILL_GATEWAY_TELEGRAM_API_TIMEOUT": "15",
                },
            )

            self.assertEqual(mod.PROVIDER_OVERRIDE, "openai")
            self.assertEqual(mod.ACTIVE_PROVIDER, "openai")
            self.assertEqual(mod.API_TIMEOUT, 15.0)


# -- Markdown conversion --

class TestEscapeTgv2(unittest.TestCase):
    def test_escapes_special_chars(self):
        self.assertEqual(escape_tgv2("hello_world"), r"hello\_world")
        self.assertEqual(escape_tgv2("bold**text"), r"bold\*\*text")
        self.assertEqual(escape_tgv2("a[b]c"), r"a\[b\]c")

    def test_no_special_chars(self):
        self.assertEqual(escape_tgv2("hello world"), "hello world")

    def test_escapes_backtick(self):
        self.assertEqual(escape_tgv2("a`b"), r"a\`b")


class TestMdToTgv2(unittest.TestCase):
    def test_bold_conversion(self):
        result = md_to_tgv2("**bold**")
        self.assertIn("*bold*", result)

    def test_italic_conversion(self):
        result = md_to_tgv2("*italic*")
        self.assertIn("_italic_", result)

    def test_heading_conversion(self):
        result = md_to_tgv2("# Heading")
        self.assertIn("*Heading*", result)

    def test_code_block_preserved(self):
        result = md_to_tgv2("```\ncode\n```")
        self.assertIn("```\ncode\n```", result)

    def test_inline_code_preserved(self):
        result = md_to_tgv2("use `foo()` here")
        self.assertIn("`foo()`", result)


# -- Text splitting --

class TestSplitText(unittest.TestCase):
    def test_short_text(self):
        self.assertEqual(_split_text("hello", 4096), ["hello"])

    def test_split_at_newline(self):
        text = "a" * 100 + "\n" + "b" * 100
        chunks = _split_text(text, 150)
        self.assertEqual(len(chunks), 2)
        self.assertTrue(chunks[0].endswith("a" * 100))

    def test_split_at_space(self):
        text = "word " * 100
        chunks = _split_text(text, 100)
        self.assertGreater(len(chunks), 1)
        for chunk in chunks:
            self.assertLessEqual(len(chunk), 100)

    def test_hard_split(self):
        text = "a" * 200
        chunks = _split_text(text, 50)
        self.assertGreater(len(chunks), 1)
        for chunk in chunks:
            self.assertLessEqual(len(chunk), 50)


# -- BotInstance --

class TestBotInstance(unittest.TestCase):
    def setUp(self):
        self.gateway = MagicMock(spec=TelegramGateway)
        self.bot = BotInstance("123:ABC", self.gateway)

    def test_tg_api_url(self):
        self.assertEqual(self.bot.TG_API, "https://api.telegram.org/bot123:ABC")

    @patch.object(_gw, "requests")
    def test_tg_calls_post(self, mock_requests):
        mock_requests.post.return_value.json.return_value = {"ok": True, "result": {}}
        result = self.bot.tg("getMe")
        mock_requests.post.assert_called_once()
        call_args = mock_requests.post.call_args
        self.assertIn("123:ABC", call_args[0][0])
        self.assertIn("getMe", call_args[0][0])
        self.assertEqual(result, {"ok": True, "result": {}})

    @patch.object(_gw, "requests")
    def test_send_message_with_parse_mode(self, mock_requests):
        mock_requests.post.return_value.json.return_value = {"ok": True}
        self.bot.send_message(42, "hello", parse_mode="MarkdownV2")
        call_kwargs = mock_requests.post.call_args[1]
        self.assertEqual(call_kwargs["json"]["chat_id"], 42)
        self.assertEqual(call_kwargs["json"]["parse_mode"], "MarkdownV2")

    @patch.object(_gw, "requests")
    def test_send_draft(self, mock_requests):
        mock_requests.post.return_value.json.return_value = {"ok": True}
        self.bot.send_draft(42, "draft-1", "streaming text")
        call_kwargs = mock_requests.post.call_args[1]
        self.assertEqual(call_kwargs["json"]["chat_id"], 42)
        self.assertEqual(call_kwargs["json"]["draft_id"], "draft-1")
        self.assertEqual(call_kwargs["json"]["text"], "streaming text")

    @patch.object(_gw, "log")
    def test_run_survives_getme_network_error(self, mock_log):
        request_error = _gw.requests.RequestException("timeout")
        with patch.object(self.bot, "tg", side_effect=request_error), \
             patch.object(_gw.requests, "get", side_effect=KeyboardInterrupt):
            with self.assertRaises(KeyboardInterrupt):
                self.bot.run()

        self.assertTrue(any("getMe failed" in call.args[0] for call in mock_log.call_args_list))


# -- TelegramGateway --

class TestTelegramGateway(unittest.TestCase):
    def setUp(self):
        with patch.object(_gw, "_discover_slash_commands", return_value=[]):
            self.gateway = TelegramGateway(cleanup_interval=0)
        self.addCleanup(self.gateway.shutdown)
        self.bot = MagicMock(spec=BotInstance)
        self.bot.TG_API = "https://api.telegram.org/bot123:ABC"

    def test_handle_update_ignores_non_message(self):
        self.gateway.handle_update({"edited_message": {}}, bot=self.bot)
        self.bot.send_message.assert_not_called()

    def test_handle_update_ignores_empty_text(self):
        self.gateway.handle_update({
            "message": {"chat": {"id": 1}, "text": "   ", "from": {"username": "u"}}
        }, bot=self.bot)
        self.bot.send_message.assert_not_called()

    @patch.object(_gw, "is_authorized", return_value=True)
    def test_authorized_message_starts_thread(self, mock_auth):
        with patch.object(self.gateway, "_process_message") as mock_process:
            self.gateway.handle_update({
                "message": {"chat": {"id": 1}, "text": "hello", "from": {"username": "u"}}
            }, bot=self.bot)
            mock_process.assert_called_once()

    @patch.object(_gw, "is_authorized", return_value=False)
    def test_unauthorized_gets_denied(self, mock_auth):
        self.gateway.handle_update({
            "message": {"chat": {"id": 1}, "text": "hello", "from": {"username": "u"}}
        }, bot=self.bot)
        self.bot.send_message.assert_called_once()
        call_args = self.bot.send_message.call_args
        text = call_args[0][1] if call_args[0] else call_args[1].get("text", "")
        self.assertIn("Access denied", text)

    @patch.object(_gw, "is_authorized", return_value=False)
    @patch.object(_gw, "create_pairing_token", return_value="PRX-ABC-DEF")
    def test_start_generates_token(self, mock_token, mock_auth):
        self.gateway.handle_update({
            "message": {"chat": {"id": 1}, "text": "/start", "from": {"username": "u"}}
        }, bot=self.bot)
        mock_token.assert_called_once()
        self.bot.send_message.assert_called_once()

    def test_different_sessions_connect_without_holding_global_lock(self):
        gateway = TelegramGateway(cleanup_interval=0)
        self.addCleanup(gateway.shutdown)
        barrier = threading.Barrier(2)
        errors: list[BaseException] = []

        class SlowSession:
            def __init__(self, session_id: str):
                self.session_id = session_id
                self.alive = True
                self.touch_calls = 0

            def connect(self):
                try:
                    barrier.wait(timeout=1)
                except BaseException as exc:
                    errors.append(exc)
                    raise

            def touch(self, now=None):
                self.touch_calls += 1

            def expiry_reason(self, now=None):
                return None

            def is_busy(self):
                return False

            def close(self, reason="closed"):
                self.alive = False

        results = []
        with patch.object(_gw, "SessionState", SlowSession):
            threads = [
                threading.Thread(target=lambda cid=cid: results.append(gateway._get_session(cid)))
                for cid in (101, 202)
            ]
            for thread in threads:
                thread.start()
            for thread in threads:
                thread.join(timeout=2)

        self.assertFalse(any(thread.is_alive() for thread in threads))
        self.assertEqual(errors, [])
        self.assertEqual({state.session_id for state in results}, {"tg-101", "tg-202"})

    def test_same_chat_session_creation_is_single_flight(self):
        gateway = TelegramGateway(cleanup_interval=0)
        self.addCleanup(gateway.shutdown)
        connect_started = threading.Event()
        release_connect = threading.Event()
        connect_calls = 0
        connect_lock = threading.Lock()

        class SlowSession:
            def __init__(self, session_id: str):
                self.session_id = session_id
                self.alive = True

            def connect(self):
                nonlocal connect_calls
                with connect_lock:
                    connect_calls += 1
                connect_started.set()
                release_connect.wait(timeout=2)

            def touch(self, now=None):
                return None

            def expiry_reason(self, now=None):
                return None

            def is_busy(self):
                return False

            def close(self, reason="closed"):
                self.alive = False

        results = []
        with patch.object(_gw, "SessionState", SlowSession):
            first = threading.Thread(target=lambda: results.append(gateway._get_session(77)))
            second = threading.Thread(target=lambda: results.append(gateway._get_session(77)))
            first.start()
            self.assertTrue(connect_started.wait(timeout=1))
            second.start()
            release_connect.set()
            first.join(timeout=2)
            second.join(timeout=2)

        self.assertFalse(first.is_alive())
        self.assertFalse(second.is_alive())
        self.assertEqual(connect_calls, 1)
        self.assertEqual(len(results), 2)
        self.assertIs(results[0], results[1])

    def test_cleanup_evicts_idle_session_and_closes_driver(self):
        gateway = TelegramGateway(cleanup_interval=0)
        self.addCleanup(gateway.shutdown)

        class FakeSession:
            def __init__(self, session_id: str):
                self.session_id = session_id
                self.alive = True
                self.closed = 0
                self.closed_reason = None

            def expiry_reason(self, now=None):
                return "idle"

            def is_busy(self):
                return False

            def close(self, reason="closed"):
                self.closed += 1
                self.closed_reason = reason
                self.alive = False

        session = FakeSession("tg-5")
        gateway.sessions[5] = session

        gateway._cleanup_sessions()

        self.assertNotIn(5, gateway.sessions)
        self.assertEqual(session.closed, 1)
        self.assertEqual(session.closed_reason, "idle")

    def test_cleanup_skips_busy_expired_session(self):
        gateway = TelegramGateway(cleanup_interval=0)
        self.addCleanup(gateway.shutdown)

        class FakeSession:
            def __init__(self, session_id: str):
                self.session_id = session_id
                self.alive = True
                self.closed = 0

            def expiry_reason(self, now=None):
                return "idle"

            def is_busy(self):
                return True

            def close(self, reason="closed"):
                self.closed += 1
                self.alive = False

        session = FakeSession("tg-6")
        gateway.sessions[6] = session

        gateway._cleanup_sessions()

        self.assertIs(gateway.sessions[6], session)
        self.assertEqual(session.closed, 0)

    def test_get_session_replaces_expired_idle_session(self):
        gateway = TelegramGateway(cleanup_interval=0)
        self.addCleanup(gateway.shutdown)

        class ExpiredSession:
            def __init__(self, session_id: str):
                self.session_id = session_id
                self.alive = True
                self.closed = 0
                self.closed_reason = None

            def expiry_reason(self, now=None):
                return "ttl"

            def is_busy(self):
                return False

            def touch(self, now=None):
                return None

            def close(self, reason="closed"):
                self.closed += 1
                self.closed_reason = reason
                self.alive = False

        replacement = MagicMock()
        replacement.session_id = "tg-9"
        replacement.alive = True
        replacement.expiry_reason.return_value = None
        replacement.is_busy.return_value = False

        existing = ExpiredSession("tg-9")
        gateway.sessions[9] = existing

        with patch.object(_gw, "SessionState", return_value=replacement) as mock_state_cls:
            result = gateway._get_session(9)

        self.assertIs(result, replacement)
        self.assertEqual(existing.closed, 1)
        self.assertEqual(existing.closed_reason, "replaced")
        mock_state_cls.assert_called_once_with("tg-9")
        replacement.connect.assert_called_once_with()

    def test_shutdown_closes_all_sessions_and_unblocks_creators(self):
        gateway = TelegramGateway(cleanup_interval=0)

        class FakeSession:
            def __init__(self, session_id: str):
                self.session_id = session_id
                self.closed = 0
                self.closed_reason = None

            def close(self, reason="closed"):
                self.closed += 1
                self.closed_reason = reason

        first = FakeSession("tg-1")
        second = FakeSession("tg-2")
        waiter = threading.Event()
        gateway.sessions = {1: first, 2: second}
        gateway._creating = {3: waiter}

        gateway.shutdown()

        self.assertEqual(first.closed, 1)
        self.assertEqual(second.closed, 1)
        self.assertEqual(first.closed_reason, "shutdown")
        self.assertEqual(second.closed_reason, "shutdown")
        self.assertTrue(waiter.is_set())
        self.assertEqual(gateway.sessions, {})
        self.assertEqual(gateway._creating, {})
        self.assertTrue(gateway._stop_event.is_set())

    @patch.object(_gw, "BotInstance")
    @patch.object(_gw, "log")
    @patch.object(_gw, "threading")
    def test_run_gateway_continues_after_setmycommands_timeout(self, mock_threading, mock_log, mock_bot_cls):
        gateway_instance = MagicMock()
        gateway_instance._commands = {"pair": {"description": "Pair bot"}}
        gateway_instance.shutdown = MagicMock()

        bot_instance = MagicMock()
        bot_instance.tg.side_effect = _gw.requests.RequestException("timeout")
        mock_bot_cls.return_value = bot_instance

        fake_thread = MagicMock()
        mock_threading.Thread.return_value = fake_thread
        mock_threading.Event = threading.Event
        mock_threading.Lock = threading.Lock

        with patch.object(_gw, "TelegramGateway", return_value=gateway_instance), \
             patch.object(_gw.time, "sleep", side_effect=KeyboardInterrupt):
            _gw._run_gateway(["123:ABC"])

        bot_instance.tg.assert_called_once()
        fake_thread.start.assert_called_once()
        self.assertTrue(any("setMyCommands network error" in call.args[0] for call in mock_log.call_args_list))


# -- SessionState.ask_stream --

class TestSessionAskStream(unittest.TestCase):
    def setUp(self):
        self.session = SessionState.__new__(SessionState)
        self.session.session_id = "tg-1"
        self.session.conn = MagicMock()
        self.session.events = queue.Queue()
        self.session.alive = True
        self.session.driver_pid = None
        self.session._thread = None
        self.session.turn_lock = threading.Lock()
        self.session._state_lock = threading.Lock()
        self.session.created_at = 0.0
        self.session.last_used_at = 0.0
        self.session.inflight_turn_id = None
        self.session.closed_reason = None

    def _feed_events(self, *events):
        """Feed events after ask_stream drains stale ones and sends the message."""
        # ask_stream drains stale events, then sends, then waits.
        # We need to feed events after the drain but before the wait.
        # Since conn.send is mocked, we can inject events right after the call.
        original_send = self.session.conn.send

        def send_and_feed(msg):
            original_send(msg)
            for ev in events:
                self.session.events.put(ev)

        self.session.conn.send = send_and_feed

    def test_yields_deltas(self):
        self._feed_events(
            ("stream_delta", "hello "),
            ("stream_delta", "world"),
            ("done", ""),
        )
        with patch.object(_gw, "ASK_TIMEOUT", 0.1):
            result = list(self.session.ask_stream("test"))
        self.assertEqual(result, ["hello ", "world"])

    def test_handles_error(self):
        self._feed_events(
            ("stream_delta", "partial"),
            ("error", "something broke"),
        )
        with patch.object(_gw, "ASK_TIMEOUT", 0.1):
            result = list(self.session.ask_stream("test"))
        self.assertEqual(result, ["partial", "\n[error: something broke]"])

    def test_handles_disconnect(self):
        self._feed_events(
            ("stream_delta", "partial"),
            ("disconnect", ""),
        )
        with patch.object(_gw, "ASK_TIMEOUT", 0.1):
            result = list(self.session.ask_stream("test"))
        self.assertEqual(result, ["partial", "\n[lost connection to kernel]"])

    def test_handles_timeout(self):
        # Don't put anything in queue — should timeout
        self.session.events = queue.Queue()
        with patch.object(_gw, "ASK_TIMEOUT", 0.1):
            result = list(self.session.ask_stream("test"))
        self.assertEqual(result, ["[timeout waiting for response]"])

    def test_sends_message_to_kernel(self):
        captured = []
        original_send = self.session.conn.send

        def send_and_capture(msg):
            captured.append(msg)
            self.session.events.put(("done", ""))

        self.session.conn.send = send_and_capture
        with patch.object(_gw, "ASK_TIMEOUT", 0.1):
            list(self.session.ask_stream("user input"))

        self.assertEqual(len(captured), 1)
        self.assertEqual(captured[0]["type"], "message")
        self.assertEqual(captured[0]["text"], "user input")

    def test_drains_stale_events_and_serializes_turn(self):
        self.session.events.put(("stream_delta", "stale"))
        seen_lock_state = []

        def send_and_capture(msg):
            seen_lock_state.append(self.session.turn_lock.locked())
            self.session.events.put(("done", ""))

        self.session.conn.send = send_and_capture
        with patch.object(_gw, "ASK_TIMEOUT", 0.1):
            list(self.session.ask_stream("fresh"))

        self.assertEqual(seen_lock_state, [True])
        self.assertTrue(self.session.events.empty())

    def test_cancel_turn_requires_matching_inflight_turn(self):
        self.session.inflight_turn_id = "turn-123"

        self.assertFalse(self.session.cancel_turn("turn-mismatch"))
        self.session.conn.send.assert_not_called()

        self.assertTrue(self.session.cancel_turn("turn-123"))
        self.assertTrue(self.session.cancel_requested)
        self.session.conn.send.assert_called_once_with({"type": "cancel"})

    def test_touch_and_expiry_helpers(self):
        with patch.object(_gw.time, "monotonic", return_value=125.0):
            self.session.touch()
        self.assertEqual(self.session.last_used_at, 125.0)

        self.session.created_at = 10.0
        self.session.last_used_at = 90.0
        with patch.object(_gw, "SESSION_IDLE_TTL", 20), patch.object(_gw, "SESSION_MAX_AGE", 200):
            self.assertEqual(self.session.idle_seconds(115.0), 25.0)
            self.assertEqual(self.session.expiry_reason(115.0), "idle")

        with patch.object(_gw, "SESSION_IDLE_TTL", 0), patch.object(_gw, "SESSION_MAX_AGE", 50):
            self.assertEqual(self.session.expiry_reason(70.0), "ttl")

    def test_close_kills_driver_once(self):
        self.session.driver_pid = 4321

        self.session.close("idle")
        self.session.close("shutdown")

        self.session.conn.send.assert_called_once_with({
            "type": "tool_use",
            "name": "process_kill",
            "id": "kill-driver",
            "input": {"pid": 4321},
        })
        self.session.conn.close.assert_called_once()
        self.assertEqual(self.session.closed_reason, "idle")

    def test_raises_when_session_is_closed(self):
        self.session.alive = False

        with self.assertRaisesRegex(RuntimeError, "session is closed"):
            list(self.session.ask_stream("test"))


if __name__ == "__main__":
    unittest.main()

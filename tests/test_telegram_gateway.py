#!/usr/bin/env python3
"""Unit tests for the Telegram gateway skill."""

from __future__ import annotations

import json
import os
import queue
import sys
import threading
import time
import unittest
from unittest.mock import MagicMock, patch

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

# gateway-telegram has a hyphen, so we can't import it as a Python package.
# Load the module directly from its file path.
import importlib.util

def _load_gateway_module():
    gw_path = os.path.join(ROOT, "skills", "gateway-telegram", "run.py")
    spec = importlib.util.spec_from_file_location("gateway_run", gw_path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod

# Load with test environment
with patch.dict(os.environ, {"TABULA_HOME": "/tmp/tabula-test-gw"}):
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


# -- TelegramGateway --

class TestTelegramGateway(unittest.TestCase):
    def setUp(self):
        with patch.object(_gw, "_discover_slash_commands", return_value=[]):
            self.gateway = TelegramGateway()
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


if __name__ == "__main__":
    unittest.main()

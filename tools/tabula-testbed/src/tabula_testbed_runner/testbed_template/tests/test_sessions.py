#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import unittest

from tabula_testbed import TestbedClient


class SessionsPluginSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def make_client(self, name: str, session: str) -> TestbedClient:
        client = TestbedClient(self.url, name=name)
        client.connect_join(session)
        return client

    def test_sessions_is_plugin_and_lists_sessions(self):
        home = Path(self.tabula_home)
        self.assertFalse((home / "skills" / "sessions").exists(), "sessions must not be installed as a skill")
        self.assertTrue((home / "plugins" / "sessions" / "plugin.toml").is_file(), "sessions plugin manifest missing")

        with self.make_client("testbed-sessions-client", "testbed-sessions") as client:
            self.assertIn("## Cross-Session Messages", client.init.get("context", ""))
            self.assertIn("delivered payload for the local human user", client.init.get("context", ""))
            self.assertIn("not a message addressed to you as the assistant", client.init.get("context", ""))
            client.wait_tools({"session_list", "session_info", "session_history", "session_send", "session_labels", "session_label_set", "session_relay"}, session="testbed-sessions")
            listed = client.call_tool("session_list", {}, timeout=10).json()
            self.assertIn("testbed-sessions", listed.get("sessions", {}))
            info = client.call_tool("session_info", {"session": "testbed-sessions"}, timeout=10).json()
            self.assertEqual(info.get("session"), "testbed-sessions")

    def test_session_send_delivers_cross_session_message(self):
        receiver = self.make_client("testbed-session-receiver", "target-session")
        sender = self.make_client("testbed-session-sender", "source-session")
        try:
            sender.wait_tools({"session_send"}, session="source-session")
            result = sender.call_tool("session_send", {
                "session": "target-session",
                "message": "hello session",
                "from": "source-session",
            }, timeout=10).json()
            self.assertTrue(result["ok"], result)
            msg = receiver.recv(type="message", timeout=5)
            self.assertIn("<cross_session from=\"source-session\">", msg.get("text", ""))
            self.assertIn("hello session", msg.get("text", ""))
            self.assertNotIn("reply_expected", msg.get("text", ""))
            self.assertNotIn("to=", msg.get("text", ""))
        finally:
            sender.close()
            receiver.close()

    def test_session_relay_resolves_explicit_label(self):
        receiver = self.make_client("testbed-relay-receiver", "target-valera")
        sender = self.make_client("testbed-relay-sender", "source-veniamin")
        try:
            sender.wait_tools({"session_labels", "session_label_set", "session_relay"}, session="source-veniamin")
            set_result = sender.call_tool("session_label_set", {
                "session": "target-valera",
                "label": "valera",
                "display_name": "Valera",
                "aliases": ["Валера"],
            }, timeout=10).json()
            self.assertTrue(set_result["ok"], set_result)
            self.assertEqual(set_result["session"], "target-valera")

            labels = sender.call_tool("session_labels", {}, timeout=10).json()
            self.assertIn("target-valera", {item.get("session") for item in labels.get("labels", [])})

            result = sender.call_tool("session_relay", {
                "target": "Валера",
                "message": "hello from Veniamin",
                "from": "source-veniamin",
            }, timeout=10).json()
            self.assertTrue(result["ok"], result)
            self.assertEqual(result["session"], "target-valera")
            self.assertEqual(result["resolved_by"], "label")
            msg = receiver.recv(type="message", timeout=5)
            self.assertIn("<cross_session from=\"source-veniamin\">", msg.get("text", ""))
            self.assertIn("hello from Veniamin", msg.get("text", ""))
        finally:
            sender.close()
            receiver.close()


def main() -> int:
    parser = argparse.ArgumentParser(description="Run sessions testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    SessionsPluginSmoke.url = args.url
    SessionsPluginSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(SessionsPluginSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

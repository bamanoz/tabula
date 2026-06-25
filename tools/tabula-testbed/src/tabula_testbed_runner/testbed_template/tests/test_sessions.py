#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
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
            self.assertIn("## Delivered Message Wrappers", client.init.get("context", ""))
            self.assertIn("delivered payload for the local human user", client.init.get("context", ""))
            self.assertIn("not a message addressed to you as the assistant", client.init.get("context", ""))
            client.wait_tools({"session_list", "session_info", "session_history", "session_context", "session_send", "session_labels", "session_label_set", "session_relay", "hook_dispatch_audit"}, session="testbed-sessions")
            listed = client.call_tool("session_list", {}, timeout=10).json()
            self.assertIn("default/testbed-sessions", listed.get("sessions", {}))
            info = client.call_tool("session_info", {"session": "testbed-sessions"}, timeout=10).json()
            self.assertEqual(info.get("session"), "testbed-sessions")

    def test_kernel_sessions_snapshot_exposes_liveness_fields(self):
        client = TestbedClient(self.url, name="testbed-session-liveness")
        client.connect_join(
            "testbed-session-liveness",
            sends=["message.user", "tool.call", "kernel.sessions.snapshot"],
            receives=["session.init", "message.user", "tool.result", "error", "kernel.sessions.snapshot"],
        )
        with client:
            client._send({"type": "request", "topic": "kernel.sessions.snapshot", "id": "liveness-snapshot"})
            reply = client.recv(type="reply", timeout=10)
        sessions = reply.get("data") or {}
        info = sessions.get("default/testbed-session-liveness") or sessions.get("testbed-session-liveness") or {}
        self.assertIn("active_tool_calls", info)
        self.assertIn("restart_observations", info)
        self.assertIn("stuck_suspended", info)

    def test_hook_dispatch_audit_reads_recent_events(self):
        session = "testbed-hook-audit"
        ledger_dir = Path(self.tabula_home) / "data" / "sessions" / session
        ledger_dir.mkdir(parents=True, exist_ok=True)
        events = [
            {
                "type": "ledger.event",
                "kind": "hook.dispatch.audit",
                "session": session,
                "tenant_id": "default",
                "producer": "kernel:hook_dispatch",
                "payload": {
                    "hook": "before_tool_call",
                    "target": "hook-permissions",
                    "reply_action": "block",
                    "dispatch_effect": "tool_not_invoked",
                    "status": "reply",
                    "input_summary": {"tool": "exec_run", "tool_call_id": "call-1", "input_keys": ["command"]},
                },
                "ts": 1.0,
            }
        ]
        (ledger_dir / "ledger.jsonl").write_text("".join(json.dumps(entry) + "\n" for entry in events), encoding="utf-8")
        with self.make_client("testbed-hook-audit-client", session) as client:
            client.wait_tools({"hook_dispatch_audit"}, session=session)
            result = client.call_tool("hook_dispatch_audit", {"session": session, "tool": "exec_run", "tool_call_id": "call-1"}, timeout=10).json()
        self.assertEqual(len(result.get("items", [])), 1)
        self.assertEqual(result["items"][0]["target"], "hook-permissions")
        self.assertEqual(result["items"][0]["dispatch_effect"], "tool_not_invoked")

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
            msg = receiver.recv(type="message.user", timeout=5)
            data = msg.get("data") or {}
            meta = msg.get("meta") or data.get("meta") or {}
            self.assertIn("<cross_session from=\"source-session\">", data.get("text", ""))
            self.assertIn("hello session", data.get("text", ""))
            self.assertEqual(meta.get("source"), "session_relay")
            self.assertEqual(meta.get("delivery"), "cross_session")
            self.assertEqual(meta.get("from_session"), "source-session")
            self.assertNotIn("reply_expected", data.get("text", ""))
            self.assertNotIn("to=", data.get("text", ""))
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
            msg = receiver.recv(type="message.user", timeout=5)
            data = msg.get("data") or {}
            meta = msg.get("meta") or data.get("meta") or {}
            self.assertIn("<cross_session from=\"source-veniamin\">", data.get("text", ""))
            self.assertIn("hello from Veniamin", data.get("text", ""))
            self.assertEqual(meta.get("source"), "session_relay")
            self.assertEqual(meta.get("from_session"), "source-veniamin")
        finally:
            sender.close()
            receiver.close()

    def test_session_relay_resolves_current_session_label(self):
        receiver = self.make_client("testbed-self-relay-receiver", "source-self")
        sender = self.make_client("testbed-self-relay-sender", "source-self")
        try:
            sender.wait_tools({"session_labels", "session_label_set", "session_relay"}, session="source-self")
            set_result = sender.call_tool("session_label_set", {
                "session": "source-self",
                "label": "tobi-test-2",
                "display_name": "Tobi Test Current Session 2",
                "aliases": ["tobi-current-test-2"],
            }, timeout=10).json()
            self.assertTrue(set_result["ok"], set_result)

            result = sender.call_tool("session_relay", {
                "target": "tobi-test-2",
                "message": "TOBI_RELAY_LABEL_RETEST_OK",
                "from": "source-self",
            }, timeout=10).json()
            self.assertTrue(result["ok"], result)
            self.assertEqual(result["session"], "source-self")
            self.assertEqual(result["resolved_by"], "label")
            msg = receiver.recv(type="message.user", timeout=5)
            data = msg.get("data") or {}
            meta = msg.get("meta") or data.get("meta") or {}
            self.assertIn("<cross_session from=\"source-self\">", data.get("text", ""))
            self.assertIn("TOBI_RELAY_LABEL_RETEST_OK", data.get("text", ""))
            self.assertEqual(meta.get("source"), "session_relay")
            self.assertEqual(meta.get("from_session"), "source-self")
        finally:
            sender.close()
            receiver.close()

    def test_session_history_summary_applies_last_after_filtering_tools(self):
        session = "testbed-history-summary"
        history_dir = Path(self.tabula_home) / "data" / "sessions" / session
        history_dir.mkdir(parents=True, exist_ok=True)
        entries = [
            {"role": "user", "text": "first visible"},
            {"role": "assistant", "text": "second visible"},
            {"role": "assistant", "tool_use": {"id": "call-1", "name": "session_labels", "input": {}}},
            {"role": "tool", "tool_use_id": "call-1", "output": "{}"},
            {"role": "assistant", "tool_use": {"id": "call-2", "name": "session_relay", "input": {}}},
            {"role": "tool", "tool_use_id": "call-2", "output": "{}"},
        ]
        (history_dir / "history.jsonl").write_text(
            "".join(json.dumps(entry) + "\n" for entry in entries),
            encoding="utf-8",
        )
        with self.make_client("testbed-history-summary-client", session) as client:
            client.wait_tools({"session_history"}, session=session)
            result = client.call_tool("session_history", {
                "session": session,
                "last": 2,
                "summary": True,
            }, timeout=10).json()
        self.assertEqual([entry.get("text") for entry in result["entries"]], ["first visible", "second visible"])

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

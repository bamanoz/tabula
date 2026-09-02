#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import unittest

from tabula_client_sdk import ClientConnection
from tabula_testbed import TestbedClient


class SessionsPluginSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def make_client(self, name: str, session: str) -> TestbedClient:
        client = TestbedClient(self.url, name=name)
        client.connect()
        client.create_session(session)
        return client

    def test_sessions_is_plugin_and_lists_sessions(self):
        home = Path(self.tabula_home)
        self.assertFalse((home / "skills" / "sessions").exists(), "sessions must not be installed as a skill")
        self.assertTrue((home / "plugins" / "sessions" / "plugin.toml").is_file(), "sessions plugin manifest missing")

        with self.make_client("testbed-sessions-client", "testbed-sessions") as client:
            listed = client.call_tool("session_list", {}, timeout=10).json()
            self.assertIn("default/testbed-sessions", listed.get("sessions", {}))
            info = client.call_tool("session_info", {"session": "testbed-sessions"}, timeout=10).json()
            self.assertEqual(info.get("session"), "testbed-sessions")



    def test_hook_dispatch_audit_reads_recent_events(self):
        session = "testbed-hook-audit"
        with self.make_client("testbed-hook-audit-client", session) as client:
            records = ClientConnection.connect(self.url, name="testbed-hook-audit-records")
            try:
                records.append_session_record(
                    "default",
                    session,
                    command_id="record-hook-audit",
                    kind="hook.dispatch.audit",
                    payload={
                        "hook": "before_tool_call",
                        "target": "hook-permissions",
                        "reply_action": "block",
                        "dispatch_effect": "tool_not_invoked",
                        "status": "reply",
                        "input_summary": {"tool": "exec_run", "tool_call_id": "call-1", "input_keys": ["cmd"]},
                    },
                )
            finally:
                records.close()
            result = client.call_tool("hook_dispatch_audit", {"session": session, "tool": "exec_run", "tool_call_id": "call-1"}, timeout=10).json()
        self.assertEqual(len(result.get("items", [])), 1)
        self.assertEqual(result["items"][0]["target"], "hook-permissions")
        self.assertEqual(result["items"][0]["dispatch_effect"], "tool_not_invoked")







    def test_session_history_summary_reads_protocol_v4_input_events(self):
        session = "testbed-history-summary"
        with self.make_client("testbed-history-summary-client", session) as client:
            client.submit_input("input-first", {"type": "text", "text": "first visible"})
            result = client.call_tool("session_history", {
                "session": session,
                "last": 1,
                "summary": True,
            }, timeout=10).json()
        self.assertEqual([entry.get("text") for entry in result["entries"]], ["first visible"])

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

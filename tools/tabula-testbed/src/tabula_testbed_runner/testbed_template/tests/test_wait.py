#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import unittest

from tabula_testbed import TestbedClient


class WaitPluginSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def make_client(self, name: str, session: str = "testbed-wait") -> TestbedClient:
        client = TestbedClient(self.url, name=name)
        client.connect_join(session)
        return client

    def test_wait_is_plugin_and_manages_registry(self):
        home = Path(self.tabula_home)
        self.assertTrue((home / "plugins" / "wait" / "plugin.toml").is_file(), "wait plugin missing")

        with self.make_client("testbed-wait-tools") as client:
            client.wait_tools({"wait", "wait_list", "wait_cancel"}, session="testbed-wait")
            self.assertFalse(client.has_tool("timer_start"))
            started = client.call_tool("wait", {
                "after": "30s",
                "message": "wait registry smoke",
                "id": "testbed-wait-registry",
                "session": "testbed-wait",
            }).json()
            self.assertTrue(started["ok"], started)
            listed = client.call_tool("wait_list", {}).json()
            ids = {wait["id"] for wait in listed.get("waits", [])}
            self.assertIn("testbed-wait-registry", ids)
            cancelled = client.call_tool("wait_cancel", {"id": "testbed-wait-registry"}).json()
            self.assertTrue(cancelled["ok"], cancelled)

    def test_wait_fires_message(self):
        receiver = self.make_client("testbed-wait-receiver")
        sender = self.make_client("testbed-wait-sender")
        try:
            sender.wait_tools({"wait"}, session="testbed-wait")
            started = sender.call_tool("wait", {
                "after": "2s",
                "message": "wait scheduled hello",
                "id": "testbed-wait-fire",
                "session": "testbed-wait",
            }).json()
            self.assertTrue(started["ok"], started)
            msg = receiver.recv(type="message.user", timeout=10)
            data = msg.get("data") or {}
            meta = msg.get("meta") or data.get("meta") or {}
            self.assertEqual(msg.get("id"), "testbed-wait-fire")
            self.assertIn('<wait id="testbed-wait-fire"', data.get("text", ""))
            self.assertIn("wait scheduled hello", data.get("text", ""))
            self.assertEqual(meta.get("source"), "wait")
            self.assertEqual(meta.get("wait_id"), "testbed-wait-fire")
        finally:
            sender.call_tool("wait_cancel", {"id": "testbed-wait-fire"})
            sender.close()
            receiver.close()

    def test_wait_defaults_to_current_session(self):
        receiver = self.make_client("testbed-wait-current-receiver", "testbed-wait-current")
        sender = self.make_client("testbed-wait-current-sender", "testbed-wait-current")
        try:
            sender.wait_tools({"wait"}, session="testbed-wait-current")
            started = sender.call_tool("wait", {
                "after": "2s",
                "message": "wait current session hello",
                "id": "testbed-wait-current",
            }).json()
            self.assertTrue(started["ok"], started)
            self.assertEqual(started["session"], "testbed-wait-current")
            msg = receiver.recv(type="message.user", timeout=10)
            data = msg.get("data") or {}
            meta = msg.get("meta") or data.get("meta") or {}
            self.assertEqual(msg.get("id"), "testbed-wait-current")
            self.assertIn('<wait id="testbed-wait-current"', data.get("text", ""))
            self.assertIn("wait current session hello", data.get("text", ""))
            self.assertEqual(meta.get("source"), "wait")
            self.assertEqual(meta.get("wait_id"), "testbed-wait-current")
        finally:
            sender.call_tool("wait_cancel", {"id": "testbed-wait-current"})
            sender.close()
            receiver.close()

    def test_wait_sync_blocks_and_returns_without_background_delivery(self):
        receiver = self.make_client("testbed-wait-sync-receiver", "testbed-wait-sync")
        sender = self.make_client("testbed-wait-sync-sender", "testbed-wait-sync")
        try:
            sender.wait_tools({"wait", "wait_list"}, session="testbed-wait-sync")
            started = sender.call_tool("wait", {
                "after": "1s",
                "message": "wait sync hello",
                "id": "testbed-wait-sync",
                "mode": "sync",
            }, timeout=10).json()
            self.assertTrue(started["ok"], started)
            self.assertEqual(started["mode"], "sync")
            self.assertEqual(started["status"], "fired")
            listed = sender.call_tool("wait_list", {}).json()
            ids = {wait["id"] for wait in listed.get("waits", [])}
            self.assertNotIn("testbed-wait-sync", ids)
            with self.assertRaises(TimeoutError):
                receiver.recv(type="message.user", timeout=2)
        finally:
            sender.close()
            receiver.close()


def main() -> int:
    parser = argparse.ArgumentParser(description="Run wait testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    WaitPluginSmoke.url = args.url
    WaitPluginSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(WaitPluginSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

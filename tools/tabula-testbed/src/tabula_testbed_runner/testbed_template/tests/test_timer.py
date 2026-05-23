#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import unittest

from tabula_testbed import TestbedClient


class TimerSkillSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def make_client(self, name: str, session: str = "testbed-timer") -> TestbedClient:
        client = TestbedClient(self.url, name=name)
        client.connect_join(session)
        return client

    def test_timer_is_skill_and_manages_registry(self):
        home = Path(self.tabula_home)
        self.assertTrue((home / "skills" / "timer" / "SKILL.md").is_file(), "timer skill missing")

        with self.make_client("testbed-timer-tools") as client:
            client.wait_tools({"timer_start", "timer_list", "timer_cancel"}, session="testbed-timer")
            started = client.call_tool("timer_start", {
                "after": "30s",
                "message": "timer registry smoke",
                "id": "testbed-timer-registry",
                "session": "testbed-timer",
            }).json()
            self.assertTrue(started["ok"], started)
            listed = client.call_tool("timer_list", {}).json()
            ids = {timer["id"] for timer in listed.get("timers", [])}
            self.assertIn("testbed-timer-registry", ids)
            cancelled = client.call_tool("timer_cancel", {"id": "testbed-timer-registry"}).json()
            self.assertTrue(cancelled["ok"], cancelled)

    def test_timer_fires_message(self):
        receiver = self.make_client("testbed-timer-receiver")
        sender = self.make_client("testbed-timer-sender")
        try:
            sender.wait_tools({"timer_start"}, session="testbed-timer")
            started = sender.call_tool("timer_start", {
                "after": "2s",
                "message": "timer scheduled hello",
                "id": "testbed-timer-fire",
                "session": "testbed-timer",
            }).json()
            self.assertTrue(started["ok"], started)
            msg = receiver.recv(type="message.user", timeout=10)
            self.assertEqual(msg.get("id"), "testbed-timer-fire")
            self.assertIn('<timer id="testbed-timer-fire"', msg.get("text", ""))
            self.assertIn("timer scheduled hello", msg.get("text", ""))
            self.assertEqual(msg.get("meta", {}).get("source"), "timer")
            self.assertEqual(msg.get("meta", {}).get("timer_id"), "testbed-timer-fire")
        finally:
            sender.call_tool("timer_cancel", {"id": "testbed-timer-fire"})
            sender.close()
            receiver.close()

    def test_timer_defaults_to_current_session(self):
        receiver = self.make_client("testbed-timer-current-receiver", "testbed-timer-current")
        sender = self.make_client("testbed-timer-current-sender", "testbed-timer-current")
        try:
            sender.wait_tools({"timer_start"}, session="testbed-timer-current")
            started = sender.call_tool("timer_start", {
                "after": "2s",
                "message": "timer current session hello",
                "id": "testbed-timer-current",
            }).json()
            self.assertTrue(started["ok"], started)
            self.assertEqual(started["session"], "testbed-timer-current")
            msg = receiver.recv(type="message.user", timeout=10)
            self.assertEqual(msg.get("id"), "testbed-timer-current")
            self.assertIn('<timer id="testbed-timer-current"', msg.get("text", ""))
            self.assertIn("timer current session hello", msg.get("text", ""))
            self.assertEqual(msg.get("meta", {}).get("source"), "timer")
            self.assertEqual(msg.get("meta", {}).get("timer_id"), "testbed-timer-current")
        finally:
            sender.call_tool("timer_cancel", {"id": "testbed-timer-current"})
            sender.close()
            receiver.close()


def main() -> int:
    parser = argparse.ArgumentParser(description="Run timer testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    TimerSkillSmoke.url = args.url
    TimerSkillSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(TimerSkillSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import unittest

from tabula_testbed import TestbedClient


class TodoInstalled(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def test_todo_plugin_is_installed_and_persists_session_items(self):
        home = Path(self.tabula_home)
        self.assertTrue((home / "plugins" / "todo" / "plugin.toml").is_file(), "todo plugin missing")
        with TestbedClient(self.url, name="testbed-todo") as client:
            client.connect_join("testbed-todo")
            client.wait_tools({"todoread", "todowrite"}, session="testbed-todo")
            initial = client.call_tool("todoread", {}, timeout=10).json()
            self.assertEqual(initial.get("items"), [])
            written = client.call_tool(
                "todowrite",
                {
                    "items": [
                        {"content": "inspect", "status": "completed", "priority": "high"},
                        {"content": "verify", "status": "in_progress", "active_form": "verifying"},
                    ]
                },
                timeout=10,
            ).json()
            self.assertIn(written.get("session"), {"testbed-todo", "_default"})
            self.assertEqual(len(written.get("items", [])), 2)
            self.assertEqual(written["items"][0]["id"], "1")
            self.assertEqual(written["items"][0]["priority"], "high")
            self.assertEqual(written["items"][1]["priority"], "medium")
            self.assertEqual(written["items"][1]["position"], 1)
            again = client.call_tool("todoread", {}, timeout=10).json()
            self.assertEqual(again.get("items"), written.get("items"))
            rewritten = client.call_tool(
                "todowrite",
                {"items": [{"content": "verify", "status": "completed"}]},
                timeout=10,
            ).json()
            self.assertEqual(len(rewritten["items"]), 1)
            self.assertEqual(rewritten["items"][0]["id"], written["items"][1]["id"])
            cleared = client.call_tool("todowrite", {"items": []}, timeout=10).json()
            self.assertEqual(cleared["items"], [])


def main() -> int:
    parser = argparse.ArgumentParser(description="Run todo testbed checks")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    TodoInstalled.url = args.url
    TodoInstalled.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(TodoInstalled))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import unittest

from tabula_testbed import TestbedClient


class CodeBundleSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def test_code_components_are_installed_and_advertised(self):
        home = Path(self.tabula_home)
        for skill in ("ask-user", "git", "review", "todo", "workspace"):
            self.assertTrue((home / "skills" / skill / "SKILL.md").is_file(), f"{skill} skill missing")
        for plugin in ("hook-approvals", "hook-workspace-boundary"):
            self.assertTrue((home / "plugins" / plugin / "plugin.toml").is_file(), f"{plugin} plugin missing")

        expected_tools = {
            "ask_user",
            "git_status",
            "git_diff",
            "diff_preview",
            "review_plan",
            "todoread",
            "todowrite",
            "workspace_info",
        }
        with TestbedClient(self.url, name="testbed-code-tools") as client:
            client.connect_join("testbed-code-tools")
            client.wait_tools(expected_tools, session="testbed-code-tools")

    def test_todo_skill_is_installed_and_persists_session_items(self):
        home = Path(self.tabula_home)
        self.assertTrue((home / "skills" / "todo" / "SKILL.md").is_file(), "todo skill missing")
        with TestbedClient(self.url, name="testbed-code") as client:
            client.connect_join("testbed-code")
            client.wait_tools({"todoread", "todowrite"}, session="testbed-code")
            initial = client.call_tool("todoread", {}, timeout=10).json()
            self.assertEqual(initial.get("items"), [])
            written = client.call_tool("todowrite", {
                "items": [
                    {"content": "inspect", "status": "completed"},
                    {"content": "verify", "status": "in_progress", "active_form": "verifying"},
                ]
            }, timeout=10).json()
            self.assertEqual(written.get("session"), "testbed-code")
            self.assertEqual(len(written.get("items", [])), 2)
            again = client.call_tool("todoread", {}, timeout=10).json()
            self.assertEqual(again.get("items"), written.get("items"))


def main() -> int:
    parser = argparse.ArgumentParser(description="Run code bundle testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    CodeBundleSmoke.url = args.url
    CodeBundleSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(CodeBundleSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

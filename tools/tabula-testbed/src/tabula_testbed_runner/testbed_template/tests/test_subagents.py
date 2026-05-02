#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import json
import unittest

from tabula_testbed import TestbedClient


class SubagentsPluginSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def make_client(self, name: str) -> TestbedClient:
        client = TestbedClient(self.url, name=name)
        client.connect_join("testbed-subagents")
        return client

    def test_subagents_plugin_and_client_are_installed(self):
        home = Path(self.tabula_home)
        self.assertTrue((home / "plugins" / "subagents" / "plugin.toml").is_file())
        self.assertTrue((home / "clients" / "subagent" / "client.toml").is_file())
        self.assertFalse((home / "skills" / "_subagent_types").exists())
        self.assertFalse((home / "plugins" / "_subagent_types").exists())
        self.assertFalse((home / "state" / "subagents").exists())
        self.assertFalse((home / "logs" / "subagents").exists())
        self.assertTrue((home / "plugins" / "subagents" / "types" / "general.toml").is_file())

    def test_subagents_tools_are_plugin_tools(self):
        with self.make_client("testbed-subagents") as client:
            required = {"subagent_spawn", "subagent_send", "subagent_steer", "subagent_wait", "subagent_list", "subagent_kill"}
            client.wait_tools(required, session="testbed-subagents")
            self.assertFalse(client.has_tool("process_spawn"))
            self.assertFalse(client.has_tool("process_kill"))
            self.assertFalse(client.has_tool("process_list"))
            listed = client.call_tool("subagent_list", {}, timeout=10).json()
            self.assertIn("items", listed)

    def test_subagents_plugin_enforces_allowed_tools(self):
        home = Path(self.tabula_home)
        entry = home / "state" / "plugins" / "subagents" / "sa-testbed.json"
        entry.parent.mkdir(parents=True, exist_ok=True)
        entry.write_text(json.dumps({
            "version": 1,
            "id": "sa-testbed",
            "session": "subagent-sa-testbed",
            "parent_session": "testbed-subagents",
            "status": "running",
            "pid": 0,
            "allowed_tools": ["session_list"],
        }), encoding="utf-8")
        with self.make_client("testbed-subagent-child") as client:
            client.refresh_init("subagent-sa-testbed")
            allowed = client.call_tool("session_list", {}, timeout=10).json()
            self.assertIn("sessions", allowed)
            blocked = client.call_tool("subagent_list", {}, timeout=10).output
            self.assertIn("blocked by hook", blocked)

    def test_subagents_writes_plugin_owned_state_and_logs(self):
        home = Path(self.tabula_home)
        with self.make_client("testbed-subagents-layout") as client:
            client.wait_tools({"subagent_spawn", "subagent_kill"}, session="testbed-subagents")
            spawned = client.call_tool("subagent_spawn", {
                "type": "general",
                "task": "layout smoke",
                "id": "sa-layout",
            }, timeout=10).json()
            self.assertEqual(spawned.get("id"), "sa-layout")
            self.assertTrue((home / "state" / "plugins" / "subagents" / "sa-layout.json").is_file())
            self.assertTrue((home / "logs" / "plugins" / "subagents" / "sa-layout.log").is_file())
            self.assertFalse((home / "state" / "subagents").exists())
            self.assertFalse((home / "logs" / "subagents").exists())
            client.call_tool("subagent_kill", {"id": "sa-layout"}, timeout=10)


def main() -> int:
    parser = argparse.ArgumentParser(description="Run subagents testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    SubagentsPluginSmoke.url = args.url
    SubagentsPluginSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(SubagentsPluginSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

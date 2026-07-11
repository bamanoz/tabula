#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
import unittest
from pathlib import Path

from tabula_testbed import TestbedClient


class DeferredToolsSmoke(unittest.TestCase):
    tabula_home = ""
    url = ""

    def write_policy(self) -> None:
        cfg = Path(self.tabula_home) / "config" / "plugins" / "deferred-tools" / "config.toml"
        cfg.parent.mkdir(parents=True, exist_ok=True)
        cfg.write_text(
            'enabled = true\nbase_tools = ["tool_*", "session_*"]\ndeferred_tools = ["exec_run"]\n\n[search_tags]\nexec_run = ["shell", "command"]\n',
            encoding="utf-8",
        )
        permissions = Path(self.tabula_home) / "config" / "plugins" / "hook-permissions" / "config.toml"
        permissions.parent.mkdir(parents=True, exist_ok=True)
        permissions.write_text('default = "allow"\ndeny_untyped = true\n', encoding="utf-8")

    def make_client(self, name: str, session: str = "testbed-deferred-tools") -> TestbedClient:
        client = TestbedClient(self.url, name=name)
        client.connect_join(session, tenant_id="default")
        return client

    def test_deferred_tool_search_and_status_round_trip(self):
        self.write_policy()
        with self.make_client("testbed-deferred-tools-client") as client:
            client.wait_tools({"deferred_tool_search", "deferred_tool_discovery_status"}, session="testbed-deferred-tools")
            search = client.call_tool("deferred_tool_search", {"query": "shell"}, timeout=10).json()
            self.assertEqual(search.get("matches"), ["exec_run"])
            self.assertEqual(search.get("discovered"), ["exec_run"])

            status = client.call_tool("deferred_tool_discovery_status", {}, timeout=10).json()
            self.assertEqual(status.get("discovered"), ["exec_run"])

            executed = client.call_tool("exec_run", {"cmd": "printf deferred-ok"}, timeout=10).json()
            self.assertEqual(executed.get("stdout"), "deferred-ok")
            self.assertEqual(executed.get("exit_code"), 0)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Run deferred-tools testbed smoke tests")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    args = parser.parse_args()
    DeferredToolsSmoke.tabula_home = args.home
    DeferredToolsSmoke.url = args.url
    unittest.main(argv=["test_deferred_tools"], verbosity=2)

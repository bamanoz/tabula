#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import re
import unittest

from tabula_testbed import TestbedClient


class PluginFixtureExecutionSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def make_client(self, name: str, session: str = "testbed-plugin-fixture") -> TestbedClient:
        client = TestbedClient(self.url, name=name)
        client.connect_join(session)
        return client

    def assert_env_payload(self, payload: dict, _tool: str, plugin_name: str) -> None:
        self.assertTrue(payload["ok"], payload)
        self.assertGreater(int(payload["pid"]), 0, payload)
        self.assertEqual(payload["tenant_id"], "default", payload)
        self.assertEqual(payload["target_id"], plugin_name, payload)
        plugin_dir = Path(self.tabula_home) / "plugins" / plugin_name
        self.assertTrue((plugin_dir / "plugin.toml").is_file(), payload)

    def test_fixture_plugins_execute_installed_tools(self):
        expected = {
            "testbed_cold_python": "testbed-cold-python",
            "testbed_cold_bash": "testbed-cold-bash",
            "testbed_cold_node": "testbed-cold-node",
        }
        with self.make_client("testbed-plugin-fixture") as client:
            client.wait_tools(set(expected), session="testbed-plugin-fixture")
            for tool, plugin_name in expected.items():
                payload = client.call_tool(tool, {"text": "first"}, timeout=10).json()
                self.assert_env_payload(payload, tool, plugin_name)

    def test_large_fixture_payload_survives_runtime_transport(self):
        requested = (1 << 20) + 8192
        with self.make_client("testbed-plugin-fixture-large") as client:
            client.wait_tools({"tool_result_read", "testbed_cold_python_large"}, session="testbed-plugin-fixture")
            output = client.call_tool("testbed_cold_python_large", {"bytes": requested}, timeout=30).output
            self.assertTrue(output.startswith("large:"), len(output))
            self.assertNotIn("[truncated: runtime tool result exceeded", output)
            self.assertIn("artifact://", output)
            self.assertIn("Use tool_result_read", output)
            match = re.search(r"artifact://[A-Za-z0-9_.-]+", output)
            self.assertIsNotNone(match, output)
            artifact_ref = match.group(0).rstrip(".")
            artifact = client.call_tool("tool_result_read", {"session": "testbed-plugin-fixture", "ref": artifact_ref, "limit_chars": 64}, timeout=10).json()
            self.assertTrue(artifact.get("ok"), artifact)
            self.assertEqual(artifact["content"], output[:64])
            self.assertTrue(artifact["truncated"], artifact)


def main() -> int:
    parser = argparse.ArgumentParser(description="Run plugin fixture execution testbed tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    PluginFixtureExecutionSmoke.url = args.url
    PluginFixtureExecutionSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(PluginFixtureExecutionSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

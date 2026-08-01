#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import re
import unittest

from tabula_testbed import TestbedClient


class ToolResultStorePluginSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def test_installed_hook_materializes_and_reads_large_tool_result(self) -> None:
        home = Path(self.tabula_home)
        self.assertFalse((home / "skills" / "tool-result-store").exists())
        self.assertTrue((home / "plugins" / "tool-result-store" / "plugin.toml").is_file())
        session = "testbed-tool-result-store"
        requested = 20000
        with TestbedClient(self.url, name="testbed-tool-result-store") as client:
            client.connect_join(session)
            client.wait_tools({"tool_result_read", "testbed_cold_python_large"}, session=session)
            output = client.call_tool("testbed_cold_python_large", {"bytes": requested}, timeout=30).output
            match = re.search(r"artifact://[A-Za-z0-9_.-]+", output)
            self.assertIsNotNone(match, output)
            ref = match.group(0).rstrip(".")
            first = client.call_tool(
                "tool_result_read",
                {"session": session, "ref": ref, "limit_chars": 64},
                timeout=10,
            ).json()
            second = client.call_tool(
                "tool_result_read",
                {"session": session, "ref": ref, "offset": first["next_offset"], "limit_chars": 32},
                timeout=10,
            ).json()

        self.assertTrue(first["ok"], first)
        self.assertTrue(first["artifact"]["ref"].startswith("artifact://"))
        self.assertEqual(first["artifact"]["tenant_id"], "default")
        self.assertEqual(first["artifact"]["owner_plugin"], "tool-result-store")
        self.assertEqual(first["artifact"]["correlations"]["session"], session)
        self.assertEqual(first["returned_chars"], 64)
        self.assertTrue(first["truncated"])
        self.assertEqual(second["offset"], 64)
        self.assertEqual(second["returned_chars"], 32)


def main() -> int:
    parser = argparse.ArgumentParser(description="Run tool-result-store testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    ToolResultStorePluginSmoke.url = args.url
    ToolResultStorePluginSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(ToolResultStorePluginSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

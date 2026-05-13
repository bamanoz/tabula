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
        for plugin in ("git", "review", "hook-approvals"):
            self.assertTrue((home / "plugins" / plugin / "plugin.toml").is_file(), f"{plugin} plugin missing")

        expected_tools = {
            "git_status",
            "git_diff",
            "diff_preview",
            "review_plan",
        }
        with TestbedClient(self.url, name="testbed-code-tools") as client:
            client.connect_join("testbed-code-tools")
            client.wait_tools(expected_tools, session="testbed-code-tools")


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

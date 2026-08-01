#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import unittest

from tabula_testbed import TestbedClient


class BundleDependencyClosureSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def test_capability_executes_transitively_installed_dependency(self) -> None:
        with TestbedClient(self.url, name="testbed-bundle-dependency") as client:
            client.connect_join("testbed-bundle-dependency")
            client.wait_tools(
                {"dependency_capability_status", "testbed_echo"},
                session="testbed-bundle-dependency",
            )
            capability = client.call_tool("dependency_capability_status", {}, timeout=10).json()
            dependency = client.call_tool("testbed_echo", {"text": "transitive"}, timeout=10).json()

        self.assertEqual(capability, {"ok": True, "capability": "dependency-fixture"})
        self.assertTrue(dependency.get("ok"), dependency)
        self.assertEqual(dependency.get("text"), "transitive")

        lock_path = Path(self.tabula_home) / "distrib" / "testbed" / "distro.lock.json"
        lock = json.loads(lock_path.read_text(encoding="utf-8"))
        self.assertEqual(lock["bundles"]["dependency-support-fixture"]["components"], [])
        self.assertEqual(lock["bundles"]["extensions"]["components"], [])
        self.assertEqual(
            lock["bundles"]["test-fixtures"]["components"],
            ["testbed-echo"],
        )
        self.assertIn("dependency-fixture", lock["bundles"])
        self.assertFalse((Path(self.tabula_home) / "skills" / "tabula-guide").exists())
        self.assertFalse((Path(self.tabula_home) / "plugins" / "skills").exists())


def main() -> int:
    parser = argparse.ArgumentParser(description="Run transitive bundle dependency testbed smoke")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    BundleDependencyClosureSmoke.url = args.url
    BundleDependencyClosureSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(
        unittest.defaultTestLoader.loadTestsFromTestCase(BundleDependencyClosureSmoke)
    )
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

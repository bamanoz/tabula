#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
import unittest

from tabula_testbed import TestbedClient


class SkillFailureModesSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def make_client(self, name: str, session: str = "testbed-skill-failure") -> TestbedClient:
        client = TestbedClient(self.url, name=name)
        client.connect_join(session)
        return client

    def test_nonzero_and_sdk_failure_envelopes_surface_to_caller(self):
        with self.make_client("testbed-skill-failure") as client:
            client.wait_tools({"testbed_fail", "testbed_cold_python_sdk_fail"}, session="testbed-skill-failure")

            nonzero = client.call_tool("testbed_fail", {"message": "boom"}, timeout=10).json()
            self.assertEqual(nonzero, {"ok": False, "error": "boom"})

            sdk = client.call_tool("testbed_cold_python_sdk_fail", {"message": "structured boom"}, timeout=10).output
            self.assertIn("ERROR: structured boom", sdk)

    def test_timed_out_skill_call_returns_timeout(self):
        with self.make_client("testbed-skill-timeout") as client:
            client.wait_tools({"testbed_cold_python_hang"}, session="testbed-skill-failure")
            timed_out = client.call_tool("testbed_cold_python_hang", {"sleep_ms": 30000}, timeout=35).output
            self.assertIn("ERROR: invoke timed out", timed_out)


def main() -> int:
    parser = argparse.ArgumentParser(description="Run skill failure-mode testbed tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    SkillFailureModesSmoke.url = args.url
    SkillFailureModesSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(SkillFailureModesSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

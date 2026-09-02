#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
import unittest

from tabula_testbed import TestbedClient


class SkillConcurrencySmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def make_client(self, name: str, session: str = "testbed-skill-concurrency") -> TestbedClient:
        client = TestbedClient(self.url, name=name)
        client.connect()
        client.create_session(session)
        return client

    def test_parallel_calls_use_parallel_cold_processes(self):
        clients = [self.make_client(f"testbed-skill-parallel-{i}") for i in range(8)]
        try:
            calls = [
                client.call_tool_async("testbed_cold_python", {"text": str(i), "sleep_ms": 500}, timeout=15)
                for i, client in enumerate(clients)
            ]
            results = [call.wait().json() for call in calls]
            self.assertTrue(all(result["ok"] for result in results), results)
            self.assertEqual(len({result["pid"] for result in results}), 8, results)
        finally:
            for client in clients:
                client.close()


def main() -> int:
    parser = argparse.ArgumentParser(description="Run skill concurrency testbed tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    SkillConcurrencySmoke.url = args.url
    SkillConcurrencySmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(SkillConcurrencySmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

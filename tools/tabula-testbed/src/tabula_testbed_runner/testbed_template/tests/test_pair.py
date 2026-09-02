#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
import time
from pathlib import Path
import unittest

from tabula_testbed import TestbedClient


class PairInstalled(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def test_gateway_web_alias_approves_canonical_gateway_token(self):
        home = Path(self.tabula_home)
        self.assertTrue((home / "plugins" / "pair" / "plugin.toml").is_file(), "pair plugin missing")
        state_dir = home / "data" / "pair"
        state_dir.mkdir(parents=True, exist_ok=True)
        token = "PRX-TESTAA-BB0001"
        state_file = state_dir / "gateway-web.json"
        state_file.write_text(
            json.dumps({
                "authorized": [],
                "pending": [{"token": token, "user_id": 4242, "username": "test", "expires": int(time.time()) + 300}],
            }),
            encoding="utf-8",
        )

        with TestbedClient(self.url, name="testbed-pair") as client:
            client.connect()
            client.create_session("testbed-pair")

            approved = client.call_tool("pair_approve", {"gateway": "gateway-web", "token": token}, timeout=10).json()
            listed = client.call_tool("pair_list", {"gateway": "gateway-web"}, timeout=10).json()

        self.assertTrue(approved["ok"], approved)
        self.assertEqual(approved["gateway"], "gateway-web")
        self.assertEqual(approved["state_file"], str(state_file))
        self.assertEqual(approved["authorized"], [4242])
        self.assertEqual(approved["pending"], [])
        self.assertEqual(listed["gateway"], "gateway-web")
        self.assertEqual(listed["authorized"], [4242])
        self.assertEqual(json.loads(state_file.read_text(encoding="utf-8"))["authorized"], [4242])


def main() -> int:
    parser = argparse.ArgumentParser(description="Run pair plugin testbed checks")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    PairInstalled.url = args.url
    PairInstalled.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(PairInstalled))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

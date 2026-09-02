#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import unittest

from tabula_testbed import TestbedClient


class WaitPluginSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def make_client(self, name: str, session: str = "testbed-wait") -> TestbedClient:
        client = TestbedClient(self.url, name=name)
        client.connect()
        client.create_session(session)
        return client

    def test_wait_is_plugin_and_manages_registry(self):
        home = Path(self.tabula_home)
        self.assertTrue((home / "plugins" / "wait" / "plugin.toml").is_file(), "wait plugin missing")

        with self.make_client("testbed-wait-tools") as client:

            started = client.call_tool("wait", {
                "after": "30s",
                "message": "wait registry smoke",
                "id": "testbed-wait-registry",
                "session": "testbed-wait",
            }).json()
            self.assertTrue(started["ok"], started)
            listed = client.call_tool("wait_list", {}).json()
            ids = {wait["id"] for wait in listed.get("waits", [])}
            self.assertIn("testbed-wait-registry", ids)
            cancelled = client.call_tool("wait_cancel", {"id": "testbed-wait-registry"}).json()
            self.assertTrue(cancelled["ok"], cancelled)








def main() -> int:
    parser = argparse.ArgumentParser(description="Run wait testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    WaitPluginSmoke.url = args.url
    WaitPluginSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(WaitPluginSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
from __future__ import annotations

import argparse
import unittest

from tabula_testbed import TestbedClient


class MyBundleSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"

    def test_my_echo(self):
        client = TestbedClient(self.url, name="my-bundle-smoke")
        try:
            client.connect_join("testbed-my-bundle")
            client.wait_tools({"my_echo"}, session="testbed-my-bundle")
            self.assertEqual(client.call_tool("my_echo", {"text": "hello"}).json(), {"ok": True, "text": "hello"})
        finally:
            client.close()


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="")
    parser.add_argument("--home", default="")
    args = parser.parse_args()
    MyBundleSmoke.url = args.url
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(MyBundleSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

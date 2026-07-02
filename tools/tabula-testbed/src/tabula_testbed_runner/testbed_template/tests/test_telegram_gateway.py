#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import unittest

from tabula_testbed import TestbedClient


class TelegramGatewayInstalled(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def test_status_tool_noops_without_bot_tokens(self):
        home = Path(self.tabula_home)
        self.assertTrue((home / "plugins" / "telegram-gateway" / "plugin.toml").is_file(), "telegram-gateway plugin missing")
        with TestbedClient(self.url, name="testbed-telegram-gateway") as client:
            client.connect_join("testbed-telegram-gateway")
            client.wait_tools({"telegram_gateway_status"}, session="testbed-telegram-gateway")
            status = client.call_tool("telegram_gateway_status", {}, timeout=10).json()

        self.assertTrue(status["enabled"])
        self.assertFalse(status["bot_tokens_configured"])
        self.assertFalse(status["running"])
        self.assertEqual(status["health"], "disabled")
        self.assertEqual(status["reason"], "bot tokens not configured")


def main() -> int:
    parser = argparse.ArgumentParser(description="Run telegram-gateway testbed checks")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    TelegramGatewayInstalled.url = args.url
    TelegramGatewayInstalled.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(TelegramGatewayInstalled))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

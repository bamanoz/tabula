#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import unittest

from tabula_testbed import TestbedClient


class AskUserInstalled(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def test_ask_user_tool_prompts_and_returns_choice(self):
        home = Path(self.tabula_home)
        self.assertTrue((home / "plugins" / "ask-user" / "plugin.toml").is_file(), "ask-user plugin missing")
        with TestbedClient(self.url, name="testbed-ask-user-caller") as caller, TestbedClient(self.url, name="testbed-ask-user-observer") as observer:
            caller.connect_join("testbed-ask-user")
            observer.connect_join(
                "testbed-ask-user",
                sends=["message", "tool_use", "status"],
                receives=["init", "message", "tool_result", "error", "status"],
            )
            caller.wait_tools({"ask_user"}, session="testbed-ask-user")
            pending = caller.call_tool_async(
                "ask_user",
                {"question": "Continue?", "options": ["yes", "no"]},
                timeout=15,
            )
            msg = observer.wait_for(
                lambda m: m.get("type") == "status"
                and isinstance(m.get("meta"), dict)
                and isinstance(m["meta"].get("ask_request"), dict),
                timeout=10,
            )
            request = msg["meta"]["ask_request"]
            self.assertEqual(request["question"], "Continue?")
            self.assertEqual(request["options"], ["yes", "no"])
            observer._send(
                {
                    "version": 1,
                    "type": "status",
                    "text": "",
                    "meta": {"ask_response": {"id": request["id"], "choice": "yes", "index": 0}},
                }
            )
            result = pending.wait().json()
            self.assertEqual(result, {"choice": "yes", "index": 0})


def main() -> int:
    parser = argparse.ArgumentParser(description="Run ask-user testbed checks")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    AskUserInstalled.url = args.url
    AskUserInstalled.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(AskUserInstalled))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

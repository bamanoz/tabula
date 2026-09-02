#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import unittest

from tabula_testbed import TestbedClient


class QuestionInstalled(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def test_question_tool_prompts_and_returns_answers(self):
        home = Path(self.tabula_home)
        self.assertTrue((home / "plugins" / "question" / "plugin.toml").is_file(), "question plugin missing")
        with TestbedClient(self.url, name="testbed-question-caller") as caller, TestbedClient(self.url, name="testbed-question-observer") as observer:
            caller.connect(sends=["exchange.choose"], receives=["exchange.choose"])
            caller.create_session("testbed-question")
            observer.connect(sends=["exchange.choose"], receives=["exchange.choose"])
            observer.get_session("testbed-question")
            snapshot = observer.get_session("testbed-question")
            observer.subscribe("testbed-question", after_cursor=snapshot["data"]["cursor"])
            observer.send_extension({"type": "join"})
            joined = observer.recv(op="extension.event", timeout=10)
            self.assertEqual(joined.get("data", {}).get("type"), "joined")

            pending = caller.call_tool_async(
                "question",
                {"questions": [{"question": "Continue?", "options": [{"label": "yes"}, {"label": "no"}]}]},
                timeout=15,
            )
            msg = observer.recv(op="extension.event", timeout=10)
            extension = msg.get("data") if isinstance(msg.get("data"), dict) else {}
            self.assertEqual(extension.get("type"), "request")
            self.assertEqual(extension.get("topic"), "exchange.choose")
            request = {"id": extension.get("id") or msg.get("id", ""), **(extension.get("data") if isinstance(extension.get("data"), dict) else {})}
            self.assertEqual(request["questions"][0]["question"], "Continue?")
            self.assertEqual(request["questions"][0]["options"], [{"label": "yes", "description": ""}, {"label": "no", "description": ""}])
            observer.send_extension(
                {
                    "type": "reply",
                    "topic": "exchange.choose",
                    "id": request["id"],
                    "data": {"answers": [["yes"]], "dismissed": False},
                }
            )
            result = pending.wait().json()
            self.assertEqual(result["answers"], [["yes"]])
            self.assertFalse(result["dismissed"])
            self.assertIn("Continue?", result["summary"])


def main() -> int:
    parser = argparse.ArgumentParser(description="Run question testbed checks")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    QuestionInstalled.url = args.url
    QuestionInstalled.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(QuestionInstalled))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

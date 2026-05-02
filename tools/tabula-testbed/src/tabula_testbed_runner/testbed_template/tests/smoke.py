#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import time
import unittest
import urllib.request

from tabula_testbed import TestbedClient


REQUIRED_TOOLS = {
    "testbed_echo",
    "testbed_fail",
    "testbed_hook_recorder_clear",
    "testbed_hook_recorder_events",
    "testbed_hook_mutator_configure",
    "testbed_hook_mutator_reset",
    "testbed_hook_blocker_configure",
    "testbed_hook_blocker_reset",
    "testbed_dynamic_ping",
    "testbed_dynamic_enable_extra",
    "shell_exec",
}


class TestbedCase(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    observer_url = "http://127.0.0.1:8091/metrics"
    tabula_home = ""

    def make_client(self, name: str, session: str) -> TestbedClient:
        client = TestbedClient(self.url, name=name)
        init = client.connect_join(session)
        self.assertIsInstance(init.get("tools"), list)
        return client

    def wait_for_required_tools(self) -> None:
        client = TestbedClient(self.url, name="testbed-tool-wait")
        try:
            client.connect_join("testbed-tool-wait")
            client.wait_tools(REQUIRED_TOOLS, session="testbed-tool-wait")
        finally:
            client.close()

    def setUp(self) -> None:
        self.wait_for_required_tools()


class BaselineSmoke(TestbedCase):

    def test_skills_and_plugin_tools(self):
        with self.make_client("testbed-skills", "testbed-skills") as client:
            echo = client.call_tool("testbed_echo", {"text": "hello"}).json()
            self.assertTrue(echo["ok"])
            self.assertEqual(echo["text"], "hello")

            fail = client.call_tool("testbed_fail", {"message": "boom"}).json()
            self.assertFalse(fail["ok"])
            self.assertEqual(fail["error"], "boom")

            ping = client.call_tool("testbed_dynamic_ping", {"value": "pong"}).json()
            self.assertEqual(ping, {"ok": True, "pong": "pong"})

    def test_progressive_disclosure_references_are_installed(self):
        skills_dir = Path(self.tabula_home) / "skills"
        expected = [
            skills_dir / "tabula-guide" / "references" / "overview.md",
            skills_dir / "tabula-guide" / "references" / "troubleshooting.md",
            skills_dir / "skill-contract" / "references" / "skill-format.md",
            skills_dir / "skill-contract" / "references" / "examples.md",
        ]
        missing = [str(path) for path in expected if not path.is_file()]
        self.assertEqual(missing, [])

    def test_session_start_and_message_hooks(self):
        session = "testbed-message-hooks"
        receiver = self.make_client("testbed-message-receiver", session)
        sender = self.make_client("testbed-message-sender", session)
        try:
            self.assertIn("[testbed-session-start]", sender.init.get("context", ""))
            sender.call_tool("testbed_hook_mutator_configure", {
                "message_match": "mutate-me",
                "message_replace": "mutated",
            })
            sender.send_message("mutate-me")
            msg = receiver.recv(type="message", timeout=5)
            self.assertEqual(msg.get("text"), "mutated")

            sender.call_tool("testbed_hook_blocker_configure", {"block_text": "block-me"})
            sender.send_message("block-me")
            err = sender.recv(type="error", timeout=5)
            self.assertIn("blocked", err.get("text", ""))
        finally:
            sender.call_tool("testbed_hook_mutator_reset", {})
            sender.call_tool("testbed_hook_blocker_reset", {})
            sender.close()
            receiver.close()

    def test_tool_hooks_and_recorder(self):
        with self.make_client("testbed-tool-hooks", "testbed-tool-hooks") as client:
            client.reset_fixtures()
            client.call_tool("testbed_hook_mutator_configure", {
                "tool": "testbed_echo",
                "tool_add": {"mutated": True},
            })
            try:
                echo = client.call_tool("testbed_echo", {"text": "hello"}).json()
                self.assertTrue(echo["input"]["mutated"])
            finally:
                client.call_tool("testbed_hook_mutator_reset", {})

            events = client.call_tool("testbed_hook_recorder_events", {}).json()["events"]
            names = {event["event"] for event in events}
            self.assertIn("before_tool_call", names)
            self.assertIn("after_tool_call", names)

            client.call_tool("testbed_hook_blocker_configure", {"block_tool": "testbed_echo"})
            try:
                blocked = client.call_tool("testbed_echo", {"text": "blocked"})
                self.assertIn("blocked by hook", blocked.output)
            finally:
                client.call_tool("testbed_hook_blocker_reset", {})

    def test_hook_logger_writes_audit_log(self):
        log_file = Path(self.tabula_home) / "logs" / "hook-logger" / "hooks.jsonl"
        before_size = log_file.stat().st_size if log_file.exists() else 0
        with self.make_client("testbed-hook-logger", "testbed-hook-logger") as client:
            client.call_tool("testbed_echo", {"text": "audit"})

        deadline = time.time() + 5
        while time.time() < deadline:
            if log_file.exists() and log_file.stat().st_size > before_size:
                entries = [json.loads(line) for line in log_file.read_text(encoding="utf-8").splitlines() if line.strip()]
                events = {entry.get("event") for entry in entries}
                self.assertIn("session_start", events)
                self.assertIn("after_tool_call", events)
                return
            time.sleep(0.1)
        self.fail(f"hook-logger did not write audit log: {log_file}")

    def test_hook_permissions_blocks_denied_command(self):
        permissions_file = Path(self.tabula_home) / "config" / "plugins" / "hook-permissions" / "config.toml"
        previous = permissions_file.read_text(encoding="utf-8") if permissions_file.exists() else None
        permissions_file.parent.mkdir(parents=True, exist_ok=True)
        permissions_file.write_text(
            '[[rules]]\n'
            'tool = "shell_exec"\n'
            'command = "echo denied-by-hook-permissions"\n'
            'effect = "deny"\n'
            '\n'
            '[[rules]]\n'
            'tool = "*"\n'
            'effect = "allow"\n',
            encoding="utf-8",
        )
        try:
            with self.make_client("testbed-hook-permissions", "testbed-hook-permissions") as client:
                blocked = client.call_tool("shell_exec", {"command": "echo denied-by-hook-permissions"})
                self.assertIn("ERROR: blocked by hook", blocked.output)
        finally:
            if previous is None:
                permissions_file.unlink(missing_ok=True)
            else:
                permissions_file.write_text(previous, encoding="utf-8")

    def test_dynamic_tool_update(self):
        with self.make_client("testbed-dynamic", "testbed-dynamic") as client:
            self.assertFalse(client.has_tool("testbed_dynamic_extra"))
            client.call_tool("testbed_dynamic_enable_extra", {})
            extra = client.call_tool("testbed_dynamic_extra", {}).json()
            self.assertEqual(extra, {"ok": True, "extra": True})

            client.refresh_init("testbed-dynamic-refresh")
            self.assertTrue(client.has_tool("testbed_dynamic_extra"))

    def test_observer_metrics(self):
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

        def metrics() -> dict:
            with opener.open(self.observer_url, timeout=5) as response:
                return json.loads(response.read().decode("utf-8"))

        deadline = time.time() + 20
        last_error: Exception | None = None
        while time.time() < deadline:
            try:
                before = metrics()
                break
            except Exception as exc:
                last_error = exc
                time.sleep(0.5)
        else:
            raise AssertionError(f"observer metrics unavailable: {last_error}")

        base_calls = before.get("tools", {}).get("testbed_echo", {}).get("calls", 0)
        with self.make_client("testbed-observer", "testbed-observer") as client:
            client.call_tool("testbed_echo", {"text": "observer"})

        for _ in range(40):
            after = metrics()
            calls = after.get("tools", {}).get("testbed_echo", {}).get("calls", 0)
            if calls > base_calls:
                return
            time.sleep(0.25)
        self.fail("observer did not record testbed_echo after_tool_call")


def main() -> int:
    parser = argparse.ArgumentParser(description="Run testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    parser.add_argument("--suite", action="append", choices=["baseline", "all"], default=[])
    args = parser.parse_args()
    TestbedCase.url = args.url
    TestbedCase.observer_url = args.observer_url
    TestbedCase.tabula_home = args.home
    BaselineSmoke.url = args.url
    BaselineSmoke.observer_url = args.observer_url
    BaselineSmoke.tabula_home = args.home
    suites = args.suite or ["baseline"]
    if "all" in suites:
        suites = ["baseline"]
    loader = unittest.defaultTestLoader
    suite = unittest.TestSuite()
    if "baseline" in suites:
        suite.addTests(loader.loadTestsFromTestCase(BaselineSmoke))
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

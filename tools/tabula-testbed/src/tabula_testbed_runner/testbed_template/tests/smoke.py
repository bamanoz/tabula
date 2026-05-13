#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import subprocess
import time
import unittest
import urllib.request

from tabula_testbed import TestbedClient


REQUIRED_TOOLS = {
    "exec_run",
    "fs_read",
    "fs_write",
    "testbed_echo",
    "testbed_fail",
    "testbed_hook_mutator_configure",
    "testbed_hook_mutator_reset",
    "testbed_hook_blocker_configure",
    "testbed_hook_blocker_reset",
    "testbed_dynamic_ping",
    "testbed_dynamic_enable_extra",
}


class TestbedCase(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    observer_url = "http://127.0.0.1:8091/metrics"
    tabula_home = ""

    @classmethod
    def tabula_bin(cls) -> str:
        candidate = Path(cls.tabula_home) / "bin" / "tabula"
        return str(candidate) if candidate.is_file() else "tabula"

    @classmethod
    def setUpClass(cls) -> None:
        root = Path(cls.tabula_home) / "baseline-workspace-root"
        root.mkdir(parents=True, exist_ok=True)
        cls.workspace_root = root
        env = os.environ.copy()
        env["TABULA_HOME"] = cls.tabula_home
        subprocess.run(
            [cls.tabula_bin(), "tenant", "set", "default", "--workspace-root", str(root)],
            env=env,
            check=True,
            timeout=30,
            stdout=subprocess.DEVNULL,
        )
        fs_config = Path(cls.tabula_home) / "config" / "plugins" / "fs"
        fs_config.mkdir(parents=True, exist_ok=True)
        (fs_config / "config.toml").write_text('roots = ["${project_root}"]\nfollow_symlinks = false\n', encoding="utf-8")
        exec_config = Path(cls.tabula_home) / "config" / "plugins" / "exec"
        exec_config.mkdir(parents=True, exist_ok=True)
        (exec_config / "config.toml").write_text('cwd_default = "${project_root}"\ntimeout_default_seconds = 5\n', encoding="utf-8")

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

    def test_workspace_plugins_execute(self):
        with self.make_client("testbed-workspace", "testbed-workspace") as client:
            note = self.workspace_root / "baseline.txt"
            written = client.call_tool("fs_write", {"path": str(note), "content": "baseline"}, timeout=10).json()
            self.assertEqual(written["bytes_written"], len("baseline"))
            read = client.call_tool("fs_read", {"path": str(note)}, timeout=10).json()
            self.assertEqual(read["content"], "baseline")
            exec_result = client.call_tool("exec_run", {"command": "pwd"}, timeout=10).json()
            self.assertEqual(Path(exec_result["stdout"].strip()).resolve(), self.workspace_root.resolve())

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

    def test_tool_hooks(self):
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

    def test_dynamic_tool_update(self):
        with self.make_client("testbed-dynamic", "testbed-dynamic") as client:
            client.call_tool("testbed_dynamic_enable_extra", {})
            extra = client.call_tool("testbed_dynamic_extra", {}).json()
            self.assertEqual(extra, {"ok": True, "extra": True})

            client.refresh_init("testbed-dynamic-refresh")
            self.assertTrue(client.has_tool("testbed_dynamic_extra"))


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

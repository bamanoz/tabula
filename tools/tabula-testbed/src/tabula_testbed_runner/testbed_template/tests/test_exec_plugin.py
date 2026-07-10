#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import subprocess
import time
import unittest

from tabula_testbed import TestbedClient


class ExecPluginSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""
    require_fs_absent = False

    @classmethod
    def tabula_bin(cls) -> str:
        candidate = Path(cls.tabula_home) / "bin" / "tabula"
        return str(candidate) if candidate.is_file() else "tabula"

    @classmethod
    def setUpClass(cls) -> None:
        cls.workspace_root = Path(cls.tabula_home) / "exec-workspace-root"
        cls.workspace_root.mkdir(parents=True, exist_ok=True)
        env = os.environ.copy()
        env["TABULA_HOME"] = cls.tabula_home
        subprocess.run(
            [cls.tabula_bin(), "tenant", "set", "default", "--workspace-root", str(cls.workspace_root)],
            env=env,
            check=True,
            timeout=30,
            stdout=subprocess.DEVNULL,
        )
        config_dir = Path(cls.tabula_home) / "config" / "plugins" / "exec"
        config_dir.mkdir(parents=True, exist_ok=True)
        (config_dir / "config.toml").write_text(
            'cwd_default = "${project_root}"\n'
            'timeout_default_seconds = 5\n'
            'timeout_max_seconds = 120\n'
            'env_passthrough = ["PATH", "HOME"]\n'
            'env_extra = { TABULA_TENANT_ID = "${tenant_id}" }\n'
            'deny_commands = ["forbidden"]\n',
            encoding="utf-8",
        )

    def make_client(self) -> TestbedClient:
        client = TestbedClient(self.url, name="testbed-exec")
        client.connect_join("testbed-exec", tenant_id="default")
        return client

    def call_json(self, client: TestbedClient, tool: str, args: dict, *, timeout: float = 10) -> dict:
        return client.call_tool(tool, args, timeout=timeout).json()

    def test_exec_run_timeout_deny_and_no_fs_dependency(self) -> None:
        with self.make_client() as client:
            tools = {"exec_run", "exec_run_background", "exec_kill_background", "exec_list_background"}
            client.wait_tools(tools, session="testbed-exec", tenant_id="default")
            advertised = {tool.get("name") for tool in client.tools()}
            if self.require_fs_absent:
                self.assertFalse("fs_read" in advertised)

            result = self.call_json(client, "exec_run", {"cmd": "pwd; printf :$TABULA_TENANT_ID; printf err >&2; exit 3"})
            self.assertEqual(Path(result["stdout"].splitlines()[0]).resolve(), self.workspace_root.resolve())
            self.assertTrue(result["stdout"].endswith(":default"))
            self.assertEqual(result["stderr"], "err")
            self.assertEqual(result["exit_code"], 3)
            self.assertFalse(result["timed_out"])

            timed = self.call_json(client, "exec_run", {"cmd": "sleep 2", "timeout_seconds": 1}, timeout=10)
            self.assertTrue(timed["timed_out"])

            too_long = client.call_tool("exec_run", {"cmd": "printf never", "timeout_seconds": 901}, timeout=10).output
            self.assertIn("timeout_seconds must be 120s or less", too_long)
            self.assertIn("exec_run_background", too_long)

            tmp = self.call_json(client, "exec_run", {"cmd": "ls /tmp >/dev/null && printf ok"})
            self.assertEqual(tmp["stdout"], "ok")
            self.assertEqual(tmp["exit_code"], 0)

            denied = client.call_tool("exec_run", {"cmd": "echo forbidden"}, timeout=10)
            self.assertIn("command denied by pattern", denied.output)
            self.assertIn("forbidden", denied.output)

    def test_exec_run_silent_command_can_outlive_default_runtime_deadline(self) -> None:
        with self.make_client() as client:
            client.wait_tools({"exec_run"}, session="testbed-exec", tenant_id="default")
            result = self.call_json(client, "exec_run", {"cmd": "sleep 40; printf done", "timeout_seconds": 60}, timeout=90)
            self.assertEqual(result["stdout"], "done")
            self.assertEqual(result["exit_code"], 0)
            self.assertFalse(result["timed_out"])

    def test_background_spawn_list_and_kill(self) -> None:
        with self.make_client() as client:
            client.wait_tools({"exec_run_background", "exec_list_background", "exec_kill_background"}, session="testbed-exec", tenant_id="default")
            started = self.call_json(client, "exec_run_background", {"cmd": "sleep 30"})
            bg_id = started["bg_id"]
            listed = self.call_json(client, "exec_list_background", {})
            self.assertEqual([item["bg_id"] for item in listed["processes"]], [bg_id])
            self.assertTrue(listed["processes"][0]["running"])
            killed = self.call_json(client, "exec_kill_background", {"bg_id": bg_id})
            self.assertTrue(killed["killed"])
            time.sleep(0.1)
            listed = self.call_json(client, "exec_list_background", {})
            self.assertEqual(listed["processes"], [])


def main() -> int:
    parser = argparse.ArgumentParser(description="Run exec plugin testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    ExecPluginSmoke.url = args.url
    ExecPluginSmoke.tabula_home = args.home
    ExecPluginSmoke.require_fs_absent = not (Path(args.home) / "plugins" / "fs" / "plugin.toml").is_file()
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(ExecPluginSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

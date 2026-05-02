#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import shutil
import tempfile
import unittest

from tabula_testbed import TestbedClient


class ShellBundleSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    home = ""

    def setUp(self) -> None:
        self.workdir = Path(tempfile.mkdtemp(prefix="tabula-shell-testbed."))

    def tearDown(self) -> None:
        shutil.rmtree(self.workdir, ignore_errors=True)

    def make_client(self, name: str, session: str = "testbed-shell") -> TestbedClient:
        client = TestbedClient(self.url, name=name)
        client.connect_join(session)
        return client

    def test_shell_exec_installed_behavior(self):
        with self.make_client("testbed-shell-main") as client:
            client.wait_tools({"shell_exec"}, session="testbed-shell")

            default_pwd = client.call_tool("shell_exec", {"command": "pwd"}).json()
            self.assertTrue(default_pwd["ok"])
            self.assertEqual(Path(default_pwd["output"].strip()).resolve(), Path(self.home).resolve())

            workdir_pwd = client.call_tool("shell_exec", {
                "command": "pwd",
                "workdir": str(self.workdir),
            }).json()
            self.assertTrue(workdir_pwd["ok"])
            self.assertEqual(Path(workdir_pwd["output"].strip()).resolve(), self.workdir.resolve())

            output = client.call_tool("shell_exec", {
                "command": "python3 -c 'import sys; print(\"out\"); print(\"err\", file=sys.stderr)'",
                "workdir": str(self.workdir),
            }).json()
            self.assertTrue(output["ok"])
            self.assertIn("out", output["output"])
            self.assertIn("err", output["output"])

            failed = client.call_tool("shell_exec", {"command": "python3 -c 'import sys; sys.exit(7)'"}).json()
            self.assertFalse(failed["ok"])
            self.assertEqual(failed["returncode"], 7)

            timed_out = client.call_tool("shell_exec", {"command": "sleep 1", "timeout": 1}, timeout=5).json()
            self.assertFalse(timed_out["ok"])
            self.assertIn("timed out", timed_out["error"])

            background = client.call_tool("shell_exec", {"command": "sleep 1 &"}).json()
            self.assertIn("background", background.get("error", ""))

            bad_workdir = client.call_tool("shell_exec", {"command": "pwd", "workdir": str(self.workdir / "missing")}).json()
            self.assertIn("workdir is not a directory", bad_workdir.get("error", ""))


def main() -> int:
    parser = argparse.ArgumentParser(description="Run shell testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    ShellBundleSmoke.url = args.url
    ShellBundleSmoke.home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(ShellBundleSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

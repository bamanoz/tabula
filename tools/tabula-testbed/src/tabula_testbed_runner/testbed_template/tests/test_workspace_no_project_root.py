#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import subprocess
import unittest

from tabula_testbed import TestbedClient


class WorkspaceNoProjectRoot(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    @classmethod
    def tabula_bin(cls) -> str:
        candidate = Path(cls.tabula_home) / "bin" / "tabula"
        return str(candidate) if candidate.is_file() else "tabula"

    @classmethod
    def setUpClass(cls) -> None:
        env = os.environ.copy()
        env["TABULA_HOME"] = cls.tabula_home
        subprocess.run([cls.tabula_bin(), "tenant", "create", "gamma", "--exists-ok"], env=env, check=True, timeout=30, stdout=subprocess.DEVNULL)
        fs_config = Path(cls.tabula_home) / "config" / "plugins" / "fs"
        fs_config.mkdir(parents=True, exist_ok=True)
        (fs_config / "config.toml").write_text('roots = ["${project_root}"]\nfollow_symlinks = false\n', encoding="utf-8")
        exec_config = Path(cls.tabula_home) / "config" / "plugins" / "exec"
        exec_config.mkdir(parents=True, exist_ok=True)
        (exec_config / "config.toml").write_text('cwd_default = "${project_root}"\ntimeout_default_seconds = 5\n', encoding="utf-8")

    def test_tenant_without_project_root_falls_back_to_tabula_home(self) -> None:
        with TestbedClient(self.url, name="testbed-no-project-root") as client:
            client.connect_join("testbed-no-project-root", tenant_id="gamma")
            client.wait_tools({"fs_read", "fs_write", "exec_run"}, session="testbed-no-project-root", tenant_id="gamma")
            path = Path(self.tabula_home) / "fallback.txt"
            client.call_tool("fs_write", {"path": str(path), "content": "fallback"}, timeout=10).json()
            self.assertEqual(client.call_tool("fs_read", {"path": str(path)}, timeout=10).json()["content"], "fallback")
            pwd = client.call_tool("exec_run", {"command": "pwd"}, timeout=10).json()
            self.assertEqual(Path(pwd["stdout"].strip()).resolve(), Path(self.tabula_home).resolve())


def main() -> int:
    parser = argparse.ArgumentParser(description="Run workspace no-project-root fallback tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    WorkspaceNoProjectRoot.url = args.url
    WorkspaceNoProjectRoot.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(WorkspaceNoProjectRoot))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

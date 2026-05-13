#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import subprocess
import unittest

from tabula_testbed import TestbedClient


class WorkspaceFSTenantDivergence(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    @classmethod
    def tabula_bin(cls) -> str:
        candidate = Path(cls.tabula_home) / "bin" / "tabula"
        return str(candidate) if candidate.is_file() else "tabula"

    @classmethod
    def setUpClass(cls) -> None:
        cls.alpha_root = Path(cls.tabula_home) / "alpha-root"
        cls.beta_root = Path(cls.tabula_home) / "beta-root"
        cls.alpha_root.mkdir(parents=True, exist_ok=True)
        cls.beta_root.mkdir(parents=True, exist_ok=True)
        env = os.environ.copy()
        env["TABULA_HOME"] = cls.tabula_home
        for tenant, root in (("alpha", cls.alpha_root), ("beta", cls.beta_root)):
            subprocess.run([cls.tabula_bin(), "tenant", "create", tenant, "--exists-ok"], env=env, check=True, timeout=30, stdout=subprocess.DEVNULL)
            subprocess.run([cls.tabula_bin(), "tenant", "set", tenant, "--workspace-root", str(root)], env=env, check=True, timeout=30, stdout=subprocess.DEVNULL)
        config_dir = Path(cls.tabula_home) / "config" / "plugins" / "fs"
        config_dir.mkdir(parents=True, exist_ok=True)
        (config_dir / "config.toml").write_text('roots = ["${project_root}"]\nfollow_symlinks = false\n', encoding="utf-8")

    def make_client(self, tenant_id: str) -> TestbedClient:
        client = TestbedClient(self.url, name=f"testbed-fs-{tenant_id}")
        client.connect_join(f"testbed-fs-{tenant_id}", tenant_id=tenant_id)
        client.wait_tools({"fs_read", "fs_write", "fs_glob"}, session=f"testbed-fs-{tenant_id}", tenant_id=tenant_id)
        return client

    def test_each_tenant_uses_its_own_workspace_root(self) -> None:
        with self.make_client("alpha") as alpha, self.make_client("beta") as beta:
            alpha.call_tool("fs_write", {"path": "alpha-only.txt", "content": "alpha-only"}, timeout=10).json()
            alpha.call_tool("fs_write", {"path": "shared.txt", "content": "alpha"}, timeout=10).json()
            beta.call_tool("fs_write", {"path": "shared.txt", "content": "beta"}, timeout=10).json()
            self.assertEqual(alpha.call_tool("fs_read", {"path": "shared.txt"}, timeout=10).json()["content"], "alpha")
            self.assertEqual(beta.call_tool("fs_read", {"path": "shared.txt"}, timeout=10).json()["content"], "beta")

            alpha_paths = alpha.call_tool("fs_glob", {"pattern": "**/*.txt"}, timeout=10).json()["paths"]
            beta_paths = beta.call_tool("fs_glob", {"pattern": "**/*.txt"}, timeout=10).json()["paths"]
            self.assertTrue(alpha_paths)
            self.assertTrue(beta_paths)
            self.assertEqual({Path(path).resolve().parent for path in alpha_paths}, {self.alpha_root.resolve()})
            self.assertEqual({Path(path).resolve().parent for path in beta_paths}, {self.beta_root.resolve()})

            denied = beta.call_tool("fs_read", {"path": str(self.alpha_root / "shared.txt")}, timeout=10)
            self.assertIn("path is outside configured roots", denied.output)
            invisible = beta.call_tool("fs_read", {"path": "alpha-only.txt"}, timeout=10)
            self.assertIn("path not found", invisible.output)


def main() -> int:
    parser = argparse.ArgumentParser(description="Run workspace fs tenant divergence tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    WorkspaceFSTenantDivergence.url = args.url
    WorkspaceFSTenantDivergence.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(WorkspaceFSTenantDivergence))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import subprocess
import unittest

from tabula_testbed import TestbedClient


class TenantInstallerFanoutSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    @classmethod
    def tabula_bin(cls) -> str:
        candidate = Path(cls.tabula_home) / "bin" / "tabula"
        return str(candidate) if candidate.is_file() else "tabula"

    @classmethod
    def tabula_distro_bin(cls) -> str:
        candidate = Path(cls.tabula_home) / ".venv" / "bin" / "tabula-distro"
        return str(candidate) if candidate.is_file() else "tabula-distro"

    def create_tenant(self, tenant_id: str) -> None:
        env = os.environ.copy()
        env["TABULA_HOME"] = self.tabula_home
        subprocess.run(
            [self.tabula_bin(), "tenant", "create", tenant_id, "--display-name", tenant_id.title(), "--exists-ok"],
            env=env,
            check=True,
            timeout=30,
            stdout=subprocess.DEVNULL,
        )

    def update_distro(self) -> None:
        env = os.environ.copy()
        env["TABULA_HOME"] = self.tabula_home
        generated = Path(self.tabula_home) / "generated-testbed"
        self.assertTrue(generated.is_dir(), f"missing generated testbed source: {generated}")
        subprocess.run(
            [self.tabula_distro_bin(), "--home", self.tabula_home, "install", str(generated), "--update"],
            env=env,
            check=True,
            timeout=60,
            stdout=subprocess.DEVNULL,
        )

    def make_client(self, session: str, tenant_id: str) -> TestbedClient:
        client = TestbedClient(self.url, name=f"testbed-{tenant_id}")
        client.connect_join(session, tenant_id=tenant_id)
        return client

    def assert_tenant_runtime_surface(self, tenant_id: str) -> None:
        tenant_root = Path(self.tabula_home) / "tenants" / tenant_id
        plugin_manifest = tenant_root / "plugins" / "testbed-cold-python" / "plugin.toml"
        lib_root = tenant_root / "packages" / "python" / "src" / "tabula_skill_sdk"
        self.assertTrue(plugin_manifest.is_file(), f"missing tenant plugin surface: {plugin_manifest}")
        self.assertTrue(lib_root.is_dir(), f"missing tenant package surface: {lib_root}")

    def assert_tenant_invoke(self, tenant_id: str, note: str) -> dict:
        with self.make_client(f"testbed-fanout-{tenant_id}", tenant_id) as client:
            client.wait_tools({"testbed_tenant_note"}, session=f"testbed-fanout-{tenant_id}", tenant_id=tenant_id)
            payload = client.call_tool("testbed_tenant_note", {"note": note}, timeout=10).json()
            self.assertTrue(payload["ok"], payload)
            self.assertEqual(payload["tenant_id"], tenant_id, payload)
            self.assertEqual(payload["note"], note, payload)
            self.assertIn(f"/tenants/{tenant_id}/", payload["path"], payload)
            return payload

    def test_tenant_created_after_install_gets_runtime_surface_and_survives_update(self) -> None:
        tenant_id = "gamma"
        self.create_tenant(tenant_id)
        self.assert_tenant_runtime_surface(tenant_id)

        before = self.assert_tenant_invoke(tenant_id, "before-update")
        self.update_distro()
        self.assert_tenant_runtime_surface(tenant_id)
        after = self.assert_tenant_invoke(tenant_id, "after-update")

        self.assertEqual(Path(before["path"]), Path(after["path"]))
        self.assertEqual(Path(after["path"]).read_text(encoding="utf-8"), "after-update")


def main() -> int:
    parser = argparse.ArgumentParser(description="Run tenant installer fanout testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    TenantInstallerFanoutSmoke.url = args.url
    TenantInstallerFanoutSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(TenantInstallerFanoutSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import subprocess
import unittest

from tabula_testbed import TestbedClient


class TenantConfigOverlaySmoke(unittest.TestCase):
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
        subprocess.run(
            [cls.tabula_bin(), "tenant", "create", "alpha", "--display-name", "Alpha", "--exists-ok"],
            env=env,
            check=True,
            timeout=30,
            stdout=subprocess.DEVNULL,
        )
        global_config = Path(cls.tabula_home) / "config" / "global.toml"
        global_config.parent.mkdir(parents=True, exist_ok=True)
        global_config.write_text('[plugins.testbed-tenant-config]\nvalue = "from-global"\n', encoding="utf-8")

        tenant_config = Path(cls.tabula_home) / "tenants" / "alpha" / "config" / "plugins" / "testbed-tenant-config" / "config.toml"
        tenant_config.parent.mkdir(parents=True, exist_ok=True)
        tenant_config.write_text('value = "from-alpha"\n', encoding="utf-8")

    def make_client(self, name: str, session: str, tenant_id: str) -> TestbedClient:
        client = TestbedClient(self.url, name=name)
        client.connect_join(session, tenant_id=tenant_id)
        return client

    def test_default_and_alpha_see_different_plugin_config_values(self) -> None:
        with self.make_client("testbed-config-default", "testbed-config-default", "default") as default_client:
            default_client.wait_tools({"testbed_tenant_config"}, session="testbed-config-default", tenant_id="default")
            default_payload = default_client.call_tool("testbed_tenant_config", {}, timeout=10).json()
            self.assertTrue(default_payload["ok"], default_payload)
            self.assertEqual(default_payload["tenant_id"], "default", default_payload)
            self.assertEqual(default_payload["value"], "from-global", default_payload)

        with self.make_client("testbed-config-alpha", "testbed-config-alpha", "alpha") as alpha_client:
            alpha_client.wait_tools({"testbed_tenant_config"}, session="testbed-config-alpha", tenant_id="alpha")
            alpha_payload = alpha_client.call_tool("testbed_tenant_config", {}, timeout=10).json()
            self.assertTrue(alpha_payload["ok"], alpha_payload)
            self.assertEqual(alpha_payload["tenant_id"], "alpha", alpha_payload)
            self.assertEqual(alpha_payload["value"], "from-alpha", alpha_payload)


def main() -> int:
    parser = argparse.ArgumentParser(description="Run tenant config overlay testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    TenantConfigOverlaySmoke.url = args.url
    TenantConfigOverlaySmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(TenantConfigOverlaySmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

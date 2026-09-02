#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import subprocess
import unittest

from tabula_testbed import TestbedClient


class TenantRuntimeWhitelistSmoke(unittest.TestCase):
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
        for tenant_id in ("alpha", "beta"):
            subprocess.run(
                [cls.tabula_bin(), "tenant", "create", tenant_id, "--display-name", tenant_id.title(), "--exists-ok"],
                env=env,
                check=True,
                timeout=30,
                stdout=subprocess.DEVNULL,
            )

    def make_client(self, name: str, session: str, tenant_id: str) -> TestbedClient:
        client = TestbedClient(self.url, name=name)
        client.connect()
        client.create_session(session, tenant_id=tenant_id)
        return client

    def status_json(self) -> dict:
        env = os.environ.copy()
        env["TABULA_HOME"] = self.tabula_home
        raw = subprocess.check_output([self.tabula_bin(), "status", "--json"], env=env, text=True, timeout=10)
        return json.loads(raw)

    def test_runtime_allows_alpha_and_rejects_beta_before_skill_execution(self) -> None:
        with self.make_client("testbed-whitelist-alpha", "testbed-whitelist-alpha", "alpha") as alpha:
            result = alpha.call_tool("testbed_tenant_note", {"note": "alpha-ok"}, timeout=10).json()
            self.assertTrue(result["ok"], result)
            self.assertEqual(result["tenant_id"], "alpha", result)

        beta_state_path = Path(self.tabula_home) / "tenants" / "beta" / "state" / "skills" / "testbed-cold-python" / "tenant-note.txt"
        with self.make_client("testbed-whitelist-beta", "testbed-whitelist-beta", "beta") as beta:
            denied = beta.call_tool("testbed_tenant_note", {"note": "must-not-write"}, timeout=10)

        self.assertIn("ERROR:", denied.output)
        self.assertIn("runtime \"local\" does not serve tenant \"beta\"", denied.output)
        self.assertFalse(beta_state_path.exists(), f"forbidden tenant wrote state: {beta_state_path}")

        status = self.status_json()
        runtime = next((item for item in status.get("runtimes", []) if item.get("id") == "local"), None)
        self.assertIsNotNone(runtime, status)
        self.assertEqual(runtime.get("tenants_served"), ["alpha"], status)


def main() -> int:
    parser = argparse.ArgumentParser(description="Run tenant runtime whitelist testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    TenantRuntimeWhitelistSmoke.url = args.url
    TenantRuntimeWhitelistSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(TenantRuntimeWhitelistSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import subprocess
import unittest

from tabula_testbed import TestbedClient


class TenantIsolationSmoke(unittest.TestCase):
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
        for tenant_id, display_name in (("alpha", "Alpha"), ("beta", "Beta")):
            subprocess.run(
                [cls.tabula_bin(), "tenant", "create", tenant_id, "--display-name", display_name, "--exists-ok"],
                env=env,
                check=True,
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
        raw = subprocess.check_output([self.tabula_bin(), "status", "--json"], env=env, text=True)
        return json.loads(raw)

    def test_tenant_scoped_skill_state_and_status(self) -> None:
        with self.make_client("testbed-alpha", "testbed-tenant-alpha", "alpha") as alpha:
            alpha_payload = alpha.call_tool("testbed_tenant_note", {"note": "alpha-only"}, timeout=10).json()

            self.assertTrue(alpha_payload["ok"], alpha_payload)
            self.assertEqual(alpha_payload["tenant_id"], "alpha", alpha_payload)
            self.assertEqual(alpha_payload["note"], "alpha-only", alpha_payload)
            self.assertIn("/tenants/alpha/", alpha_payload["path"], alpha_payload)

            with self.make_client("testbed-beta", "testbed-tenant-beta", "beta") as beta:
                beta_empty = beta.call_tool("testbed_tenant_note", {}, timeout=10).json()
                self.assertTrue(beta_empty["ok"], beta_empty)
                self.assertEqual(beta_empty["tenant_id"], "beta", beta_empty)
                self.assertEqual(beta_empty["note"], "", beta_empty)
                self.assertIn("/tenants/beta/", beta_empty["path"], beta_empty)

                beta_payload = beta.call_tool("testbed_tenant_note", {"note": "beta-only"}, timeout=10).json()
                self.assertEqual(beta_payload["note"], "beta-only", beta_payload)
                self.assertNotEqual(alpha_payload["path"], beta_payload["path"])

                alpha_again = alpha.call_tool("testbed_tenant_note", {}, timeout=10).json()
                self.assertEqual(alpha_again["note"], "alpha-only", alpha_again)

                status = self.status_json()
                tenants = {tenant["id"]: tenant for tenant in status.get("tenants", [])}
                self.assertIn("alpha", tenants, status)
                self.assertIn("beta", tenants, status)
                self.assertGreaterEqual(int(tenants["alpha"].get("active_sessions") or 0), 1, status)
                self.assertGreaterEqual(int(tenants["beta"].get("active_sessions") or 0), 1, status)

                runtime = next((item for item in status.get("runtimes", []) if item.get("id") == "local"), None)
                self.assertIsNotNone(runtime, status)
                self.assertTrue(runtime.get("attached"), status)
                self.assertIn("alpha", runtime.get("tenants_served") or [], status)
                self.assertIn("beta", runtime.get("tenants_served") or [], status)
                capabilities_by_tenant = runtime.get("capabilities_by_tenant", {})
                self.assertEqual(capabilities_by_tenant.get("alpha"), runtime.get("capabilities"), status)
                self.assertEqual(capabilities_by_tenant.get("beta"), runtime.get("capabilities"), status)

        alpha_path = Path(self.tabula_home) / "tenants" / "alpha" / "state" / "skills" / "testbed-cold-python" / "tenant-note.txt"
        beta_path = Path(self.tabula_home) / "tenants" / "beta" / "state" / "skills" / "testbed-cold-python" / "tenant-note.txt"
        self.assertEqual(alpha_path.read_text(encoding="utf-8"), "alpha-only")
        self.assertEqual(beta_path.read_text(encoding="utf-8"), "beta-only")


def main() -> int:
    parser = argparse.ArgumentParser(description="Run tenant isolation testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    TenantIsolationSmoke.url = args.url
    TenantIsolationSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(TenantIsolationSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

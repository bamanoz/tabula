#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path
import unittest


class HostServiceSmoke(unittest.TestCase):
    tabula_home = ""

    def run_service(self, *args: str, check: bool = True) -> subprocess.CompletedProcess[str]:
        home = Path(self.tabula_home)
        env = os.environ.copy()
        env["TABULA_HOME"] = str(home)
        command = [sys.executable, "-m", "tabula_distro.cli", "host-service", *args]
        result = subprocess.run(command, env=env, text=True, capture_output=True, check=False, timeout=20)
        if check and result.returncode != 0:
            self.fail(f"{' '.join(command)} failed: {result.stderr or result.stdout}")
        return result

    def test_installed_host_service_lifecycle_and_rollback(self) -> None:
        home = Path(self.tabula_home)
        source = home / "distrib" / "testbed" / "host-services" / "testbed-host-service"
        manifest_path = source / "service.toml"
        original_manifest = manifest_path.read_text(encoding="utf-8")
        state = home / "host-services" / "testbed-host-service" / "state" / "version"
        try:
            self.run_service("reconcile", "--adapter", "process")
            status = json.loads(self.run_service("status", "testbed-host-service", "--json").stdout)
            self.assertTrue(status["running"], status)
            self.assertEqual(state.read_text(encoding="utf-8").strip(), "v1")

            kernel_status = json.loads(
                subprocess.run(
                    [str(home / "bin" / "tabula"), "status", "--json"],
                    env={**os.environ, "TABULA_HOME": str(home)},
                    text=True,
                    capture_output=True,
                    check=True,
                    timeout=10,
                ).stdout
            )
            self.assertNotEqual(status["pid"], kernel_status["kernel"]["pid"])
            service_ppid = int(
                subprocess.run(
                    ["ps", "-p", str(status["pid"]), "-o", "ppid="],
                    text=True,
                    capture_output=True,
                    check=True,
                    timeout=5,
                ).stdout.strip()
            )
            self.assertNotEqual(service_ppid, kernel_status["kernel"]["pid"])

            manifest_path.write_text(original_manifest.replace('args = ["v1"]', 'args = ["v2"]'), encoding="utf-8")
            self.run_service("reconcile", "--adapter", "process")
            self.assertEqual(state.read_text(encoding="utf-8").strip(), "v2")

            manifest_path.write_text(original_manifest.replace('args = ["v1"]', 'args = ["fail"]'), encoding="utf-8")
            failed = self.run_service("reconcile", "--adapter", "process", check=False)
            self.assertNotEqual(failed.returncode, 0, failed.stdout)
            self.assertIn("did not become ready", failed.stderr)
            status = json.loads(self.run_service("status", "testbed-host-service", "--json").stdout)
            self.assertTrue(status["running"], status)
            self.assertEqual(state.read_text(encoding="utf-8").strip(), "v2")

            self.run_service("remove", "testbed-host-service")
            self.assertTrue(state.is_file())
            manifest_path.write_text(original_manifest, encoding="utf-8")
            self.run_service("reconcile", "--adapter", "process")
            self.assertEqual(state.read_text(encoding="utf-8").strip(), "v1")
        finally:
            manifest_path.write_text(original_manifest, encoding="utf-8")
            self.run_service("remove", "testbed-host-service", "--purge", check=False)
            (home / "run" / "testbed-host-service.ready").unlink(missing_ok=True)


def main() -> int:
    parser = argparse.ArgumentParser(description="Run installed host-service lifecycle smoke")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    HostServiceSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(
        unittest.defaultTestLoader.loadTestsFromTestCase(HostServiceSmoke)
    )
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

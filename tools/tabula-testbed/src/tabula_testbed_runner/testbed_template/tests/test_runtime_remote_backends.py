#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import subprocess
import unittest


class RuntimeRemoteBackendSuites(unittest.TestCase):
    tabula_root = ""

    def run_go(self, *args: str) -> None:
        env = os.environ.copy()
        result = subprocess.run(["go", "test", *args], cwd=self.tabula_root, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=180)
        if result.returncode != 0:
            self.fail(result.stdout)

    def test_wss_loopback(self) -> None:
        self.run_go("./internal/runtime/transport/wss", "-run", "TestWSSTransportRuntimeConnRoundTrip")

    def test_mtls_loopback(self) -> None:
        self.run_go("./internal/runtime/transport/wss", "-run", "TestWSSMTLS")

    def test_ssh_loopback(self) -> None:
        self.run_go("./internal/runtime/backend/ssh", "-run", "TestLocalhostSSHLoopbackAvailable")

    def test_token_revoke(self) -> None:
        self.run_go("./cmd/tabula", "-run", "TestWatchRuntimeTokenRevocationsDetachesRuntime")

    def test_multi_backend(self) -> None:
        self.run_go("./internal/kernel", "-run", "TestServeAuthenticatedRuntimeUnixAndWSSCoexist")
        self.run_go("./cmd/tabula", "-run", "TestSSHRuntimeSupervisorsAttachMultipleRuntimes")


def main() -> int:
    parser = argparse.ArgumentParser(description="Run remote backend loopback testbed suites")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    parser.add_argument("--tabula-root", default=os.environ.get("TABULA_ROOT", os.getcwd()))
    args = parser.parse_args()
    RuntimeRemoteBackendSuites.tabula_root = args.tabula_root
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(RuntimeRemoteBackendSuites))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

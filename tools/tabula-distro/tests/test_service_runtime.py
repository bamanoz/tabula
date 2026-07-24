from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from tabula_distro import service_runtime


class ServiceRuntimeTests(unittest.TestCase):
    def test_kernel_health_falls_back_to_websocket_connect(self):
        class FakeWS:
            sent: list[str] = []

            def send(self, payload: str) -> None:
                self.sent.append(payload)

            def recv(self) -> str:
                return '{"type":"hello_ack"}'

            def close(self) -> None:
                pass

        with mock.patch.object(service_runtime, "_urlopen", side_effect=OSError("http unavailable")):
            with mock.patch.dict("sys.modules", {"websocket": mock.Mock(create_connection=mock.Mock(return_value=FakeWS()))}):
                self.assertTrue(service_runtime.kernel_healthy("ws://127.0.0.1:65530/ws", timeout_seconds=0.1))

    def test_runtime_ready_requires_matching_tenant_capability(self):
        class FakeResponse:
            status = 200

            def __enter__(self):
                return self

            def __exit__(self, *_exc):
                return None

            def read(self) -> bytes:
                return b'{"runtimes":[{"attached":true,"tenants_served":["first-tenant","claw-tenant"],"capabilities_by_tenant":{"claw-tenant":["fs_read"]}}]}'

        with mock.patch.object(service_runtime, "_urlopen", return_value=FakeResponse()):
            self.assertTrue(service_runtime._runtime_ready("ws://127.0.0.1:65530/ws", "claw-tenant", timeout_seconds=0.1))
            self.assertFalse(service_runtime._runtime_ready("ws://127.0.0.1:65530/ws", "first-tenant", timeout_seconds=0.1))

    def test_runtime_ready_rejects_attached_runtime_before_targets_prime(self):
        class FakeResponse:
            status = 200

            def __enter__(self):
                return self

            def __exit__(self, *_exc):
                return None

            def read(self) -> bytes:
                return b'{"runtimes":[{"attached":true,"tenants_served":["claw-tenant"],"targets":[]}]}'

        with mock.patch.object(service_runtime, "_urlopen", return_value=FakeResponse()):
            self.assertFalse(service_runtime._runtime_ready("ws://127.0.0.1:65530/ws", "claw-tenant", timeout_seconds=0.1))

    def test_runtime_ready_accepts_tenant_capability_summary(self):
        class FakeResponse:
            status = 200

            def __enter__(self):
                return self

            def __exit__(self, *_exc):
                return None

            def read(self) -> bytes:
                return b'{"runtimes":[{"attached":true,"tenants_served":["claw-tenant"],"capabilities_by_tenant":{"claw-tenant":["fs_read"]}}]}'

        with mock.patch.object(service_runtime, "_urlopen", return_value=FakeResponse()):
            self.assertTrue(service_runtime._runtime_ready("ws://127.0.0.1:65530/ws", "claw-tenant", timeout_seconds=0.1))

    def test_runtime_ready_rejects_failed_worker_target_for_tenant(self):
        data = {
            "runtimes": [
                {
                    "attached": True,
                    "tenants_served": ["claw-tenant"],
                    "capabilities_by_tenant": {"claw-tenant": ["gateway_web_status"]},
                    "targets": [
                        {
                            "id": "any-plugin",
                            "source": "worker",
                            "state": "ready",
                            "lifecycle_state": "crashed",
                            "tenants": ["claw-tenant"],
                        }
                    ],
                }
            ]
        }

        self.assertFalse(service_runtime._runtime_ready_from_snapshot(data, "claw-tenant"))

    def test_runtime_ready_ignores_failed_worker_target_for_other_tenant(self):
        data = {
            "runtimes": [
                {
                    "attached": True,
                    "tenants_served": ["claw-tenant", "other-tenant"],
                    "capabilities_by_tenant": {"claw-tenant": ["fs_read"]},
                    "targets": [
                        {
                            "id": "other-plugin",
                            "source": "worker",
                            "state": "failed",
                            "tenants": ["other-tenant"],
                        }
                    ],
                }
            ]
        }

        self.assertTrue(service_runtime._runtime_ready_from_snapshot(data, "claw-tenant"))

    def test_runtime_ready_falls_back_to_local_status(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            tabula = home / "bin" / "tabula"
            tabula.parent.mkdir(parents=True)
            tabula.write_text("#!/bin/sh\nexit 1\n", encoding="utf-8")
            status = json.dumps({"runtimes": [{"attached": True, "tenants_served": ["claw-tenant"], "capabilities_by_tenant": {"claw-tenant": ["fs_read"]}}]})
            result = mock.Mock(returncode=0, stdout=status)
            with mock.patch.object(service_runtime, "_urlopen", side_effect=OSError("http unavailable")), mock.patch.object(service_runtime.subprocess, "run", return_value=result) as run:
                self.assertTrue(service_runtime.wait_for_runtime_ready("ws://127.0.0.1:65530/ws", "claw-tenant", home=home, tabula_bin=str(tabula), timeout_seconds=0.1))
            run.assert_called()

    def test_local_internal_http_bypasses_proxy_handlers(self):
        opener = mock.Mock()
        with mock.patch.object(service_runtime, "build_opener", return_value=opener) as build:
            service_runtime._urlopen("http://localhost:8089/internal/snapshot/runtimes", timeout=0.1)

        build.assert_called_once()
        self.assertIsInstance(build.call_args.args[0], service_runtime.ProxyHandler)
        opener.open.assert_called_once_with("http://localhost:8089/internal/snapshot/runtimes", timeout=0.1)

    def test_internal_urls_prefer_kernel_status_endpoint(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            (home / "run").mkdir()
            (home / "run" / "kernel-status.json").write_text('{"ws_endpoint":"ws://127.0.0.1:7777/ws"}', encoding="utf-8")

            self.assertEqual(
                service_runtime._internal_url("ws://127.0.0.1:65530/ws", "/internal/reload/runtime", home=home),
                "http://127.0.0.1:7777/internal/reload/runtime",
            )

    def test_internal_urls_fall_back_when_kernel_status_missing(self):
        with tempfile.TemporaryDirectory() as tmp:
            self.assertEqual(
                service_runtime._internal_url("ws://127.0.0.1:65530/ws", "/internal/reload/runtime", home=Path(tmp)),
                "http://127.0.0.1:65530/internal/reload/runtime",
            )



if __name__ == "__main__":
    unittest.main()

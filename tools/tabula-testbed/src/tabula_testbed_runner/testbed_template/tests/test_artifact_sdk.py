#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
import time
import unittest

from tabula_testbed import TestbedClient


class ArtifactSDKTestbed(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def test_installed_sdk_persists_across_warm_worker_restart(self) -> None:
        session = "testbed-artifact-sdk"
        artifact_id = "installed-artifact-sdk-restart"
        content = "durable installed artifact " + ("x" * 256)
        tools = {"artifact_sdk_fixture_write_and_exit", "artifact_sdk_fixture_read"}
        with TestbedClient(self.url, name="testbed-artifact-sdk") as client:
            client.connect_join(session)
            client.wait_tools(tools, session=session)
            try:
                client.call_tool(
                    "artifact_sdk_fixture_write_and_exit",
                    {"id": artifact_id, "content": content},
                    timeout=10,
                )
            except Exception:
                pass

            payload = None
            deadline = time.monotonic() + 20
            while time.monotonic() < deadline:
                try:
                    candidate = client.call_tool(
                        "artifact_sdk_fixture_read",
                        {"id": artifact_id, "limit_chars": 64},
                        timeout=5,
                    ).json()
                    if candidate.get("ok"):
                        payload = candidate
                        break
                except Exception:
                    pass
                time.sleep(0.5)

            self.assertIsNotNone(payload, "artifact fixture worker did not restart with durable state")
            assert payload is not None
            artifact = payload["artifact"]
            self.assertEqual(artifact["tenant_id"], "default")
            self.assertEqual(artifact["owner_plugin"], "artifact-sdk-fixture")
            self.assertEqual(artifact["correlations"]["session"], session)
            self.assertEqual(artifact["correlations"]["task_id"], artifact_id)
            self.assertEqual(payload["read"]["content"], content[:64])
            self.assertTrue(payload["read"]["truncated"])
            self.assertEqual(payload["query_refs"], [artifact["ref"]])

            deleted = client.call_tool(
                "artifact_sdk_fixture_read",
                {"id": artifact_id, "limit_chars": 16, "delete": True},
                timeout=10,
            ).json()
            self.assertEqual(deleted["deleted"]["ref"], artifact["ref"])


def main() -> int:
    parser = argparse.ArgumentParser(description="Run installed artifact SDK restart test")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    ArtifactSDKTestbed.url = args.url
    ArtifactSDKTestbed.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(ArtifactSDKTestbed))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

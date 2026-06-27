#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import unittest

from tabula_testbed import TestbedClient


class ToolResultStorePluginSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def make_client(self, name: str, session: str) -> TestbedClient:
        client = TestbedClient(self.url, name=name)
        client.connect_join(session)
        return client

    def test_tool_result_store_plugin_is_installed_and_reads_bounded_chunks(self):
        home = Path(self.tabula_home)
        self.assertFalse((home / "skills" / "tool-result-store").exists(), "tool-result-store must not be installed as a skill")
        self.assertTrue((home / "plugins" / "tool-result-store" / "plugin.toml").is_file(), "tool-result-store plugin manifest missing")
        session = "testbed-tool-result-store"
        ref = "artifact://exec_run-call-1-demo"
        artifact_dir = home / "data" / "sessions" / session / "artifacts"
        artifact_dir.mkdir(parents=True, exist_ok=True)
        content = "full output\n" + ("x" * 20000)
        (artifact_dir / "exec_run-call-1-demo.txt").write_text(content, encoding="utf-8")
        (artifact_dir / "index.json").write_text(json.dumps({
            "version": 1,
            "artifacts": {
                "exec_run-call-1-demo": {
                    "id": "exec_run-call-1-demo",
                    "ref": ref,
                    "session": session,
                    "tenant_id": "default",
                    "tool_id": "call-1",
                    "tool_name": "exec_run",
                    "filename": "exec_run-call-1-demo.txt",
                    "mime_type": "text/plain; charset=utf-8",
                    "chars": len(content),
                    "bytes": len(content.encode("utf-8")),
                    "sha256": "testbed",
                    "preview_chars": 12,
                    "created_at": 123.0,
                },
            },
        }), encoding="utf-8")
        with self.make_client("testbed-tool-result-store-client", session) as client:
            client.wait_tools({"tool_result_read"}, session=session)
            result = client.call_tool("tool_result_read", {"session": session, "ref": ref, "limit_chars": 64}, timeout=10).json()
            next_result = client.call_tool("tool_result_read", {"session": session, "ref": ref, "offset": result["next_offset"], "limit_chars": 32}, timeout=10).json()
        self.assertTrue(result["ok"], result)
        self.assertEqual(result["content"], content[:64])
        self.assertEqual(result["returned_chars"], 64)
        self.assertTrue(result["truncated"])
        self.assertEqual(result["artifact"]["ref"], ref)
        self.assertEqual(next_result["content"], content[64:96])


def main() -> int:
    parser = argparse.ArgumentParser(description="Run tool-result-store testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    ToolResultStorePluginSmoke.url = args.url
    ToolResultStorePluginSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(ToolResultStorePluginSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

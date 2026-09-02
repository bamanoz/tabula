#!/usr/bin/env python3
from __future__ import annotations

import argparse
import importlib.util
import json
import os
from pathlib import Path
import sys
import time
import unittest

from tabula_client_sdk import ClientConnection
from tabula_testbed import TestbedClient


class ToolLifecycleInstalled(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def test_tool_lifecycle_records_use_protocol_v4_storage(self):
        session = f"testbed-tool-lifecycle-{time.time_ns()}"
        with TestbedClient(self.url, name="testbed-tool-lifecycle-session") as client:
            client.connect()
            client.create_session(session)

        records = ClientConnection.connect(self.url, name="testbed-tool-lifecycle-records")
        try:
            appended = records.append_session_record(
                "default",
                session,
                command_id=f"record-{time.time_ns()}",
                kind="tool.lifecycle",
                payload={
                    "state": "terminal",
                    "status": "interrupted",
                    "tool_call_id": "call-lifecycle",
                    "tool": "exec_run",
                    "reason": "kernel_restarted",
                    "previous_run_id": "kernel-old",
                },
            )
            page = records.list_session_records(
                "default",
                session,
                request_id=f"list-{time.time_ns()}",
                kind="tool.lifecycle",
                limit=10,
            )
        finally:
            records.close()

        self.assertEqual(appended.kind, "tool.lifecycle")
        self.assertEqual(len(page.records), 1)
        self.assertEqual(page.records[0].payload["tool_call_id"], "call-lifecycle")
        self.assertEqual(page.records[0].payload["reason"], "kernel_restarted")

    def test_interrupted_tool_result_normalizes_from_protocol_v4_event(self):
        home = Path(self.tabula_home)
        daemon_path = home / "plugins" / "gateway-web" / "daemon.py"
        self.assertTrue(daemon_path.is_file(), "gateway-web daemon missing from installed layout")

        daemon = self.load_daemon(daemon_path)
        output = json.dumps({
            "error": "tool_interrupted",
            "kind": "kernel_restarted",
            "retryable": False,
        })
        result = daemon.normalize_kernel_event(daemon.CommittedEvent(
            event_id="evt-tool-interrupted",
            cursor="cur_1",
            session_version=1,
            type="tool.result",
            occurred_at="2026-08-08T00:00:00Z",
            data={
                "turn_id": "turn-lifecycle",
                "attempt_id": "attempt-lifecycle",
                "output_type": "tool.result",
                "sequence": 1,
                "payload": {
                    "id": "call-lifecycle",
                    "name": "exec_run",
                    "output": output,
                    "error": True,
                },
            },
        ))

        self.assertEqual(result["type"], "tool.result")
        self.assertEqual(result["id"], "call-lifecycle")
        self.assertEqual(result["name"], "exec_run")
        self.assertTrue(result["error"])
        payload = json.loads(result["output"])
        self.assertEqual(payload["error"], "tool_interrupted")
        self.assertEqual(payload["kind"], "kernel_restarted")
        self.assertFalse(payload["retryable"])

    def load_daemon(self, path: Path):
        module_name = "testbed_gateway_web_daemon_tool_lifecycle"
        sys.modules.pop(module_name, None)
        spec = importlib.util.spec_from_file_location(module_name, path)
        self.assertIsNotNone(spec)
        self.assertIsNotNone(spec.loader)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module


def main() -> int:
    parser = argparse.ArgumentParser(description="Run tool lifecycle testbed tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    ToolLifecycleInstalled.url = args.url
    ToolLifecycleInstalled.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(ToolLifecycleInstalled))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

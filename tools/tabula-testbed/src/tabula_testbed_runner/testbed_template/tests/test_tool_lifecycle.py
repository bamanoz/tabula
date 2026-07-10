#!/usr/bin/env python3
from __future__ import annotations

import importlib.util
import json
import argparse
import os
from pathlib import Path
import sys
import unittest


class ToolLifecycleInstalled(unittest.TestCase):
    tabula_home = ""

    def test_interrupted_tool_lifecycle_replays_as_terminal_tool_result(self):
        home = Path(self.tabula_home)
        daemon_path = home / "plugins" / "gateway-web" / "daemon.py"
        self.assertTrue(daemon_path.is_file(), "gateway-web daemon missing from installed layout")

        session = "testbed-tool-lifecycle"
        session_dir = home / "data" / "sessions" / session
        session_dir.mkdir(parents=True, exist_ok=True)
        (session_dir / "history.jsonl").write_text(
            json.dumps({"role": "user", "text": "start", "ts": 1}) + "\n",
            encoding="utf-8",
        )
        (session_dir / "ledger.jsonl").write_text(
            json.dumps({
                "type": "ledger.event",
                "kind": "tool.lifecycle",
                "session": session,
                "tenant_id": "default",
                "producer": "kernel:tool_lifecycle",
                "payload": {
                    "state": "terminal",
                    "status": "interrupted",
                    "tool_call_id": "call-lifecycle",
                    "tool": "exec_run",
                    "reason": "kernel_restarted",
                    "previous_run_id": "kernel-old",
                },
                "ts": 2,
            }) + "\n",
            encoding="utf-8",
        )

        daemon = self.load_daemon(daemon_path)
        daemon.TRANSCRIPTS.clear()
        replay = daemon.transcript_replay("default", session)

        self.assertEqual([event["type"] for event in replay], ["message.user", "tool.result"])
        result = replay[1]
        self.assertEqual(result["id"], "call-lifecycle")
        self.assertEqual(result["name"], "exec_run")
        self.assertTrue(result["error"])
        payload = json.loads(result["output"])
        self.assertEqual(payload["error"], "tool_interrupted")
        self.assertEqual(payload["kind"], "kernel_restarted")
        self.assertFalse(payload["retryable"])

    def test_waiting_for_driver_status_retries_session_join(self):
        home = Path(self.tabula_home)
        daemon_path = home / "plugins" / "gateway-web" / "daemon.py"
        self.assertTrue(daemon_path.is_file(), "gateway-web daemon missing from installed layout")

        daemon = self.load_daemon(daemon_path)
        session = object.__new__(daemon.GatewaySession)
        sent = []
        delivered = []

        class FakeKernel:
            def send(self, message):
                sent.append(message)

        class FakeBrowser:
            def send_json(self, message):
                delivered.append(message)

        session.browser = FakeBrowser()
        session.kernel = FakeKernel()
        session.config = {"auto_driver": True}
        session.session = "main"
        session.tenant_id = "default"
        session._inflight_lock = daemon.threading.Lock()
        session._inflight_sessions = {("default", "main"): 1}
        session._inflight_touched = {("default", "main"): daemon.time.monotonic()}
        session._stream_routes = {}
        session._tool_routes = {}
        session._driver_wake_retries = {}

        session._handle_kernel_message({
            "type": "event",
            "topic": "session.status",
            "session": "main",
            "tenant_id": "default",
            "data": {"state": "waiting_for_driver", "reason": "turn_receiver_unavailable"},
        })

        self.assertEqual(sent, [{"type": "join", "session": "main", "tenant_id": "default"}])
        self.assertEqual(delivered[-1]["type"], "session.status")
        self.assertFalse(session._has_inflight("default", "main"))

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
    parser.add_argument("--url", default="")
    parser.add_argument("--observer-url", default="")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    ToolLifecycleInstalled.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(ToolLifecycleInstalled))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

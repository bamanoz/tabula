#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time
import unittest

from tabula_testbed import TestbedClient


class InitiativeInstalled(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""
    tools = {
        "initiative_configure",
        "initiative_task_add",
        "initiative_enable",
        "initiative_pause",
        "initiative_run",
        "initiative_get",
    }

    def configure_fake_acp(self) -> None:
        home = Path(self.tabula_home)
        script = home / "run" / "initiative-fake-acp.py"
        script.parent.mkdir(parents=True, exist_ok=True)
        script.write_text(
            '''import json
import sys
import uuid

for raw in sys.stdin:
    message = json.loads(raw)
    method = message.get("method")
    if method == "initialize":
        result = {"protocolVersion": 1, "agentInfo": {"name": "initiative-fixture", "version": "1"}, "agentCapabilities": {}}
    elif method == "session/new":
        result = {"sessionId": "initiative-" + uuid.uuid4().hex[:8]}
    elif method == "session/prompt":
        update = {
            "jsonrpc": "2.0",
            "method": "session/update",
            "params": {
                "sessionId": "fixture",
                "update": {
                    "sessionUpdate": "agent_message_chunk",
                    "content": {"type": "text", "text": "installed initiative completed"},
                },
            },
        }
        sys.stdout.write(json.dumps(update) + "\\n")
        sys.stdout.flush()
        result = {"stopReason": "end_turn"}
    else:
        result = {}
    sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": message.get("id"), "result": result}) + "\\n")
    sys.stdout.flush()
''',
            encoding="utf-8",
        )
        config = home / "tenants" / "default" / "config" / "plugins" / "initiative" / "config.toml"
        config.parent.mkdir(parents=True, exist_ok=True)
        command = f"{sys.executable} {script}"
        config.write_text(
            f'subagent_type = "acp"\nacp_command = {json.dumps(command)}\npoll_interval = 0.1\n',
            encoding="utf-8",
        )

    def connect(self, name: str, session: str) -> TestbedClient:
        client = TestbedClient(self.url, name=f"{name}-{time.time_ns()}")
        client.connect_join(
            session,
            sends=["message.user", "tool.call"],
            receives=["session.init", "message.user", "tool.result", "error"],
        )
        client.wait_tools(self.tools, session=session)
        return client

    def kill_worker(self) -> None:
        home = str(Path(self.tabula_home).resolve())
        output = subprocess.check_output(["ps", "-axo", "pid=,command="], text=True)
        worker_pids = []
        for line in output.splitlines():
            fields = line.strip().split(None, 1)
            if len(fields) != 2 or "scripts/run.py" not in fields[1]:
                continue
            pid = int(fields[0])
            try:
                cwd_lines = subprocess.check_output(
                    ["lsof", "-a", "-p", str(pid), "-d", "cwd", "-Fn"], text=True
                ).splitlines()
            except (OSError, subprocess.CalledProcessError):
                cwd_lines = []
            cwd = next((entry[1:] for entry in cwd_lines if entry.startswith("n")), "")
            if home in cwd and cwd.endswith("/plugins/initiative"):
                worker_pids.append(pid)
        self.assertTrue(worker_pids, f"initiative worker not found under {home}")
        for pid in worker_pids:
            os.kill(pid, signal.SIGKILL)
        trigger = Path(self.tabula_home) / "run" / "reload.touch"
        trigger.write_text("tenant=default\n", encoding="utf-8")

    def wait_after_restart(self, session: str) -> TestbedClient:
        deadline = time.monotonic() + 20
        last_error: Exception | None = None
        while time.monotonic() < deadline:
            client = TestbedClient(self.url, name=f"initiative-restart-{time.time_ns()}")
            try:
                client.connect_join(session)
                client.wait_tools(self.tools, session=session)
                state = client.call_tool("initiative_get", {"controller_id": "main"}, timeout=5).json()
                if state.get("controller", {}).get("state") == "paused":
                    return client
            except Exception as exc:
                last_error = exc
            client.close()
            time.sleep(0.5)
        raise AssertionError(f"initiative worker did not restart with persisted state: {last_error}")

    def test_bounded_run_notification_pause_and_restart_safety(self) -> None:
        home = Path(self.tabula_home)
        self.assertTrue((home / "plugins" / "initiative" / "plugin.toml").is_file())
        self.assertTrue((home / "packages" / "python" / "src" / "tabula_initiative_sdk").is_dir())
        for excluded in ("continuity", "activity", "reflection", "evolution", "mempalace"):
            self.assertFalse((home / "plugins" / excluded).exists(), f"initiative suite must not install {excluded}")
        self.configure_fake_acp()

        receiver = self.connect("initiative-receiver", "initiative-installed")
        caller = self.connect("initiative-caller", "initiative-installed")
        try:
            configured = caller.call_tool(
                "initiative_configure",
                {
                    "controller_id": "main",
                    "session": "initiative-installed",
                    "interval_minutes": 5,
                    "policy": {
                        "max_concurrency": 1,
                        "max_runs_per_day": 2,
                        "max_cost_units_per_day": 2,
                        "max_task_seconds": 30,
                        "cooldown_seconds": 0,
                        "failure_limit": 2,
                        "noop_limit": 2,
                    },
                },
                timeout=10,
            ).json()
            self.assertEqual(configured["controller"]["state"], "disabled")
            caller.call_tool(
                "initiative_task_add",
                {"controller_id": "main", "task_id": "task-1", "content": "Complete installed initiative task"},
                timeout=10,
            )
            completed = caller.call_tool(
                "initiative_run", {"controller_id": "main", "request_id": "installed-once"}, timeout=60
            ).json()
            self.assertEqual(completed["run"]["status"], "completed")
            self.assertEqual(completed["run"]["result"], "installed initiative completed")

            message = receiver.recv(type="message.user", timeout=10)
            data = message.get("data") if isinstance(message.get("data"), dict) else {}
            meta = message.get("meta") or data.get("meta") or {}
            text = message.get("text") or data.get("text") or ""
            self.assertIn('<initiative controller_id="main"', text)
            self.assertEqual(meta.get("source"), "initiative")
            self.assertEqual(meta.get("status"), "completed")

            caller.call_tool("initiative_enable", {"controller_id": "main"}, timeout=10)
            paused = caller.call_tool(
                "initiative_pause", {"controller_id": "main", "reason": "installed pause"}, timeout=10
            ).json()
            self.assertEqual(paused["controller"]["state"], "paused")
            caller.call_tool(
                "initiative_task_add",
                {"controller_id": "main", "task_id": "task-2", "content": "Must remain pending while paused"},
                timeout=10,
            )
            blocked = caller.call_tool(
                "initiative_run", {"controller_id": "main", "request_id": "blocked-second"}, timeout=10
            ).json()
            self.assertIn("paused", blocked.get("error", ""))
        finally:
            caller.close()
            receiver.close()

        self.kill_worker()
        restarted = self.wait_after_restart("initiative-installed")
        with restarted:
            state = restarted.call_tool("initiative_get", {"controller_id": "main"}, timeout=10).json()
            self.assertEqual(state["controller"]["state"], "paused")
            tasks = {item["id"]: item for item in state["agenda"]["items"]}
            self.assertEqual(tasks["task-1"]["status"], "completed")
            self.assertEqual(tasks["task-2"]["status"], "pending")
            self.assertEqual(len(state["runs"]), 1)


def main() -> int:
    parser = argparse.ArgumentParser(description="Run installed initiative capability test")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    InitiativeInstalled.url = args.url
    InitiativeInstalled.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(
        unittest.defaultTestLoader.loadTestsFromTestCase(InitiativeInstalled)
    )
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

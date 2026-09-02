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
        client.connect()
        client.create_session(session)

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
                client.connect()
                client.create_session(session)

                state = client.call_tool("initiative_get", {"controller_id": "main"}, timeout=5).json()
                if state.get("controller", {}).get("state") == "paused":
                    return client
            except Exception as exc:
                last_error = exc
            client.close()
            time.sleep(0.5)
        raise AssertionError(f"initiative worker did not restart with persisted state: {last_error}")




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

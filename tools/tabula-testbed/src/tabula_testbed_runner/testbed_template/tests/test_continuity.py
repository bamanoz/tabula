#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import signal
import subprocess
import time
import unittest

from tabula_testbed import TestbedClient


class ContinuityInstalled(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""
    tools = {
        "continuity_current",
        "continuity_history",
        "continuity_update",
        "continuity_milestone",
        "continuity_restore",
    }

    def connect(self, session: str) -> TestbedClient:
        deadline = time.monotonic() + 20
        advertised: set[str] = set()
        while time.monotonic() < deadline:
            client = TestbedClient(self.url, name=f"continuity-{session}-{time.time_ns()}")
            client.connect_join(
                session,
                sends=["message.user", "tool.call", "exchange.approve"],
                receives=["session.init", "message.user", "tool.result", "error", "exchange.approve"],
            )
            advertised = {str(tool.get("name")) for tool in client.tools() if tool.get("name")}
            if self.tools <= advertised:
                return client
            client.close()
            time.sleep(0.25)
        raise AssertionError(f"continuity tools not advertised: {sorted(advertised)}")

    def approved_call(self, client: TestbedClient, tool: str, payload: dict) -> dict:
        pending = client.call_tool_async(tool, payload, timeout=15)
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            msg = client.recv(timeout=max(0.1, deadline - time.monotonic()))
            if msg.get("type") == "request" and msg.get("topic") == "exchange.approve":
                data = msg.get("data") if isinstance(msg.get("data"), dict) else {}
                options = data.get("options") if isinstance(data.get("options"), list) else []
                self.assertIn("allow once", options)
                client._send(
                    {
                        "type": "reply",
                        "topic": "exchange.approve",
                        "id": msg.get("id", ""),
                        "data": {
                            "choice": "allow once",
                            "index": options.index("allow once"),
                            "approved": True,
                        },
                    }
                )
                return pending.wait().json()
        self.fail(f"approval exchange not received for {tool}")

    def kill_worker(self) -> None:
        home = str(Path(self.tabula_home).resolve())
        suffix = "/plugins/continuity/scripts/run.py"
        output = subprocess.check_output(["ps", "-axo", "pid=,command="], text=True)
        pids = []
        for line in output.splitlines():
            pid_text, _, command = line.strip().partition(" ")
            if home in command and suffix in command:
                pids.append(int(pid_text))
        self.assertTrue(pids, f"continuity worker not found under {home}")
        for pid in pids:
            os.kill(pid, signal.SIGKILL)

    def wait_after_restart(self, session: str) -> TestbedClient:
        deadline = time.monotonic() + 20
        last_error: Exception | None = None
        while time.monotonic() < deadline:
            client = TestbedClient(self.url, name=f"continuity-{session}-{time.time_ns()}")
            try:
                client.connect_join(
                    session,
                    sends=["message.user", "tool.call", "exchange.approve"],
                    receives=["session.init", "message.user", "tool.result", "error", "exchange.approve"],
                )
                current = client.call_tool("continuity_current", {}, timeout=5).json()
                if current.get("initialized"):
                    return client
            except Exception as exc:
                last_error = exc
            client.close()
            time.sleep(0.5)
        raise AssertionError(f"continuity worker did not restart: {last_error}")

    def test_profile_survives_fresh_session_restart_revision_and_restore(self) -> None:
        home = Path(self.tabula_home)
        self.assertTrue((home / "plugins" / "continuity" / "plugin.toml").is_file())
        for excluded in ("mempalace", "activity", "reflection", "initiative", "evolution"):
            self.assertFalse((home / "plugins" / excluded).exists(), f"continuity suite must not install {excluded}")
        config_dir = home / "config" / "plugins" / "continuity"
        config_dir.mkdir(parents=True, exist_ok=True)
        (config_dir / "config.toml").write_text(
            'approval_required_fields = ["voice", "purpose"]\n', encoding="utf-8"
        )
        state = home / "tenants" / "default" / "state" / "plugins" / "continuity"

        with self.connect("continuity-initial") as client:
            self.assertNotIn("<continuity_profile ", client.init.get("context", ""))
            first = self.approved_call(
                client,
                "continuity_update",
                {
                    "fields": {
                        "name": "Aster",
                        "purpose": "Preserve durable context",
                        "voice": "direct",
                    },
                    "reason": "initialize installed identity",
                    "approved": True,
                },
            )["profile"]
            milestone = client.call_tool(
                "continuity_milestone",
                {"summary": "Installed continuity", "significance": "First biography event"},
                timeout=10,
            ).json()["milestone"]

        first_revision_path = state / "revisions" / f"{first['revision']}.json"
        first_revision_bytes = first_revision_path.read_bytes()
        biography_bytes = (state / "biography.jsonl").read_bytes()

        with self.connect("continuity-fresh") as fresh:
            context = fresh.init.get("context", "")
            self.assertIn("<continuity_profile ", context)
            self.assertIn("name: Aster", context)
            self.assertIn("Installed continuity", context)
            second = self.approved_call(
                fresh,
                "continuity_update",
                {"fields": {"purpose": "Preserve verified durable context"}, "reason": "revise purpose"},
            )["profile"]
            history = fresh.call_tool("continuity_history", {"limit": 10}, timeout=10).json()
            self.assertEqual([item["action"] for item in history["items"]], ["update", "update"])
            self.assertEqual(history["items"][1]["previous_revision"], first["revision"])
        self.assertEqual(first_revision_path.read_bytes(), first_revision_bytes)
        self.assertEqual((state / "biography.jsonl").read_bytes(), biography_bytes)
        self.assertEqual(len((state / "history.jsonl").read_text(encoding="utf-8").splitlines()), 2)

        self.kill_worker()
        recovery = self.wait_after_restart("continuity-recovery-trigger")
        with recovery:
            current = recovery.call_tool("continuity_current", {}, timeout=10).json()
            self.assertEqual(current["profile"]["revision"], second["revision"])
            self.assertEqual(current["milestones"][0]["id"], milestone["id"])

        with self.connect("continuity-restarted") as restarted:
            self.assertIn(second["revision"], restarted.init.get("context", ""))
            restored = self.approved_call(
                restarted,
                "continuity_restore",
                {"revision": first["revision"], "reason": "verify controlled restore"},
            )
            self.assertEqual(restored["restored_from"], first["revision"])
            self.assertNotEqual(restored["profile"]["revision"], first["revision"])
            self.assertEqual(first_revision_path.read_bytes(), first_revision_bytes)
            self.assertEqual((state / "biography.jsonl").read_bytes(), biography_bytes)
            self.assertEqual(len((state / "history.jsonl").read_text(encoding="utf-8").splitlines()), 3)
            rejected = restarted.call_tool(
                "continuity_update",
                {"fields": {"constitution": "mutable"}, "reason": "must fail", "approved": True},
                timeout=10,
            ).json()
            self.assertFalse(rejected["ok"])
            self.assertIn("immutable distro policy", rejected["error"])

        with self.connect("continuity-restored") as final:
            context = final.init.get("context", "")
            self.assertIn("purpose: Preserve durable context", context)
            self.assertNotIn("purpose: Preserve verified durable context", context)


def main() -> int:
    parser = argparse.ArgumentParser(description="Run installed continuity capability test")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    ContinuityInstalled.url = args.url
    ContinuityInstalled.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(
        unittest.defaultTestLoader.loadTestsFromTestCase(ContinuityInstalled)
    )
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

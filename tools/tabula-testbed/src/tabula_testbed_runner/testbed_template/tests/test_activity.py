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


class ActivityInstalled(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""
    tools = {
        "activity_emit",
        "activity_artifact",
        "activity_artifact_read",
        "activity_timeline",
        "activity_current",
        "activity_outcomes",
    }

    def connect(self, session: str) -> TestbedClient:
        deadline = time.monotonic() + 20
        advertised: set[str] = set()
        while time.monotonic() < deadline:
            client = TestbedClient(self.url, name=f"activity-{session}-{time.time_ns()}")
            client.connect_join(
                session,
                sends=["message.user", "tool.call"],
                receives=["session.init", "message.user", "tool.result", "error"],
            )
            advertised = {str(tool.get("name")) for tool in client.tools() if tool.get("name")}
            if self.tools <= advertised:
                return client
            client.close()
            time.sleep(0.25)
        raise AssertionError(f"activity tools not advertised: {sorted(advertised)}")

    def kill_worker(self) -> None:
        home = str(Path(self.tabula_home).resolve())
        output = subprocess.check_output(["ps", "-axo", "pid=,ppid=,command="], text=True)
        processes: dict[int, tuple[int, str]] = {}
        for line in output.splitlines():
            fields = line.strip().split(None, 2)
            if len(fields) == 3:
                processes[int(fields[0])] = (int(fields[1]), fields[2])
        worker_pids = []
        for pid, (_, command) in processes.items():
            if "scripts/run.py" not in command:
                continue
            proc_cwd = Path(f"/proc/{pid}/cwd")
            if proc_cwd.exists():
                cwd = str(proc_cwd.resolve())
            else:
                try:
                    cwd_lines = subprocess.check_output(
                        ["lsof", "-a", "-p", str(pid), "-d", "cwd", "-Fn"], text=True
                    ).splitlines()
                except (OSError, subprocess.CalledProcessError):
                    cwd_lines = []
                cwd = next((line[1:] for line in cwd_lines if line.startswith("n")), "")
            if home in cwd and cwd.endswith("/plugins/activity"):
                worker_pids.append(pid)
        self.assertTrue(worker_pids, f"activity worker not found under {home}")
        for pid in worker_pids:
            os.kill(pid, signal.SIGKILL)

    def wait_after_restart(self, session: str) -> TestbedClient:
        deadline = time.monotonic() + 20
        last_error: Exception | None = None
        while time.monotonic() < deadline:
            client = TestbedClient(self.url, name=f"activity-{session}-{time.time_ns()}")
            try:
                client.connect_join(
                    session,
                    sends=["message.user", "tool.call"],
                    receives=["session.init", "message.user", "tool.result", "error"],
                )
                timeline = client.call_tool(
                    "activity_timeline", {"correlation_id": "corr-installed"}, timeout=5
                ).json()
                if len(timeline.get("items", [])) == 5:
                    return client
            except Exception as exc:
                last_error = exc
            client.close()
            time.sleep(0.5)
        raise AssertionError(f"activity worker did not restart: {last_error}")

    def test_cross_session_activity_artifact_and_projections_survive_restart(self) -> None:
        home = Path(self.tabula_home)
        self.assertTrue((home / "plugins" / "activity" / "plugin.toml").is_file())
        self.assertTrue(
            (home / "packages" / "python" / "src" / "tabula_activity_sdk").is_dir()
        )
        for excluded in ("continuity", "reflection", "initiative", "evolution", "mempalace"):
            self.assertFalse((home / "plugins" / excluded).exists(), f"activity suite must not install {excluded}")
        lock = (home / "distrib" / "testbed" / "distro.lock.json").read_text(encoding="utf-8")
        for excluded in ("continuity", "reflection", "initiative", "evolution", "mempalace"):
            self.assertNotIn(f'"{excluded}"', lock)

        with self.connect("activity-session-a") as first:
            started = first.call_tool(
                "activity_emit",
                {
                    "id": "installed-work-started",
                    "kind": "work",
                    "summary": "Implement activity capability",
                    "project_id": "project-installed",
                    "correlation_id": "corr-installed",
                    "correlations": {"task_id": "task-installed"},
                    "work_id": "work-installed",
                    "status": "started",
                },
                timeout=10,
            ).json()["event"]
            decision = first.call_tool(
                "activity_emit",
                {
                    "id": "installed-decision",
                    "kind": "decision",
                    "summary": "Use curated append-only events",
                    "project_id": "project-installed",
                    "correlation_id": "corr-installed",
                    "work_id": "work-installed",
                },
                timeout=10,
            ).json()["event"]
            stored = first.call_tool(
                "activity_artifact",
                {
                    "id": "installed-artifact-event",
                    "summary": "Stored activity report",
                    "content": "installed activity report body",
                    "name": "activity-report.txt",
                    "artifact_id": "installed-activity-report",
                    "project_id": "project-installed",
                    "correlation_id": "corr-installed",
                    "work_id": "work-installed",
                },
                timeout=10,
            ).json()

        with self.connect("activity-session-b") as second:
            completed = second.call_tool(
                "activity_emit",
                {
                    "id": "installed-work-completed",
                    "kind": "work",
                    "summary": "Activity capability implemented",
                    "project_id": "project-installed",
                    "correlation_id": "corr-installed",
                    "work_id": "work-installed",
                    "status": "completed",
                },
                timeout=10,
            ).json()["event"]
            active = second.call_tool(
                "activity_emit",
                {
                    "id": "installed-followup-started",
                    "kind": "work",
                    "summary": "Review activity capability",
                    "project_id": "project-installed",
                    "correlation_id": "corr-installed",
                    "work_id": "work-review",
                    "status": "started",
                },
                timeout=10,
            ).json()["event"]
            timeline = second.call_tool(
                "activity_timeline", {"correlation_id": "corr-installed"}, timeout=10
            ).json()
            self.assertEqual(
                [item["id"] for item in timeline["items"]],
                [started["id"], decision["id"], stored["event"]["id"], completed["id"], active["id"]],
            )
            self.assertEqual({item["session"] for item in timeline["items"]}, {
                "activity-session-a", "activity-session-b"
            })
            artifact = second.call_tool(
                "activity_artifact_read", {"ref": stored["artifact"]["ref"]}, timeout=10
            ).json()
            self.assertEqual(artifact["content"], "installed activity report body")
            self.assertEqual(artifact["artifact"]["owner_plugin"], "activity")

        state = home / "tenants" / "default" / "state" / "plugins" / "activity"
        self.assertEqual(len((state / "events.jsonl").read_text(encoding="utf-8").splitlines()), 5)
        self.assertTrue((state / "projections.json").is_file())

        self.kill_worker()
        restarted = self.wait_after_restart("activity-restart")
        with restarted:
            current = restarted.call_tool(
                "activity_current", {"project_id": "project-installed"}, timeout=10
            ).json()
            self.assertEqual([item["id"] for item in current["items"]], [active["id"]])
            outcomes = restarted.call_tool(
                "activity_outcomes", {"project_id": "project-installed"}, timeout=10
            ).json()
            self.assertEqual([item["id"] for item in outcomes["items"]], [completed["id"]])
            artifact = restarted.call_tool(
                "activity_artifact_read", {"ref": stored["artifact"]["ref"]}, timeout=10
            ).json()
            self.assertEqual(artifact["content"], "installed activity report body")


def main() -> int:
    parser = argparse.ArgumentParser(description="Run installed activity capability test")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    ActivityInstalled.url = args.url
    ActivityInstalled.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(
        unittest.defaultTestLoader.loadTestsFromTestCase(ActivityInstalled)
    )
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

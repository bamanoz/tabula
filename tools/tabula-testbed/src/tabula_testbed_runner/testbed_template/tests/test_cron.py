#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import subprocess
import sys
import unittest

from tabula_testbed import TestbedClient


class CronPluginSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def make_client(self, name: str, session: str = "testbed-cron") -> TestbedClient:
        client = TestbedClient(self.url, name=name)
        client.connect_join(session)
        return client

    def test_cron_is_plugin_and_manages_jobs(self):
        home = Path(self.tabula_home)
        self.assertFalse((home / "skills" / "cron").exists(), "cron must not be installed as a skill")
        self.assertTrue((home / "plugins" / "cron" / "plugin.toml").is_file(), "cron plugin manifest missing")

        with self.make_client("testbed-cron-tools") as client:
            client.wait_tools({"cron_add", "cron_list", "cron_remove", "cron_sync"}, session="testbed-cron")
            add = client.call_tool("cron_add", {
                "cron": "* * * * *",
                "task": "hello cron",
                "id": "testbed-cron-job",
                "session": "testbed-cron",
                "once": True,
            }).json()
            self.assertTrue(add["ok"], add)
            listed = client.call_tool("cron_list", {}).json()
            ids = {job["id"] for job in listed.get("jobs", [])}
            self.assertIn("testbed-cron-job", ids)
            removed = client.call_tool("cron_remove", {"id": "testbed-cron-job"}).json()
            self.assertTrue(removed["ok"], removed)

    def test_cron_plugin_scheduler_fires_messages(self):
        receiver = self.make_client("testbed-cron-receiver")
        sender = self.make_client("testbed-cron-sender")
        try:
            sender.wait_tools({"cron_add"}, session="testbed-cron")
            add = sender.call_tool("cron_add", {
                "cron": "* * * * *",
                "task": "scheduled hello",
                "id": "testbed-cron-fire",
                "session": "testbed-cron",
                "once": True,
            }).json()
            self.assertTrue(add["ok"], add)
            cron_runner = Path(self.tabula_home) / "plugins" / "cron" / "run.py"
            python = Path(self.tabula_home) / ".venv" / "bin" / "python3"
            if not python.is_file():
                python = Path(sys.executable)
            env = os.environ.copy()
            env.update({"TABULA_HOME": self.tabula_home, "TABULA_URL": self.url})
            subprocess.run(
                [str(python), str(cron_runner), "fire", "--id", "testbed-cron-fire", "--task", "scheduled hello", "--session", "testbed-cron"],
                env=env,
                check=True,
            )
            msg = receiver.recv(type="message.user", timeout=10)
            self.assertEqual(msg.get("id"), "testbed-cron-fire")
            self.assertIn('<cron_job id="testbed-cron-fire"', msg.get("text", ""))
            self.assertIn("scheduled hello", msg.get("text", ""))
            self.assertEqual(msg.get("meta", {}).get("source"), "cron")
            self.assertEqual(msg.get("meta", {}).get("job_id"), "testbed-cron-fire")
        finally:
            sender.call_tool("cron_remove", {"id": "testbed-cron-fire"})
            sender.close()
            receiver.close()


def main() -> int:
    parser = argparse.ArgumentParser(description="Run cron testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    CronPluginSmoke.url = args.url
    CronPluginSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(CronPluginSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

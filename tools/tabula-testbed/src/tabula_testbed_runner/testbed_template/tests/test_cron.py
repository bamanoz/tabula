#!/usr/bin/env python3
from __future__ import annotations

import argparse
from datetime import datetime, timedelta, timezone
import os
from pathlib import Path
import unittest

from tabula_testbed import TestbedClient


class SchedulePluginSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def make_client(self, name: str, session: str = "testbed-schedule") -> TestbedClient:
        client = TestbedClient(self.url, name=name)
        client.connect()
        client.create_session(session)
        return client

    def test_schedule_is_plugin_and_manages_canonical_records(self) -> None:
        home = Path(self.tabula_home)
        self.assertFalse((home / "skills" / "cron").exists(), "cron must not be installed as a skill")
        self.assertTrue((home / "plugins" / "cron" / "plugin.toml").is_file(), "cron plugin manifest missing")

        tools = {"schedule_put", "schedule_list", "schedule_pause", "schedule_resume", "schedule_remove"}
        with self.make_client("testbed-schedule-tools") as client:

            created = client.call_tool("schedule_put", {
                "id": "testbed-schedule-job",
                "trigger": {"type": "cron", "expr": "0 9 * * *"},
                "payload": {"message": "hello schedule"},
                "timezone": "UTC",
            }).json()
            self.assertTrue(created["ok"], created)
            record = created["schedule"]
            self.assertEqual(record["payload"]["session"], "testbed-schedule")
            for legacy in ("cron", "task", "session", "once"):
                self.assertNotIn(legacy, record)

            listed = client.call_tool("schedule_list", {}).json()
            self.assertIn("testbed-schedule-job", {job["id"] for job in listed.get("schedules", [])})
            paused = client.call_tool("schedule_pause", {"id": "testbed-schedule-job"}).json()
            self.assertTrue(paused["schedule"]["paused"])
            resumed = client.call_tool("schedule_resume", {"id": "testbed-schedule-job"}).json()
            self.assertFalse(resumed["schedule"]["paused"])
            removed = client.call_tool("schedule_remove", {"id": "testbed-schedule-job"}).json()
            self.assertTrue(removed["ok"], removed)




def main() -> int:
    parser = argparse.ArgumentParser(description="Run durable schedule testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    SchedulePluginSmoke.url = args.url
    SchedulePluginSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(SchedulePluginSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

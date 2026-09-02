#!/usr/bin/env python3
from __future__ import annotations

import unittest

from tabula_testbed import TestbedClient


class TaskSchedulingSDKTestbed(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def test_installed_controller_recovers_claims_and_records_completion(self) -> None:
        session = "testbed-task-scheduling-sdk"
        with TestbedClient(self.url, name="testbed-task-scheduling-sdk") as client:
            client.connect()
            client.create_session(session)
            payload = client.call_tool(
                "task_scheduling_sdk_fixture_run",
                {"id": "installed-task-scheduling"},
                timeout=60,
            ).json()

        self.assertTrue(payload["ok"], payload)
        self.assertEqual(payload["task_status"], "completed")
        self.assertEqual(payload["task_attempt"], 2)
        self.assertEqual(payload["task_recovered_ids"], ["installed-task-scheduling"])
        self.assertEqual(payload["schedule_recovered_ids"], ["installed-task-scheduling-schedule"])
        self.assertEqual(payload["first_delivery_id"], payload["second_delivery_id"])
        self.assertTrue(payload["schedule_paused"])
        self.assertIsNone(payload["schedule_next_run_at"])

#!/usr/bin/env python3
from __future__ import annotations

import unittest

from tabula_testbed import TestbedClient


class SubagentSDKTestbed(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def test_installed_plugin_spawns_and_consumes_subagent_via_sdk(self) -> None:
        session = "testbed-subagents-sdk"
        with TestbedClient(self.url, name="testbed-subagents-sdk") as client:
            client.connect_join(session)
            client.wait_tools({"subagent_sdk_fixture_run"}, session=session)
            payload = client.call_tool("subagent_sdk_fixture_run", {
                "id": "testbed-sdk-job",
                "campaign_id": "testbed-campaign",
            }, timeout=90).json()

        self.assertTrue(payload["ok"], payload)
        self.assertEqual(payload["status"], "completed")
        self.assertEqual(payload["result"], "SDK_RESULT_OK")
        self.assertEqual(payload["correlation"]["campaign_id"], "testbed-campaign")
        self.assertIn("testbed-sdk-job", payload["recovered_ids"])
        self.assertNotIn("testbed-sdk-job", payload["running_after"])

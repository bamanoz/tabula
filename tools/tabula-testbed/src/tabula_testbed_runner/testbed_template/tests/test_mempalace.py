#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import unittest

from tabula_testbed import TestbedClient


class MempalaceSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def make_client(self) -> TestbedClient:
        client = TestbedClient(self.url, name="testbed-mempalace")
        client.connect_join("testbed-mempalace")
        return client

    def test_mempalace_validation_errors_execute_installed_scripts(self):
        with self.make_client() as client:
            client.wait_tools({
                "mempalace_add_drawer",
                "mempalace_search",
                "mempalace_list_drawers",
                "mempalace_get_drawer",
                "mempalace_delete_drawer",
                "mempalace_list_wings",
                "mempalace_list_rooms",
                "mempalace_status",
            }, session="testbed-mempalace")
            result = client.call_tool("mempalace_add_drawer", {"wing": "", "room": "", "content": "x"}).json()
            self.assertFalse(result.get("success"), result)
            result = client.call_tool("mempalace_add_drawer", {"wing": "people", "room": "facts", "content": ""}).json()
            self.assertFalse(result.get("success"), result)
            result = client.call_tool("mempalace_search", {"query": ""}).json()
            self.assertIn("error", result)
            result = client.call_tool("mempalace_search", {"query": "x", "limit": "nope"}).json()
            self.assertIn("error", result)
            result = client.call_tool("mempalace_list_drawers", {"limit": "nope"}).json()
            self.assertIn("error", result)
            result = client.call_tool("mempalace_list_drawers", {"offset": "nope"}).json()
            self.assertIn("error", result)
            result = client.call_tool("mempalace_get_drawer", {"drawer_id": ""}).json()
            self.assertIn("error", result)
            result = client.call_tool("mempalace_delete_drawer", {"drawer_id": ""}).json()
            self.assertFalse(result.get("success"), result)

    def test_mempalace_success_search_admin_delete_flow(self):
        with self.make_client() as client:
            client.wait_tools({"mempalace_add_drawer", "mempalace_search", "mempalace_status"}, session="testbed-mempalace-flow")
            content = "Veniamin likes precise installed-layout tests for Tabula MemPalace."
            saved = client.call_tool("mempalace_add_drawer", {
                "wing": "testbed",
                "room": "facts",
                "content": content,
                "source_file": "testbed-mempalace",
            }, timeout=30).json()
            self.assertTrue(saved.get("success"), saved)
            drawer_id = saved.get("drawer_id")
            self.assertIsInstance(drawer_id, str)
            self.assertTrue(drawer_id)

            status = client.call_tool("mempalace_status", {}, timeout=30).json()
            self.assertEqual(status.get("palace_path"), str(Path(self.tabula_home) / "data" / "mempalace" / "palace"))
            self.assertGreaterEqual(status.get("total_drawers", 0), 1)

            wings = client.call_tool("mempalace_list_wings", {}, timeout=30).json()
            self.assertIn("testbed", str(wings))

            rooms = client.call_tool("mempalace_list_rooms", {"wing": "testbed"}, timeout=30).json()
            self.assertIn("facts", str(rooms))

            listed = client.call_tool("mempalace_list_drawers", {"wing": "testbed", "room": "facts", "limit": 10}, timeout=30).json()
            self.assertIn(drawer_id, {item.get("drawer_id") for item in listed.get("drawers", [])})

            got = client.call_tool("mempalace_get_drawer", {"drawer_id": drawer_id}, timeout=30).json()
            self.assertEqual(got.get("drawer_id"), drawer_id)
            self.assertEqual(got.get("content"), content)

            found = client.call_tool("mempalace_search", {"query": "installed-layout tests", "wing": "testbed", "limit": 5}, timeout=30).json()
            self.assertIn(content, str(found))

            cyrillic_room = "cyrillic-search"
            warmup = client.call_tool("mempalace_search", {"query": "smoke", "wing": "testbed", "room": cyrillic_room, "limit": 5}, timeout=30).json()
            self.assertEqual(warmup.get("results"), [])
            cyrillic_content = "кириллица smoke память поиск"
            cyrillic = client.call_tool("mempalace_add_drawer", {
                "wing": "testbed",
                "room": cyrillic_room,
                "content": cyrillic_content,
                "source_file": "testbed-mempalace-cyrillic",
            }, timeout=30).json()
            self.assertTrue(cyrillic.get("success"), cyrillic)
            cyrillic_id = cyrillic.get("drawer_id")
            found_cyrillic = client.call_tool("mempalace_search", {"query": "smoke", "wing": "testbed", "room": cyrillic_room, "limit": 5}, timeout=30).json()
            self.assertIn(cyrillic_content, str(found_cyrillic))
            self.assertTrue(client.call_tool("mempalace_delete_drawer", {"drawer_id": cyrillic_id}, timeout=30).json().get("success"))

            wake = client.call_tool("mempalace_get_taxonomy", {}, timeout=30).json()
            self.assertIn("testbed", str(wake))

            deleted = client.call_tool("mempalace_delete_drawer", {"drawer_id": drawer_id}, timeout=30).json()
            self.assertTrue(deleted.get("success"), deleted)

            after = client.call_tool("mempalace_get_drawer", {"drawer_id": drawer_id}, timeout=30).json()
            self.assertIn("not found", str(after).lower())


def main() -> int:
    parser = argparse.ArgumentParser(description="Run MemPalace testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    MempalaceSmoke.url = args.url
    MempalaceSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(MempalaceSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import unittest

from tabula_testbed import TestbedClient


class MemorySmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def make_client(self) -> TestbedClient:
        client = TestbedClient(self.url, name="testbed-memory")
        client.connect_join("testbed-memory")
        return client

    def test_memory_validation_errors_execute_installed_scripts(self):
        with self.make_client() as client:
            client.wait_tools({
                "memory_save",
                "memory_search",
                "memory_wake_up",
                "memory_list",
                "memory_get",
                "memory_delete",
                "memory_wings",
                "memory_rooms",
                "memory_status",
            }, session="testbed-memory")
            result = client.call_tool("memory_save", {"wing": "", "room": "", "content": "x"}).json()
            self.assertEqual(result, {"error": "wing and room are required"})
            result = client.call_tool("memory_save", {"wing": "people", "room": "facts", "content": ""}).json()
            self.assertEqual(result, {"error": "content is empty"})
            result = client.call_tool("memory_search", {"query": ""}).json()
            self.assertEqual(result, {"error": "query is required"})
            result = client.call_tool("memory_search", {"query": "x", "limit": "nope"}).json()
            self.assertEqual(result, {"error": "limit must be an integer"})
            result = client.call_tool("memory_list", {"limit": "nope"}).json()
            self.assertEqual(result, {"error": "limit must be an integer"})
            result = client.call_tool("memory_list", {"offset": "nope"}).json()
            self.assertEqual(result, {"error": "offset must be an integer"})
            result = client.call_tool("memory_get", {"drawer_id": ""}).json()
            self.assertEqual(result, {"error": "drawer_id is required"})
            result = client.call_tool("memory_delete", {"drawer_id": ""}).json()
            self.assertEqual(result, {"error": "drawer_id is required"})

    def test_memory_success_search_admin_delete_flow(self):
        with self.make_client() as client:
            client.wait_tools({"memory_save", "memory_search", "memory_status"}, session="testbed-memory-flow")
            content = "Veniamin likes precise installed-layout tests for Tabula memory."
            saved = client.call_tool("memory_save", {
                "wing": "testbed",
                "room": "facts",
                "content": content,
                "source": "testbed-memory",
            }, timeout=30).json()
            self.assertTrue(saved.get("success"), saved)
            drawer_id = saved.get("drawer_id")
            self.assertIsInstance(drawer_id, str)
            self.assertTrue(drawer_id)

            status = client.call_tool("memory_status", {}, timeout=30).json()
            self.assertEqual(status.get("palace_path"), str(Path(self.tabula_home) / "data" / "memory" / "palace"))
            self.assertGreaterEqual(status.get("total_drawers", 0), 1)

            wings = client.call_tool("memory_wings", {}, timeout=30).json()
            self.assertIn("testbed", str(wings))

            rooms = client.call_tool("memory_rooms", {"wing": "testbed"}, timeout=30).json()
            self.assertIn("facts", str(rooms))

            listed = client.call_tool("memory_list", {"wing": "testbed", "room": "facts", "limit": 10}, timeout=30).json()
            self.assertIn(drawer_id, {item.get("drawer_id") for item in listed.get("drawers", [])})

            got = client.call_tool("memory_get", {"drawer_id": drawer_id}, timeout=30).json()
            self.assertEqual(got.get("drawer_id"), drawer_id)
            self.assertEqual(got.get("content"), content)

            found = client.call_tool("memory_search", {"query": "installed-layout tests", "wing": "testbed", "limit": 5}, timeout=30).json()
            self.assertIn(content, str(found))

            cyrillic_room = "cyrillic-search"
            warmup = client.call_tool("memory_search", {"query": "smoke", "wing": "testbed", "room": cyrillic_room, "limit": 5}, timeout=30).json()
            self.assertEqual(warmup.get("results"), [])
            cyrillic_content = "кириллица smoke память поиск"
            cyrillic = client.call_tool("memory_save", {
                "wing": "testbed",
                "room": cyrillic_room,
                "content": cyrillic_content,
                "source": "testbed-memory-cyrillic",
            }, timeout=30).json()
            self.assertTrue(cyrillic.get("success"), cyrillic)
            cyrillic_id = cyrillic.get("drawer_id")
            found_cyrillic = client.call_tool("memory_search", {"query": "smoke", "wing": "testbed", "room": cyrillic_room, "limit": 5}, timeout=30).json()
            self.assertIn(cyrillic_content, str(found_cyrillic))
            self.assertTrue(client.call_tool("memory_delete", {"drawer_id": cyrillic_id}, timeout=30).json().get("success"))

            wake = client.call_tool("memory_wake_up", {"wing": "testbed"}, timeout=30).json()
            self.assertIn("testbed", str(wake))

            deleted = client.call_tool("memory_delete", {"drawer_id": drawer_id}, timeout=30).json()
            self.assertTrue(deleted.get("success"), deleted)

            after = client.call_tool("memory_get", {"drawer_id": drawer_id}, timeout=30).json()
            self.assertIn("not found", str(after).lower())


def main() -> int:
    parser = argparse.ArgumentParser(description="Run memory testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    MemorySmoke.url = args.url
    MemorySmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(MemorySmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

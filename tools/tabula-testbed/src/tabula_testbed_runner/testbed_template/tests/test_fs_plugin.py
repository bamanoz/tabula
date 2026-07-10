#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import subprocess
import unittest

from tabula_testbed import TestbedClient


class FSPluginSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    @classmethod
    def tabula_bin(cls) -> str:
        candidate = Path(cls.tabula_home) / "bin" / "tabula"
        return str(candidate) if candidate.is_file() else "tabula"

    @classmethod
    def setUpClass(cls) -> None:
        cls.workspace_root = Path(cls.tabula_home) / "workspace-root"
        cls.workspace_root.mkdir(parents=True, exist_ok=True)
        cls.shared_root = Path(cls.tabula_home) / "shared-skill-root"
        cls.shared_root.mkdir(parents=True, exist_ok=True)
        (cls.shared_root / "guide.txt").write_text("shared skill root\n", encoding="utf-8")
        env = os.environ.copy()
        env["TABULA_HOME"] = cls.tabula_home
        subprocess.run(
            [cls.tabula_bin(), "tenant", "set", "default", "--workspace-root", str(cls.workspace_root)],
            env=env,
            check=True,
            timeout=30,
            stdout=subprocess.DEVNULL,
        )
        config_dir = Path(cls.tabula_home) / "config" / "plugins" / "fs"
        config_dir.mkdir(parents=True, exist_ok=True)
        (config_dir / "config.toml").write_text(
            'deny_globs = ["**/.env", "blocked", "blocked/**"]\nmax_read_bytes = 1048576\nfollow_symlinks = false\n',
            encoding="utf-8",
        )
        tenant_config_dir = Path(cls.tabula_home) / "tenants" / "default" / "config" / "plugins" / "fs"
        tenant_config_dir.mkdir(parents=True, exist_ok=True)
        (tenant_config_dir / "config.toml").write_text('roots = ["${project_root}"]\n', encoding="utf-8")
        global_config = Path(cls.tabula_home) / "config" / "global.toml"
        existing = global_config.read_text(encoding="utf-8") if global_config.is_file() else ""
        global_config.write_text(existing + f'\n[plugins.fs]\nroots = ["{cls.shared_root}"]\n', encoding="utf-8")

    def make_client(self) -> TestbedClient:
        client = TestbedClient(self.url, name="testbed-fs")
        client.connect_join("testbed-fs", tenant_id="default")
        return client

    def call_json(self, client: TestbedClient, tool: str, args: dict) -> dict:
        return client.call_tool(tool, args, timeout=10).json()

    def test_all_fs_tools_and_outside_root_denial(self) -> None:
        with self.make_client() as client:
            client.wait_tools({"fs_read", "fs_write", "fs_edit", "fs_delete", "fs_glob", "fs_grep", "fs_list", "fs_stat", "session_edits"}, session="testbed-fs", tenant_id="default")
            note = self.workspace_root / "notes.txt"
            nested = self.workspace_root / "nested"
            nested.mkdir()
            other = nested / "other.txt"
            other.write_text("needle\n", encoding="utf-8")
            symlink = None
            if hasattr(os, "symlink"):
                outside_target = Path(self.tabula_home).parent / "outside-symlink-target.txt"
                outside_target.write_text("needle escape", encoding="utf-8")
                symlink = nested / "escape-link.txt"
                symlink.symlink_to(outside_target)

            written = self.call_json(client, "fs_write", {"path": str(note), "content": "alpha\nbeta\nalpha\n"})
            self.assertEqual(written["bytes_written"], len("alpha\nbeta\nalpha\n".encode("utf-8")))

            read = self.call_json(client, "fs_read", {"path": str(note), "start_line": 2, "end_line": 3})
            self.assertEqual(read, {"content": "beta\nalpha", "truncated": False})

            edit = self.call_json(client, "fs_edit", {"path": str(note), "old_string": "alpha", "new_string": "gamma", "replace_all": True})
            self.assertEqual(edit, {"replacements": 2})

            listed = self.call_json(client, "fs_list", {"path": str(self.workspace_root)})
            self.assertEqual(sorted(entry["name"] for entry in listed["entries"]), ["nested", "notes.txt"])
            self.assertEqual(listed["denied_count"], 0)

            stat = self.call_json(client, "fs_stat", {"path": str(note)})
            self.assertEqual(stat["kind"], "file")
            self.assertGreater(stat["size"], 0)

            globbed = self.call_json(client, "fs_glob", {"root": str(self.workspace_root), "pattern": "**/*.txt"})
            self.assertEqual(sorted(Path(path).name for path in globbed["paths"]), ["notes.txt", "other.txt"])
            self.assertEqual(globbed["denied_count"], 1 if symlink else 0)

            grep = self.call_json(client, "fs_grep", {"path": str(self.workspace_root), "pattern": "gamma|needle", "include": "*.txt"})
            self.assertEqual(sorted(Path(match["path"]).name for match in grep["matches"]), ["notes.txt", "notes.txt", "other.txt"])
            self.assertEqual(grep["denied_count"], 1 if symlink else 0)

            deleted = self.call_json(client, "fs_delete", {"path": str(other)})
            self.assertEqual(deleted, {"ok": True})

            edits = self.call_json(client, "session_edits", {"session": "testbed-fs", "last": 10})
            self.assertEqual([group["edits"][0]["operation"] for group in edits["groups"][-3:]], ["create", "update", "delete"])
            self.assertEqual(edits["groups"][-3]["edits"][0]["path"], str(note))
            self.assertEqual(edits["groups"][-2]["edits"][0]["patch"]["format"], "unified")
            self.assertEqual(edits["groups"][-1]["edits"][0]["path"], str(other))

            env_file = self.workspace_root / ".env"
            env_file.write_text("needle=secret\n", encoding="utf-8")
            blocked = self.workspace_root / "blocked"
            blocked.mkdir()
            (blocked / "hidden.txt").write_text("needle\n", encoding="utf-8")

            listed = self.call_json(client, "fs_list", {"path": str(self.workspace_root)})
            self.assertEqual(sorted(entry["name"] for entry in listed["entries"]), ["nested", "notes.txt"])
            self.assertEqual(listed["denied_count"], 2)

            globbed = self.call_json(client, "fs_glob", {"root": str(self.workspace_root), "pattern": "**/*"})
            self.assertNotIn(".env", {Path(path).name for path in globbed["paths"]})
            self.assertNotIn("hidden.txt", {Path(path).name for path in globbed["paths"]})
            self.assertGreaterEqual(globbed["denied_count"], 2)

            grep = self.call_json(client, "fs_grep", {"path": str(self.workspace_root), "pattern": "needle"})
            self.assertNotIn(".env", {Path(match["path"]).name for match in grep["matches"]})
            self.assertNotIn("hidden.txt", {Path(match["path"]).name for match in grep["matches"]})
            self.assertGreaterEqual(grep["denied_count"], 2)

            denied = client.call_tool("fs_read", {"path": str(env_file)}, timeout=10)
            self.assertIn("path denied by glob", denied.output)

            outside = Path(self.tabula_home).parent / "outside.txt"
            denied = client.call_tool("fs_read", {"path": str(outside)}, timeout=10)
            self.assertIn("path is outside configured roots", denied.output)

            if symlink is not None:
                denied = client.call_tool("fs_read", {"path": str(symlink)}, timeout=10)
                self.assertIn("path is outside configured roots", denied.output)

    def test_project_root_config_change_applies_on_next_call(self) -> None:
        with self.make_client() as client:
            client.wait_tools({"fs_read", "fs_write"}, session="testbed-fs", tenant_id="default")
            before = self.workspace_root / "before.txt"
            self.call_json(client, "fs_write", {"path": str(before), "content": "before"})

            next_root = Path(self.tabula_home) / "workspace-root-next"
            next_root.mkdir(parents=True, exist_ok=True)
            env = os.environ.copy()
            env["TABULA_HOME"] = self.tabula_home

            def restore_workspace_root() -> None:
                subprocess.run(
                    [self.tabula_bin(), "tenant", "set", "default", "--workspace-root", str(self.workspace_root)],
                    env=env,
                    check=True,
                    timeout=30,
                    stdout=subprocess.DEVNULL,
                )

            self.addCleanup(restore_workspace_root)
            subprocess.run(
                [self.tabula_bin(), "tenant", "set", "default", "--workspace-root", str(next_root)],
                env=env,
                check=True,
                timeout=30,
                stdout=subprocess.DEVNULL,
            )

            after = next_root / "after.txt"
            self.call_json(client, "fs_write", {"path": str(after), "content": "after"})
            denied = client.call_tool("fs_read", {"path": str(before)}, timeout=10)
            self.assertIn("path is outside configured roots", denied.output)

    def test_global_plugin_roots_compose_with_tenant_workspace_root(self) -> None:
        with self.make_client() as client:
            client.wait_tools({"fs_read", "fs_write"}, session="testbed-fs", tenant_id="default")
            relative = self.workspace_root / "relative.txt"
            self.call_json(client, "fs_write", {"path": "relative.txt", "content": "workspace default"})
            self.assertEqual(relative.read_text(encoding="utf-8"), "workspace default")

            shared = self.call_json(client, "fs_read", {"path": str(self.shared_root / "guide.txt")})
            self.assertEqual(shared, {"content": "shared skill root\n", "truncated": False})


def main() -> int:
    parser = argparse.ArgumentParser(description="Run fs plugin testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    FSPluginSmoke.url = args.url
    FSPluginSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(FSPluginSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

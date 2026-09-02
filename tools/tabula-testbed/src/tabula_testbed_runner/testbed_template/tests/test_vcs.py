#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import subprocess
import unittest

from tabula_testbed import TestbedClient


class VCSInstalled(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    @classmethod
    def tabula_bin(cls) -> str:
        candidate = Path(cls.tabula_home) / "bin" / ("tabula.exe" if os.name == "nt" else "tabula")
        return str(candidate) if candidate.is_file() else "tabula"

    @classmethod
    def git(cls, cwd: Path, *args: str) -> str:
        proc = subprocess.run(["git", *args], cwd=cwd, text=True, capture_output=True, check=True, timeout=30)
        return proc.stdout.strip()

    @classmethod
    def setUpClass(cls) -> None:
        cls.repo = Path(cls.tabula_home) / "vcs-workspace"
        cls.repo.mkdir(parents=True, exist_ok=True)
        cls.git(cls.repo, "init")
        cls.git(cls.repo, "config", "user.name", "Testbed")
        cls.git(cls.repo, "config", "user.email", "testbed@example.test")
        (cls.repo / "file.txt").write_text("one\n", encoding="utf-8")
        (cls.repo / "other.txt").write_text("base\n", encoding="utf-8")
        cls.git(cls.repo, "add", "file.txt", "other.txt")
        cls.git(cls.repo, "commit", "-m", "initial")

        env = os.environ.copy()
        env["TABULA_HOME"] = cls.tabula_home
        subprocess.run(
            [cls.tabula_bin(), "tenant", "set", "default", "--workspace-root", str(cls.repo)],
            env=env,
            check=True,
            timeout=30,
            stdout=subprocess.DEVNULL,
        )

        vcs_config = Path(cls.tabula_home) / "tenants" / "default" / "config" / "plugins" / "vcs"
        vcs_config.mkdir(parents=True, exist_ok=True)
        (vcs_config / "config.toml").write_text('repositories = ["${project_root}"]\n', encoding="utf-8")

        permissions = Path(cls.tabula_home) / "tenants" / "default" / "config" / "plugins" / "hook-permissions"
        permissions.mkdir(parents=True, exist_ok=True)
        (permissions / "config.toml").write_text(
            'default = "deny"\ndeny_untyped = true\n'
            'rules = [{ tool = "vcs_*", effect = "allow", meta = { resource = "vcs" } }]\n',
            encoding="utf-8",
        )

    def make_client(self) -> TestbedClient:
        client = TestbedClient(self.url, name="testbed-vcs")
        client.connect()
        client.create_session("testbed-vcs", tenant_id="default")
        return client

    @staticmethod
    def call(client: TestbedClient, tool: str, args: dict) -> dict:
        return client.call_tool(tool, args, timeout=30).json()

    def test_structured_read_worktree_commit_and_recovery(self) -> None:
        with self.make_client() as client:
            status = self.call(client, "vcs_status", {})
            self.assertTrue(status["clean"])
            self.assertEqual(Path(status["repository"]["configured_root"]).resolve(), self.repo.resolve())
            initial = status["repository"]["head"]

            (self.repo / "file.txt").write_text("one\ntwo\n", encoding="utf-8")
            (self.repo / "other.txt").write_text("staged unrelated\n", encoding="utf-8")
            self.git(self.repo, "add", "other.txt")
            diff = self.call(client, "vcs_diff", {"paths": ["file.txt"]})
            self.assertIn("two", diff["patch"])
            self.assertEqual(diff["paths"], ["file.txt"])

            committed = self.call(client, "vcs_commit", {"message": "second", "paths": ["file.txt"]})
            second = committed["commit"]
            self.assertEqual(committed["changed_paths"], ["file.txt"])
            self.assertEqual(self.git(self.repo, "show", "--format=", "--name-only", "HEAD"), "file.txt")
            self.assertEqual(self.git(self.repo, "diff", "--cached", "--name-only"), "other.txt")
            self.git(self.repo, "restore", "--staged", "--worktree", "other.txt")
            self.assertNotEqual(second, initial)

            worktree = Path(self.tabula_home) / "vcs-worktrees" / "isolated"
            created = self.call(
                client,
                "vcs_worktree_create",
                {"path": str(worktree), "branch": "testbed/isolated", "ref": "HEAD"},
            )
            self.assertEqual(Path(created["worktree"]["worktree_root"]).resolve(), worktree.resolve())
            preview = self.call(client, "vcs_worktree_remove", {"path": str(worktree)})
            self.assertFalse(preview["confirmed"])
            removed = self.call(client, "vcs_worktree_remove", {"path": str(worktree), "confirm": True})
            self.assertTrue(removed["confirmed"])
            self.assertFalse(worktree.exists())

            (self.repo / "file.txt").write_text("discarded\n", encoding="utf-8")
            (self.repo / "untracked.txt").write_text("recoverable\n", encoding="utf-8")
            preview = self.call(client, "vcs_restore", {"paths": ["file.txt", "untracked.txt"]})
            self.assertFalse(preview["confirmed"])
            restored = self.call(
                client,
                "vcs_restore",
                {"paths": ["file.txt", "untracked.txt"], "confirm": True},
            )
            self.assertTrue(restored["confirmed"])
            rescue_ref = restored["rescue"]["ref"]
            self.assertEqual(self.git(self.repo, "show", f"{rescue_ref}:untracked.txt"), "recoverable")
            self.assertEqual((self.repo / "file.txt").read_text(encoding="utf-8"), "one\ntwo\n")
            self.assertFalse((self.repo / "untracked.txt").exists())

            if os.name != "nt":
                outside = Path(self.tabula_home) / "vcs-outside"
                outside.mkdir(exist_ok=True)
                victim = outside / "victim.txt"
                victim.write_text("keep\n", encoding="utf-8")
                link = self.repo / "link"
                link.symlink_to(outside, target_is_directory=True)
                rejected = client.call_tool(
                    "vcs_restore",
                    {"paths": ["link/victim.txt"], "confirm": True},
                    timeout=30,
                )
                self.assertFalse(rejected.ok)
                self.assertEqual(victim.read_text(encoding="utf-8"), "keep\n")
                link.unlink()

            rollback_preview = self.call(client, "vcs_rollback", {"target": initial})
            self.assertFalse(rollback_preview["confirmed"])
            rolled_back = self.call(client, "vcs_rollback", {"target": initial, "confirm": True})
            self.assertTrue(rolled_back["confirmed"])
            self.assertEqual(self.git(self.repo, "rev-parse", rolled_back["rescue"]["ref"]), second)
            self.assertEqual(self.git(self.repo, "rev-parse", "HEAD"), initial)

            log = self.call(client, "vcs_log", {"count": 1})
            self.assertEqual(log["entries"][0]["commit"], initial)


def main() -> int:
    parser = argparse.ArgumentParser(description="Run installed workspace VCS tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    VCSInstalled.url = args.url
    VCSInstalled.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(VCSInstalled))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

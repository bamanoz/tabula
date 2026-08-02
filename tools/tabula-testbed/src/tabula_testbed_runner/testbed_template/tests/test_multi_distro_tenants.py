#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import tomllib
import unittest

from tabula_testbed import TestbedClient


class MultiDistroTenantsInstalled(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    @classmethod
    def setUpClass(cls) -> None:
        cls.home = Path(cls.tabula_home).resolve()
        cls.generated = cls.home / "generated-testbed"
        with (cls.generated / "distro.toml").open("rb") as handle:
            manifest = tomllib.load(handle)
        cls.sources = {
            name: str(entry["source"])
            for name, entry in manifest.get("sources", {}).items()
        }
        cls.code_source = cls._distro_source("code")
        cls.code_immune_source = cls._distro_source("code-immune")
        cls.claw_source = cls._distro_source("claw")
        cls.code_workspace = cls.home / "workspaces" / "code"
        cls.code_immune_workspace = cls.home / "workspaces" / "code-immune"
        cls.claw_workspace = cls.home / "workspaces" / "claw"
        cls.code_workspace.mkdir(parents=True)
        cls.code_immune_workspace.mkdir(parents=True)
        cls.claw_workspace.mkdir(parents=True)
        (cls.code_workspace / "code-only.txt").write_text("code\n", encoding="utf-8")
        (cls.claw_workspace / "claw-only.txt").write_text("claw\n", encoding="utf-8")

        cls._install_tenant("code-project", cls.code_source, cls.code_workspace)
        cls._install_tenant("code-immune-project", cls.code_immune_source, cls.code_immune_workspace)
        cls._install_tenant("claw-project", cls.claw_source, cls.claw_workspace)
        cls._wait_for_tenant_tools("code-project", {"fs_list", "todo_read", "codegraph_search"})
        cls._wait_for_tenant_tools("code-immune-project", {"fs_read"})
        cls._wait_for_tenant_tools("claw-project", {"fs_list", "fs_read", "todo_read", "subagent_list"})

    @classmethod
    def _distro_source(cls, distro: str) -> str:
        source = cls.sources.get("tabula-distrib", "").strip()
        if not source:
            raise AssertionError("testbed source tabula-distrib is required")
        base, marker, fragment = source.partition("#")
        if marker and fragment:
            raise AssertionError(f"tabula-distrib source must address repository root: {source}")
        return f"{base}#path={distro}"

    @classmethod
    def _python(cls) -> str:
        candidates = (
            cls.home / ".venv" / "bin" / "python3",
            cls.home / ".venv" / "Scripts" / "python.exe",
        )
        return str(next((path for path in candidates if path.is_file()), Path(sys.executable)))

    @classmethod
    def _agent(cls) -> list[str]:
        candidates = (
            cls.home / ".venv" / "bin" / "tabula-agent",
            cls.home / ".venv" / "Scripts" / "tabula-agent.exe",
        )
        executable = next((path for path in candidates if path.is_file()), None)
        if executable is not None:
            return [str(executable)]
        return [cls._python(), "-m", "tabula_distro.agent_cli"]

    @classmethod
    def _env(cls) -> dict[str, str]:
        env = os.environ.copy()
        env["TABULA_HOME"] = str(cls.home)
        bundles = cls.sources.get("tabula-bundles", "").strip()
        if bundles.startswith("local:"):
            env["TABULA_SOURCE_ALIAS_TABULA_BUNDLES"] = bundles
        return env

    @classmethod
    def _install_tenant(cls, tenant: str, source: str, workspace: Path) -> None:
        subprocess.run(
            [
                *cls._agent(),
                "--home", str(cls.home),
                "install",
                "--distro", source,
                "--bind", str(workspace),
                "--tenant", tenant,
                "--no-start",
                "--non-interactive",
            ],
            env=cls._env(),
            check=True,
            timeout=180,
        )

    @classmethod
    def _wait_for_tenant_tools(cls, tenant: str, tools: set[str]) -> None:
        deadline = time.monotonic() + 45
        last_error: Exception | None = None
        while time.monotonic() < deadline:
            try:
                with TestbedClient(cls.url, name=f"testbed-ready-{tenant}") as client:
                    client.connect_join(f"testbed-ready-{tenant}", tenant_id=tenant)
                    client.wait_tools(
                        tools,
                        timeout=5,
                        session=f"testbed-ready-{tenant}",
                        tenant_id=tenant,
                    )
                    return
            except Exception as exc:
                last_error = exc
                time.sleep(0.5)
        raise AssertionError(f"tenant {tenant} tools did not become ready: {last_error}")

    def _client(self, tenant: str, session: str = "shared-session") -> TestbedClient:
        client = TestbedClient(self.url, name=f"testbed-{tenant}")
        client.connect_join(session, tenant_id=tenant)
        return client

    def _catalog(self, tenant: str) -> set[str]:
        with self._client(tenant, session=f"catalog-{tenant}") as client:
            return {str(tool.get("name")) for tool in client.tools() if tool.get("name")}

    def _stable_catalog(self, tenant: str) -> set[str]:
        deadline = time.monotonic() + 15
        previous: set[str] | None = None
        while time.monotonic() < deadline:
            current = self._catalog(tenant)
            if current == previous:
                return current
            previous = current
            time.sleep(0.5)
        raise AssertionError(f"tenant {tenant} catalog did not stabilize")

    def _resolved_tenant(self, workspace: Path) -> str:
        script = (
            "from pathlib import Path; "
            "from tabula_distro.agent_cli import _select_tenant; "
            f"home=Path({str(self.home)!r}); "
            "print(_select_tenant(home, None, Path.cwd()))"
        )
        return subprocess.check_output(
            [self._python(), "-c", script],
            cwd=workspace,
            env=self._env(),
            text=True,
            timeout=30,
        ).strip()

    def test_real_tools_catalogs_workspaces_sessions_and_state_are_isolated(self) -> None:
        code_catalog = self._catalog("code-project")
        claw_catalog = self._catalog("claw-project")
        self.assertIn("codegraph_search", code_catalog)
        self.assertNotIn("subagent_list", code_catalog)
        self.assertIn("subagent_list", claw_catalog)
        self.assertNotIn("codegraph_search", claw_catalog)

        for tenant, marker in (
            ("code-project", "code-only.txt"),
            ("claw-project", "claw-only.txt"),
        ):
            with self._client(tenant) as client:
                client.wait_tools(
                    {"fs_list", "todo_read", "todo_write"},
                    session="shared-session",
                    tenant_id=tenant,
                )
                listing = client.call_tool("fs_list", {}, timeout=15).json()
                names = {entry["name"] for entry in listing["entries"]}
                self.assertIn(marker, names)
                self.assertNotIn("claw-only.txt" if tenant == "code-project" else "code-only.txt", names)
                written = client.call_tool(
                    "todo_write",
                    {"items": [{"content": tenant, "status": "in_progress"}]},
                    timeout=15,
                ).json()
                self.assertEqual(written["items"][0]["content"], tenant)

        for tenant in ("code-project", "claw-project"):
            state = self.home / "tenants" / tenant / "state" / "plugins" / "todo" / "shared-session.json"
            self.assertTrue(state.is_file(), state)
            self.assertEqual(json.loads(state.read_text(encoding="utf-8"))["items"][0]["content"], tenant)

        status = json.loads(subprocess.check_output(
            [str(self.home / "bin" / ("tabula.exe" if os.name == "nt" else "tabula")), "status", "--json"],
            env=self._env(),
            text=True,
            timeout=30,
        ))
        attached = [runtime for runtime in status.get("runtimes", []) if runtime.get("attached")]
        self.assertEqual(len(attached), 1, status)
        self.assertTrue({"code-project", "claw-project"}.issubset(set(attached[0].get("tenants_served") or [])), status)

        with self._client("code-project") as code_client, self._client("claw-project") as claw_client:
            code_items = code_client.call_tool("todo_read", {}, timeout=15).json()["items"]
            claw_items = claw_client.call_tool("todo_read", {}, timeout=15).json()["items"]
            self.assertEqual(code_items[0]["content"], "code-project")
            self.assertEqual(claw_items[0]["content"], "claw-project")

    def test_skill_tools_manage_external_skills_and_protect_sandbox(self) -> None:
        runtime_skill = self.home / "skills" / "tabula-guide" / "SKILL.md"
        for tenant, workspace in (
            ("code-immune-project", self.code_immune_workspace),
            ("claw-project", self.claw_workspace),
        ):
            skill_root = workspace / ".agents" / "skills"
            skill_dir = skill_root / "installed-test"
            skill_md = skill_dir / "SKILL.md"
            reference = skill_dir / "references" / "notes.md"
            script = skill_dir / "scripts" / "check.sh"
            content = "---\nname: installed-test\ndescription: Installed skill tool test\n---\n\nOriginal body.\n"
            with self._client(tenant, session=f"skills-{tenant}") as client:
                catalog = {str(tool.get("name")) for tool in client.tools() if tool.get("name")}
                self.assertTrue({"skill_read", "skill_write", "skill_edit", "skill_delete"}.issubset(catalog), catalog)

                runtime = client.call_tool("skill_read", {"path": str(runtime_skill)}, timeout=15)
                self.assertTrue(runtime.ok, runtime.output)
                self.assertIn("name: tabula-guide", runtime.json()["content"])

                written = client.call_tool("skill_write", {"path": str(skill_md), "content": content}, timeout=15)
                self.assertTrue(written.ok, written.output)
                self.assertEqual(skill_md.read_text(encoding="utf-8"), content)

                invalid = client.call_tool("skill_write", {
                    "path": str(skill_root / "wrong-name" / "SKILL.md"),
                    "content": "---\nname: other-name\ndescription: Invalid\n---\n",
                }, timeout=15)
                self.assertFalse(invalid.ok, invalid.output)
                self.assertFalse((skill_root / "wrong-name" / "SKILL.md").exists())

                for path, body, executable in (
                    (reference, "reference body\n", False),
                    (script, "#!/bin/sh\nexit 0\n", True),
                ):
                    result = client.call_tool("skill_write", {
                        "path": str(path), "content": body, "executable": executable,
                    }, timeout=15)
                    self.assertTrue(result.ok, result.output)
                self.assertTrue(script.stat().st_mode & 0o100)

                listing = client.call_tool("skill_read", {"path": str(skill_dir)}, timeout=15)
                self.assertTrue(listing.ok, listing.output)
                self.assertEqual([entry["name"] for entry in listing.json()["entries"]], ["SKILL.md", "references", "scripts"])
                resource = client.call_tool("skill_read", {"path": str(reference)}, timeout=15)
                self.assertEqual(resource.json()["content"], "reference body\n")

                edited = client.call_tool("skill_edit", {
                    "path": str(skill_md), "old_string": "Original body.", "new_string": "Edited body.",
                }, timeout=15)
                self.assertTrue(edited.ok, edited.output)
                self.assertIn("Edited body.", skill_md.read_text(encoding="utf-8"))

                runtime_write = client.call_tool("skill_edit", {
                    "path": str(runtime_skill), "old_string": "name: tabula-guide", "new_string": "name: changed",
                }, timeout=15)
                self.assertFalse(runtime_write.ok, runtime_write.output)
                traversal = client.call_tool("skill_read", {"path": str(skill_dir / ".." / "installed-test" / "SKILL.md")}, timeout=15)
                self.assertFalse(traversal.ok, traversal.output)

                outside = workspace / "outside-skill-test.txt"
                outside.write_text("outside\n", encoding="utf-8")
                link = skill_dir / "outside-link"
                link.symlink_to(outside)
                escaped = client.call_tool("skill_read", {"path": str(link)}, timeout=15)
                self.assertFalse(escaped.ok, escaped.output)

                non_recursive = client.call_tool("skill_delete", {"path": str(skill_dir)}, timeout=15)
                self.assertFalse(non_recursive.ok, non_recursive.output)
                deleted = client.call_tool("skill_delete", {"path": str(skill_dir), "recursive": True}, timeout=15)
                self.assertTrue(deleted.ok, deleted.output)
                self.assertFalse(skill_dir.exists())
                self.assertEqual(outside.read_text(encoding="utf-8"), "outside\n")

    def test_reinstalling_code_distro_does_not_change_claw_tenant(self) -> None:
        claw_root = self.home / "tenants" / "claw-project"
        claw_lock = (claw_root / "install.lock.json").read_bytes()
        claw_plugins = (claw_root / "plugins").resolve()
        claw_state_path = claw_root / "state" / "plugins" / "todo" / "shared-session.json"
        if not claw_state_path.is_file():
            with self._client("claw-project") as client:
                client.call_tool(
                    "todo_write",
                    {"items": [{"content": "claw-project", "status": "in_progress"}]},
                    timeout=15,
                )
        claw_state = claw_state_path.read_bytes()
        before_catalog = self._stable_catalog("claw-project")

        script = (
            "from pathlib import Path; "
            "from tabula_distro import install; "
            "from tabula_distro.cli import _temporary_local_alias_overrides; "
            f"source={self.code_source!r}; home=Path({str(self.home)!r}); "
            "ctx=_temporary_local_alias_overrides(source); ctx.__enter__(); "
            "install.install(source, home, update=True, tenant='code-project', expose_global_boot=False); "
            "ctx.__exit__(None, None, None)"
        )
        subprocess.run(
            [self._python(), "-c", script],
            env=self._env(),
            check=True,
            timeout=180,
        )
        self._wait_for_tenant_tools("code-project", {"fs_list", "todo_read", "codegraph_search"})

        self.assertEqual((claw_root / "install.lock.json").read_bytes(), claw_lock)
        self.assertEqual((claw_root / "plugins").resolve(), claw_plugins)
        self.assertEqual(claw_state_path.read_bytes(), claw_state)
        self._wait_for_tenant_tools("claw-project", before_catalog)
        self.assertEqual(self._stable_catalog("claw-project"), before_catalog)

    def test_tenant_selection_follows_cwd_binding(self) -> None:
        self.assertEqual(self._resolved_tenant(self.code_workspace), "code-project")
        self.assertEqual(self._resolved_tenant(self.claw_workspace), "claw-project")


def main() -> int:
    parser = argparse.ArgumentParser(description="Run installed multi-distro tenant checks")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    MultiDistroTenantsInstalled.url = args.url
    MultiDistroTenantsInstalled.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(
        unittest.defaultTestLoader.loadTestsFromTestCase(MultiDistroTenantsInstalled)
    )
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

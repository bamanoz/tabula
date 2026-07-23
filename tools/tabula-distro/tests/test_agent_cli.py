from __future__ import annotations

import json
import tempfile
import tomllib
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from tabula_distro import agent_cli
from tabula_distro import install as installmod


def _write(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def _make_distro(root: Path, name: str = "demo") -> Path:
    distro = root / name
    _write(
        distro / "distro.toml",
        f'[distro]\nid = "tabula.{name}"\nname = "{name}"\nversion = "0.1.0"\n',
    )
    _write(distro / "templates" / "SYSTEM.md", f"{name}\n")
    return distro


class AgentCliTests(unittest.TestCase):
    def _run(self, argv: list[str]) -> tuple[int, str, str]:
        stdout = StringIO()
        stderr = StringIO()
        with redirect_stdout(stdout), redirect_stderr(stderr):
            code = agent_cli.main(argv)
        return code, stdout.getvalue(), stderr.getvalue()

    def test_install_local_distro_creates_local_tenant_and_binding(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            project = root / "project"
            project.mkdir()
            distro = _make_distro(root)

            with mock.patch.object(agent_cli.secrets, "token_hex", return_value="0123456789ab"):
                code, out, err = self._run([
                    "--home", str(home), "install",
                    "--distro", str(distro),
                    "--bind", str(project), "--no-start",
                ])

            self.assertEqual(code, 0, err)
            self.assertIn("installed tenant agent-0123456789ab", out)
            tenant = home / "tenants" / "agent-0123456789ab"
            self.assertTrue(tenant.is_dir())
            lock = json.loads((tenant / "install.lock.json").read_text(encoding="utf-8"))
            self.assertEqual(lock["distro"]["distro_source"], str(distro.resolve()))
            bindings = tomllib.loads((home / "bindings.toml").read_text(encoding="utf-8"))
            self.assertEqual(bindings["directory"], [{
                "root": str(project.resolve()),
                "tenant": "agent-0123456789ab",
            }])
            self.assertFalse((project / "tabula.agent.toml").exists())

    def test_install_reuses_binding_for_same_source(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            project = root / "project"
            project.mkdir()
            distro = _make_distro(root)
            args = [
                "--home", str(home), "install",
                "--distro", str(distro),
                "--bind", str(project), "--no-start",
            ]

            with mock.patch.object(agent_cli.secrets, "token_hex", return_value="111111111111"):
                self.assertEqual(self._run(args)[0], 0)
            code, out, err = self._run(args)

            self.assertEqual(code, 0, err)
            self.assertIn("using existing tenant agent-111111111111", out)
            self.assertEqual(
                [entry.name for entry in (home / "tenants").iterdir()],
                ["agent-111111111111"],
            )

    def test_install_conflict_requires_replacement_and_preserves_old_tenant(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            project = root / "project"
            project.mkdir()
            first = _make_distro(root / "first", "first")
            second = _make_distro(root / "second", "second")
            common = ["--home", str(home), "install", "--bind", str(project), "--no-start"]

            with mock.patch.object(agent_cli.secrets, "token_hex", return_value="222222222222"):
                self.assertEqual(self._run([*common, "--distro", str(first)])[0], 0)

            code, _out, err = self._run([*common, "--distro", str(second)])
            self.assertEqual(code, 1)
            self.assertIn("--replace-binding", err)
            self.assertFalse((home / "tenants" / "agent-333333333333").exists())

            with mock.patch.object(agent_cli.secrets, "token_hex", return_value="333333333333"):
                code, _out, err = self._run([
                    *common, "--distro", str(second), "--replace-binding",
                ])
            self.assertEqual(code, 0, err)
            self.assertTrue((home / "tenants" / "agent-222222222222").is_dir())
            self.assertTrue((home / "tenants" / "agent-333333333333").is_dir())
            bindings = tomllib.loads((home / "bindings.toml").read_text(encoding="utf-8"))
            self.assertEqual(bindings["directory"][0]["tenant"], "agent-333333333333")

    def test_install_git_uri_records_source_and_resolved_sha(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            project = root / "project"
            project.mkdir()
            distro = _make_distro(root)
            source = "git+https://example.invalid/distros.git@main#path=demo"
            sha = "a" * 40

            class FakeCheckout:
                worktree = root

                def __init__(self) -> None:
                    self.sha = sha

            class FakeGitCache:
                def __init__(self, _root: Path):
                    pass

                def fetch(self, _source, *, offline: bool = False):
                    return FakeCheckout()

            with (
                mock.patch.object(installmod, "GitCache", FakeGitCache),
                mock.patch.object(agent_cli.secrets, "token_hex", return_value="444444444444"),
            ):
                code, _out, err = self._run([
                    "--home", str(home), "install",
                    "--distro", source,
                    "--bind", str(project), "--no-start",
                ])

            self.assertEqual(code, 0, err)
            lock = json.loads(
                (home / "tenants" / "agent-444444444444" / "install.lock.json").read_text(encoding="utf-8")
            )
            self.assertEqual(lock["distro"]["distro_source"], source)
            self.assertEqual(lock["distro"]["distro_resolved_sha"], sha)

    def test_install_defaults_to_cwd_and_starts_service(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            project = root / "project"
            project.mkdir()
            distro = _make_distro(root)
            _write(home / "config" / "kernel.toml", '[kernel]\nurl = "ws://127.0.0.1:8089/ws"\n')

            with (
                mock.patch.object(agent_cli.Path, "cwd", return_value=project),
                mock.patch.object(agent_cli.secrets, "token_hex", return_value="888888888888"),
                mock.patch.object(agent_cli, "_ensure_ready") as ready,
            ):
                code, out, err = self._run([
                    "--home", str(home), "install", "--distro", str(distro),
                    "--non-interactive",
                ])

            self.assertEqual(code, 0, err)
            self.assertIn("tenant agent-888888888888 ready", out)
            binding = tomllib.loads((home / "bindings.toml").read_text(encoding="utf-8"))
            self.assertEqual(binding["directory"], [{
                "root": str(project.resolve()),
                "tenant": "agent-888888888888",
            }])
            ready.assert_called_once_with(
                home.resolve(), "agent-888888888888", "ws://localhost:8089/ws", 30.0
            )

    def test_install_default_with_explicit_tenant_is_idempotent(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            project = root / "project"
            project.mkdir()
            distro = _make_distro(root)
            args = [
                "--home", str(home), "install", "--distro", str(distro),
                "--tenant", "agent-explicit", "--default", "--no-start",
            ]

            with mock.patch.object(agent_cli.Path, "cwd", return_value=project):
                self.assertEqual(self._run(args)[0], 0)
                code, out, err = self._run(args)

            self.assertEqual(code, 0, err)
            self.assertIn("using existing tenant agent-explicit", out)
            bindings = tomllib.loads((home / "bindings.toml").read_text(encoding="utf-8"))
            self.assertEqual(bindings["default"]["tenant"], "agent-explicit")
            self.assertNotIn("directory", bindings)
            self.assertEqual(
                [entry.name for entry in (home / "tenants").iterdir()],
                ["agent-explicit"],
            )

    def test_install_update_refreshes_existing_same_source_tenant(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            project = root / "project"
            project.mkdir()
            distro = _make_distro(root)
            args = [
                "--home", str(home), "install", "--distro", str(distro),
                "--tenant", "agent-explicit", "--bind", str(project), "--no-start",
            ]
            self.assertEqual(self._run(args)[0], 0)
            result = SimpleNamespace(
                distro=SimpleNamespace(
                    lock=SimpleNamespace(distro="demo"),
                    generation=SimpleNamespace(name="0002-refresh"),
                )
            )

            with mock.patch.object(agent_cli.tenant_materializer, "refresh", return_value=result) as refresh:
                code, out, err = self._run([*args, "--update"])

            self.assertEqual(code, 0, err)
            self.assertIn("updated existing tenant agent-explicit", out)
            refresh.assert_called_once_with(
                str(distro.resolve()),
                home.resolve(),
                "agent-explicit",
                project.resolve(),
                None,
                offline=False,
                update=True,
                keep_generations=5,
            )

    def test_install_no_start_does_not_read_kernel_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            project = root / "project"
            project.mkdir()
            distro = _make_distro(root)

            with (
                mock.patch.object(agent_cli.Path, "cwd", return_value=project),
                mock.patch.object(agent_cli.secrets, "token_hex", return_value="999999999999"),
                mock.patch.object(agent_cli, "_start_installed_tenant") as start,
            ):
                code, _out, err = self._run([
                    "--home", str(home), "install", "--distro", str(distro), "--no-start",
                ])

            self.assertEqual(code, 0, err)
            start.assert_not_called()

    def test_tenant_id_generation_skips_existing_id(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            (home / "tenants" / "agent-aaaaaaaaaaaa").mkdir(parents=True)
            with mock.patch.object(
                agent_cli.secrets,
                "token_hex",
                side_effect=["aaaaaaaaaaaa", "bbbbbbbbbbbb"],
            ):
                self.assertEqual(agent_cli._new_tenant_id(home), "agent-bbbbbbbbbbbb")

    def test_start_resolves_binding_and_ensures_tenant_ready(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            project = root / "project"
            nested = project / "src"
            nested.mkdir(parents=True)
            distro = _make_distro(root)
            args = [
                "--home", str(home), "install",
                "--distro", str(distro),
                "--bind", str(project), "--no-start",
            ]
            with mock.patch.object(agent_cli.secrets, "token_hex", return_value="555555555555"):
                self.assertEqual(self._run(args)[0], 0)
            _write(home / "config" / "kernel.toml", '[kernel]\nurl = "ws://127.0.0.1:8089/ws"\n')

            with (
                mock.patch.object(agent_cli.Path, "cwd", return_value=nested),
                mock.patch.object(agent_cli, "_ensure_ready") as ready,
            ):
                code, out, err = self._run(["--home", str(home)])

            self.assertEqual(code, 0, err)
            self.assertEqual(out, "tenant agent-555555555555 ready\n")
            ready.assert_called_once_with(
                home.resolve(), "agent-555555555555", "ws://127.0.0.1:8089/ws", 30.0
            )

    def test_start_explicit_tenant_bypasses_binding(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            project = root / "project"
            project.mkdir()
            distro = _make_distro(root)
            with mock.patch.object(agent_cli.secrets, "token_hex", return_value="666666666666"):
                self.assertEqual(self._run([
                    "--home", str(home), "install", "--distro", str(distro), "--bind", str(project), "--no-start",
                ])[0], 0)
            _write(home / "config" / "kernel.toml", '[kernel]\nurl = "ws://127.0.0.1:8089/ws"\n')

            with mock.patch.object(agent_cli, "_ensure_ready") as ready:
                code, out, err = self._run([
                    "--home", str(home), "--tenant", "agent-666666666666",
                ])

            self.assertEqual(code, 0, err)
            self.assertEqual(out, "tenant agent-666666666666 ready\n")
            ready.assert_called_once_with(
                home.resolve(), "agent-666666666666", "ws://127.0.0.1:8089/ws", 30.0
            )

    def test_launch_without_binding_has_one_actionable_error(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            code, _out, err = self._run(["--home", str(root / "home")])

            self.assertEqual(code, 1)
            self.assertIn("tabula-agent install --distro <source> --bind", err)
            self.assertIn("--tenant <id>", err)
            self.assertEqual(err.count("tabula-agent:"), 1)

    def test_ensure_ready_reuses_healthy_service(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            with (
                mock.patch.object(agent_cli.service_runtime, "kernel_healthy", return_value=True),
                mock.patch.object(agent_cli.service_runtime, "request_runtime_reload", return_value="") as reload_runtime,
                mock.patch.object(agent_cli.service_runtime, "wait_for_runtime_ready", return_value=True) as wait_ready,
                mock.patch.object(agent_cli.subprocess, "Popen") as popen,
            ):
                agent_cli._ensure_ready(home, "tenant-a", "ws://127.0.0.1:8089/ws", 3.0)

            reload_runtime.assert_called_once()
            wait_ready.assert_called_once()
            popen.assert_not_called()

    def test_ensure_ready_reports_unready_tenant(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            with (
                mock.patch.object(agent_cli.service_runtime, "kernel_healthy", return_value=True),
                mock.patch.object(agent_cli.service_runtime, "request_runtime_reload", return_value="reload denied"),
                mock.patch.object(agent_cli.service_runtime, "wait_for_runtime_ready", return_value=False),
            ):
                with self.assertRaisesRegex(agent_cli.AgentError, "runtime is not ready.*reload denied"):
                    agent_cli._ensure_ready(home, "tenant-a", "ws://127.0.0.1:8089/ws", 3.0)

    def test_install_treats_missing_tenant_lock_as_conflict(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            project = root / "project"
            project.mkdir()
            distro = _make_distro(root)
            _write(
                home / "bindings.toml",
                f'[[directory]]\nroot = "{project.resolve()}"\ntenant = "broken"\n',
            )

            code, _out, err = self._run([
                "--home", str(home), "install",
                "--distro", str(distro),
                "--bind", str(project), "--no-start",
            ])

            self.assertEqual(code, 1)
            self.assertIn("has no valid tenant install lock", err)
            self.assertIn("--replace-binding", err)


if __name__ == "__main__":
    unittest.main()

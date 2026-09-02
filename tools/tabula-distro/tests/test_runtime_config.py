from __future__ import annotations

import os
import tempfile
import unittest
import sys
from pathlib import Path
from unittest import mock

import tomllib

from tabula_distro import runtime_config


class RuntimeConfigWriteTests(unittest.TestCase):
    def test_initial_write_produces_valid_toml(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            path = runtime_config.write(home, ["/p/a", "/p/b"])
            data = tomllib.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(data["plugin_dirs"], ["/p/a", "/p/b"])
            self.assertEqual(data["skill_dirs"], [str(home / "skills")])
            self.assertEqual(len(data["kernel"]), 1)
            self.assertEqual(data["kernel"][0]["id"], "main")
            self.assertEqual(data["kernel"][0]["tenants"], ["*"])
            self.assertEqual(data["pool"]["cold_workers_per_tenant_max"], 16)
            self.assertEqual(data["runtimes"]["python"]["command"], [str(Path(sys.executable).absolute())])

    @unittest.skipIf(os.name == "nt", "creating symlinks requires elevated privileges on Windows")
    def test_python_runtime_preserves_virtualenv_symlink(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            base_python = root / "base-python"
            base_python.touch()
            venv_python = root / "venv" / "bin" / "python"
            venv_python.parent.mkdir(parents=True)
            venv_python.symlink_to(base_python)
            doc: dict = {}

            runtime_config.write_python_runtime(doc, executable=venv_python)

            self.assertEqual(doc["runtimes"]["python"]["command"], [str(venv_python.absolute())])

    def test_rewrite_preserves_user_comments_and_unknown_sections(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            path = runtime_config.write(home, ["/p/a"])
            # User adds comments and a section we do not own.
            existing = path.read_text(encoding="utf-8")
            doctored = (
                "# user-owned: keep me\n"
                "# trailing trivia\n"
                + existing
                + "\n[my_custom]\nflag = true\n"
            )
            path.write_text(doctored, encoding="utf-8")

            # Reinstall with a different plugin list.
            runtime_config.write(home, ["/p/a", "/p/b"])

            result = path.read_text(encoding="utf-8")
            self.assertIn("# user-owned: keep me", result)
            self.assertIn("# trailing trivia", result)
            self.assertIn("[my_custom]", result)
            self.assertIn("flag = true", result)

            data = tomllib.loads(result)
            self.assertEqual(data["plugin_dirs"], ["/p/a", "/p/b"])
            self.assertTrue(data["my_custom"]["flag"])

    def test_rewrite_preserves_other_runtime_commands(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            path = runtime_config.write(home, ["/p/a"])
            path.write_text(path.read_text(encoding="utf-8") + '\n[runtimes.node]\ncommand = ["node", "--no-warnings"]\n', encoding="utf-8")

            runtime_config.write(home, ["/p/a"])

            data = tomllib.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(data["runtimes"]["node"]["command"], ["node", "--no-warnings"])
            self.assertEqual(data["runtimes"]["python"]["command"], [str(Path(sys.executable).absolute())])

    def test_rewrite_updates_owned_keys(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            runtime_config.write(home, ["/old"])
            path = runtime_config.write(home, ["/new1", "/new2"])
            data = tomllib.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(data["plugin_dirs"], ["/new1", "/new2"])
            # Still exactly one kernel entry, not appended.
            self.assertEqual(len(data["kernel"]), 1)

    def test_default_plugin_dir_when_payload_empty(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            path = runtime_config.write(home, [str(home / "plugins")])
            data = tomllib.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(data["plugin_dirs"], [str(home / "plugins")])

    def test_runtime_socket_environment_override(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            socket_path = Path(tmp) / "runtime" / "runtime.sock"
            with mock.patch.dict(os.environ, {"TABULA_RUNTIME_SOCKET_PATH": str(socket_path)}):
                path = runtime_config.write(home, [str(home / "plugins")])

            data = tomllib.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(data["kernel"][0]["url"], "unix://" + str(socket_path))

    def test_kernel_config_write_contains_only_kernel_transport(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            path = runtime_config.write_kernel_config(home, url="ws://127.0.0.1:9090/ws")
            data = tomllib.loads(path.read_text(encoding="utf-8"))

            self.assertEqual(data["kernel"], {"url": "ws://127.0.0.1:9090/ws"})
            self.assertEqual(data["runtime_wss"], {"enabled": False})
            self.assertNotIn("meta", data)
            self.assertNotIn("workspace", data)
            self.assertNotIn("default_provider", data)

    def test_kernel_config_rewrite_preserves_unknown_sections(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            path = runtime_config.write_kernel_config(home, url="ws://127.0.0.1:8089/ws")
            path.write_text(path.read_text(encoding="utf-8") + "\n[custom]\nflag = true\n", encoding="utf-8")

            runtime_config.write_kernel_config(home, url="ws://127.0.0.1:9090/ws")
            data = tomllib.loads(path.read_text(encoding="utf-8"))

            self.assertEqual(data["kernel"]["url"], "ws://127.0.0.1:9090/ws")
            self.assertTrue(data["custom"]["flag"])

    def test_sync_for_distro_uses_installed_layout_without_boot(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            installed = home / "distrib" / "demo"
            (installed / "plugins").mkdir(parents=True)

            path = runtime_config.sync_for_distro(home, installed)
            data = tomllib.loads(path.read_text(encoding="utf-8"))

            self.assertEqual(data["plugin_dirs"], [str((installed / "plugins").resolve())])
            self.assertEqual(data["distro"], {"active": "demo", "dir": str(installed.resolve())})
            kernel = tomllib.loads((home / "config" / "kernel.toml").read_text(encoding="utf-8"))
            self.assertEqual(kernel["kernel"], {"url": "ws://localhost:8089/ws"})

    def test_sync_tenant_rebuilds_all_installed_tenant_surfaces(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            default = home / "tenants" / "default"
            recovery = home / "tenants" / "recovery"
            project = home / "tenants" / "project"
            default.mkdir(parents=True)
            recovery.mkdir(parents=True)
            project.mkdir(parents=True)
            runtime_config.write(home, [str(home / "plugins")])

            runtime_config.sync_tenant(home, "recovery", recovery)
            path = runtime_config.sync_tenant(home, "project", project)
            data = tomllib.loads(path.read_text(encoding="utf-8"))

            tenants = {item["id"]: item for item in data["tenant"]}
            self.assertEqual(set(tenants), {"default", "project", "recovery"})
            self.assertEqual(tenants["default"]["plugin_dirs"], [str(home / "plugins")])
            self.assertEqual(tenants["default"]["skill_dirs"], [str(home / "skills")])
            self.assertEqual(tenants["recovery"]["plugin_dirs"], [str(recovery / "plugins")])
            self.assertEqual(tenants["project"]["plugin_dirs"], [str(project / "plugins")])
            self.assertEqual(set(data["kernel"][0]["tenants"]), {"default", "project", "recovery"})

    def test_sync_tenant_discovers_materialized_app_tenant_surfaces(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            installed = home / "tenants" / "installed"
            app = home / "tenants" / "app"
            later = home / "tenants" / "later"
            for tenant in (installed, app, later):
                tenant.mkdir(parents=True)
            (installed / "install.lock.json").write_text("{}", encoding="utf-8")
            (app / "app.lock.json").write_text("{}", encoding="utf-8")
            (app / "plugins").mkdir()
            (app / "skills").mkdir()
            runtime_config.write(home, [str(home / "plugins")])

            runtime_config.sync_tenant(home, "installed", installed)
            path = runtime_config.sync_tenant(home, "later", later)
            data = tomllib.loads(path.read_text(encoding="utf-8"))

            tenants = {item["id"]: item for item in data["tenant"]}
            self.assertEqual(tenants["app"]["plugin_dirs"], [str(app / "plugins")])
            self.assertEqual(tenants["app"]["skill_dirs"], [str(app / "skills")])


if __name__ == "__main__":
    unittest.main()

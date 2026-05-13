#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import subprocess
import time
import tomllib
import unittest

from tabula_testbed import TestbedClient


class AppManifestInstalledLayout(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    @classmethod
    def app_project(cls) -> Path:
        project = Path(cls.tabula_home) / "app-project"
        project.mkdir(parents=True, exist_ok=True)
        return project

    @classmethod
    def tabula_install_bin(cls) -> str:
        for candidate in (
            Path(cls.tabula_home) / "bin" / "tabula-install",
            Path(cls.tabula_home) / ".venv" / "bin" / "tabula-install",
        ):
            if candidate.is_file():
                return str(candidate)
        return "tabula-install"

    def test_app_run_dry_run_materializes_tenant_layout(self) -> None:
        home = Path(self.tabula_home)
        project = self.app_project()
        manifest_path = project / "tabula.app.toml"
        manifest_path.write_text(self.app_manifest(home, project), encoding="utf-8")

        env = os.environ.copy()
        env["TABULA_HOME"] = str(home)
        result = subprocess.run(
            [self.tabula_install_bin(), "--home", str(home), "app", "run", str(manifest_path), "--dry-run"],
            env=env,
            cwd=project,
            check=True,
            timeout=60,
            text=True,
            capture_output=True,
        )
        self.assertIn("run plan: app=testbed-app", result.stdout)

        tenant_dir = home / "tenants" / "testbed-app"
        self.assertTrue((tenant_dir / "app.lock.json").is_file())
        self.assertTrue((tenant_dir / "plugins" / "fs" / "plugin.toml").is_file())
        self.assertTrue((tenant_dir / "plugins" / "exec" / "plugin.toml").is_file())
        self.assertTrue((tenant_dir / "skills").is_dir())
        self.assertIn("phase=run", (tenant_dir / "config" / "materializer.txt").read_text(encoding="utf-8"))
        self.assertIn("dry_run=1", (tenant_dir / "config" / "materializer.txt").read_text(encoding="utf-8"))

        lock = json.loads((tenant_dir / "app.lock.json").read_text(encoding="utf-8"))
        self.assertEqual(lock["application"]["id"], "testbed-app")

        runtime_cfg = tomllib.loads((home / "config" / "runtime.toml").read_text(encoding="utf-8"))
        self.assertEqual(runtime_cfg["plugin_dirs"], [])
        self.assertEqual(runtime_cfg["skill_dirs"], [])
        self.assertEqual(runtime_cfg["tenant"][0]["id"], "testbed-app")
        self.assertEqual([Path(item).resolve() for item in runtime_cfg["tenant"][0]["plugin_dirs"]], [(tenant_dir / "plugins").resolve()])
        self.assertEqual([Path(item).resolve() for item in runtime_cfg["tenant"][0]["skill_dirs"]], [(tenant_dir / "skills").resolve()])
        self.assertEqual(runtime_cfg["kernel"][0]["tenants"], ["testbed-app"])

        bindings = tomllib.loads((home / "app-bindings.toml").read_text(encoding="utf-8"))
        directories = bindings.get("directory", [])
        self.assertTrue(
            any(
                item["app"] == "testbed-app"
                and Path(item["root"]).resolve() == project.resolve()
                for item in directories
            )
        )

    def test_app_run_reuses_kernel_and_executes_installed_plugins_for_app_tenant(self) -> None:
        home = Path(self.tabula_home)
        project = self.app_project()
        manifest_path = project / "tabula.app.toml"
        manifest_path.write_text(self.app_manifest(home, project), encoding="utf-8")

        env = os.environ.copy()
        env["TABULA_HOME"] = str(home)
        env["TABULA_PRESERVE_RUNTIME_CONFIG"] = "1"
        result = subprocess.run(
            [
                self.tabula_install_bin(),
                "--home",
                str(home),
                "app",
                "run",
                str(manifest_path),
                "--tabula-bin",
                str(home / "bin" / "tabula"),
            ],
            env=env,
            cwd=project,
            timeout=60,
            text=True,
            capture_output=True,
        )
        self.assertEqual(result.returncode, 0, result.stderr or result.stdout)
        materializer = home / "tenants" / "testbed-app" / "config" / "materializer.txt"
        materializer_text = materializer.read_text(encoding="utf-8")
        self.assertIn("phase=run", materializer_text)
        self.assertIn("dry_run=0", materializer_text)
        time.sleep(2.5)
        self.assert_app_claw_ready(home, project)

        with TestbedClient(self.url, name="testbed-app-manifest") as client:
            client.connect_join("testbed-app-manifest-tools", tenant_id="testbed-app")
            client.wait_tools({"fs_read", "fs_write", "exec_run"}, session="testbed-app-manifest", tenant_id="testbed-app")

            note = project / "app-note.txt"
            client.call_tool("fs_write", {"path": str(note), "content": "app tenant"}, timeout=10).json()
            self.assertEqual(client.call_tool("fs_read", {"path": str(note)}, timeout=10).json()["content"], "app tenant")

            exec_result = client.call_tool("exec_run", {"command": "pwd; printf :$TABULA_TENANT_ID"}, timeout=10).json()
            self.assertEqual(Path(exec_result["stdout"].splitlines()[0]).resolve(), project.resolve())
            self.assertTrue(exec_result["stdout"].endswith(":testbed-app"))

    def assert_app_claw_ready(self, home: Path, project: Path) -> None:
        candidate = home / "bin" / "tabula-cli"
        tabula_cli = str(candidate) if candidate.is_file() else "tabula-cli"
        env = os.environ.copy()
        env["TABULA_HOME"] = str(home)
        env["TABULA_URL"] = self.url
        subprocess.run(
            [tabula_cli, "--expected-distro-id", "tabula.claw", "--app", "testbed-app", "--kernel", "testbed-app", "--help"],
            env=env,
            cwd=project,
            check=True,
            timeout=10,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )

    def app_manifest(self, home: Path, project: Path) -> str:
        source = "local:" + str(home / "generated-testbed")
        return f'''[application]
id = "testbed-app"
name = "Testbed App"

[distro]
id = "tabula.testbed"
name = "testbed"
source = {source!r}

[kernel]
mode = "managed"
id = "testbed-app"
url = {self.url!r}

[[runtimes]]
id = "local"
mode = "managed"
tenants = ["testbed-app"]

[runtimes.exec]
backend = "bare"

[[bindings.directory]]
root = {str(project)!r}
app = "testbed-app"
kernel = "testbed-app"

[values.workspace]
path = {str(project)!r}
'''


def main() -> int:
    parser = argparse.ArgumentParser(description="Run app manifest installed-layout testbed checks")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", required=True)
    args = parser.parse_args()
    AppManifestInstalledLayout.url = args.url
    AppManifestInstalledLayout.tabula_home = args.home
    result = unittest.main(argv=["test_app_manifest"], exit=False)
    return 0 if result.result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

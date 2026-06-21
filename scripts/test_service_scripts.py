#!/usr/bin/env python3
from __future__ import annotations

import os
import subprocess
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[1]
INSTALL = ROOT / "scripts" / "install-service.sh"
UNINSTALL = ROOT / "scripts" / "uninstall-service.sh"


class ServiceScriptTests(unittest.TestCase):
    def make_shim(self, *, uname: str = "Linux", id_u: str = "1000", status_running: bool = True) -> tuple[Path, dict[str, str]]:
        shim = Path(self.tempdir) / "bin"
        shim.mkdir(parents=True, exist_ok=True)
        log = Path(self.tempdir) / "calls.log"
        (shim / "uname").write_text(f"#!/bin/sh\necho {uname}\n", encoding="utf-8")
        (shim / "id").write_text(f"#!/bin/sh\nif [ \"$1\" = \"-u\" ]; then echo {id_u}; else /usr/bin/id \"$@\"; fi\n", encoding="utf-8")
        for name in ("launchctl", "systemctl"):
            (shim / name).write_text(f"#!/bin/sh\necho {name} \"$@\" >> {log}\nexit 0\n", encoding="utf-8")
        tabula_status = '{"kernel":{"running": true}}' if status_running else '{"kernel":{"running": false}}'
        (shim / "tabula").write_text(f"#!/bin/sh\necho tabula \"$@\" >> {log}\necho '{tabula_status}'\n", encoding="utf-8")
        for path in shim.iterdir():
            path.chmod(0o755)
        env = os.environ.copy()
        env["PATH"] = f"{shim}:{env['PATH']}"
        env["HOME"] = str(Path(self.tempdir) / "user-home")
        env["TABULA_HOME"] = str(Path(self.tempdir) / "home")
        env["TABULA_BIN"] = str(shim / "tabula")
        return log, env

    def render(self, *, uname: str = "Linux", id_u: str = "1000", args: list[str] | None = None) -> str:
        _, env = self.make_shim(uname=uname, id_u=id_u)
        result = subprocess.run([str(INSTALL), "--render", *(args or [])], env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True)
        return result.stdout

    def setUp(self) -> None:
        self.temp = __import__("tempfile").TemporaryDirectory()
        self.tempdir = self.temp.name

    def tearDown(self) -> None:
        self.temp.cleanup()

    def test_systemd_user_unit_fidelity(self) -> None:
        unit = self.render(uname="Linux")
        self.assertIn("ExecStart=" + str(Path(self.tempdir) / "bin" / "tabula") + " serve --foreground", unit)
        self.assertIn("Restart=on-failure", unit)
        self.assertIn("RestartSec=5s", unit)
        self.assertIn("Environment=TABULA_HOME=" + str(Path(self.tempdir) / "home"), unit)
        self.assertNotIn("Restart=always", unit)

    def test_launchd_plist_fidelity(self) -> None:
        plist = self.render(uname="Darwin")
        self.assertIn("<string>ai.tabula.kernel</string>", plist)
        self.assertIn("<string>serve</string>", plist)
        self.assertIn("<string>--foreground</string>", plist)
        self.assertIn("<key>RunAtLoad</key>", plist)
        self.assertIn("<key>KeepAlive</key>", plist)
        self.assertIn("<key>SuccessfulExit</key>", plist)
        self.assertIn("<false/>", plist)
        self.assertIn("logs/kernel.log", plist)

    def test_linux_system_mode_requires_root(self) -> None:
        shim = Path(self.tempdir) / "bin"
        shim.mkdir(parents=True, exist_ok=True)
        (shim / "uname").write_text("#!/bin/sh\necho Linux\n", encoding="utf-8")
        (shim / "id").write_text("#!/bin/sh\nif [ \"$1\" = \"-u\" ]; then echo 1000; else /usr/bin/id \"$@\"; fi\n", encoding="utf-8")
        for path in shim.iterdir():
            path.chmod(0o755)
        env = os.environ.copy()
        env["PATH"] = f"{shim}:{env['PATH']}"
        result = subprocess.run([str(INSTALL), "--system", "--dry-run"], env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires root", result.stderr)

    def test_uninstall_dry_run_is_idempotent(self) -> None:
        _, env = self.make_shim(uname="Linux")
        result = subprocess.run([str(UNINSTALL), "--dry-run"], env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True)
        self.assertIn("tabula-kernel.service", result.stdout)

    def test_linux_user_install_and_uninstall_orchestrate_systemctl(self) -> None:
        log, env = self.make_shim(uname="Linux")
        home = Path(env["TABULA_HOME"])
        result = subprocess.run([str(INSTALL)], env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True)
        unit = Path(env["HOME"]) / ".config" / "systemd" / "user" / "tabula-kernel.service"
        self.assertTrue(unit.is_file(), result.stdout + result.stderr)
        calls = log.read_text(encoding="utf-8")
        self.assertIn("systemctl --user daemon-reload", calls)
        self.assertIn("systemctl --user enable --now tabula-kernel.service", calls)
        self.assertIn("tabula status --json", calls)

        result = subprocess.run([str(UNINSTALL)], env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True)
        self.assertFalse(unit.exists(), result.stdout + result.stderr)
        calls = log.read_text(encoding="utf-8")
        self.assertIn("systemctl --user disable --now tabula-kernel.service", calls)

    def test_launchd_install_and_uninstall_orchestrate_launchctl(self) -> None:
        log, env = self.make_shim(uname="Darwin")
        home = Path(self.tempdir)
        env["HOME"] = str(home)
        result = subprocess.run([str(INSTALL)], env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True)
        plist = home / "Library" / "LaunchAgents" / "ai.tabula.kernel.plist"
        self.assertTrue(plist.is_file(), result.stdout + result.stderr)
        calls = log.read_text(encoding="utf-8")
        self.assertIn("launchctl load", calls)
        self.assertIn("tabula status --json", calls)

        result = subprocess.run([str(UNINSTALL)], env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True)
        self.assertFalse(plist.exists(), result.stdout + result.stderr)
        calls = log.read_text(encoding="utf-8")
        self.assertIn("launchctl unload", calls)

    @unittest.skipUnless(os.environ.get("TABULA_SERVICE_INTEGRATION") == "1", "set TABULA_SERVICE_INTEGRATION=1 to run host service-manager integration")
    def test_host_service_manager_integration_hook(self) -> None:
        result = subprocess.run([str(INSTALL), "--dry-run"], text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True)
        self.assertTrue(result.stdout.strip())


if __name__ == "__main__":
    unittest.main()

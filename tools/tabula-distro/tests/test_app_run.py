from __future__ import annotations

import json
import os
import subprocess
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path
from unittest import mock

from tabula_distro import app_manifest as appmod
from tabula_distro import app_run as runmod
from tabula_distro import cli as climod


def _write(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def _make_distro(root: Path) -> None:
    distro = root / "claw"
    _write(distro / "distro.toml", '[distro]\nid = "tabula.claw"\nname = "claw"\n')
    (distro / "templates").mkdir(parents=True, exist_ok=True)


def _manifest(root: Path, *, kernel_mode: str = "managed", runtime_mode: str = "managed", backend: str = "bare") -> str:
    return f'''
[application]
id = "claw-tabula"

[distro]
source = "local:{root / 'claw'}"

[kernel]
mode = "{kernel_mode}"
url = "ws://127.0.0.1:65530/ws"

[[runtimes]]
mode = "{runtime_mode}"

[runtimes.exec]
backend = "{backend}"
'''


class AppRunTests(unittest.TestCase):
    def _run_cli(self, argv: list[str]) -> tuple[int, str, str]:
        out = StringIO()
        err = StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            code = climod.main(argv)
        return code, out.getvalue(), err.getvalue()

    def test_plan_maps_runtime_mode_and_backend(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            _make_distro(root)
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest(root))
            manifest = appmod.load(manifest_path, tabula_home=root / "home")

            plan = runmod.plan(manifest)

            self.assertEqual(plan.app_id, "claw-tabula")
            self.assertEqual(plan.runtime_mode, "managed")
            self.assertEqual(plan.execution_backends, ("bare",))

    def test_dry_run_materializes_without_starting_kernel(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_distro(root)
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest(root))

            code, out, err = self._run_cli(["--home", str(home), "app", "run", str(manifest_path), "--dry-run"])

            self.assertEqual(code, 0, err)
            self.assertIn("run plan: app=claw-tabula", out)
            self.assertTrue((home / "tenants" / "claw-tabula" / "app.lock.json").is_file())
            runtime_cfg = (home / "config" / "runtime.toml").read_text(encoding="utf-8")
            self.assertIn('[[tenant]]', runtime_cfg)
            self.assertIn('id = "claw-tabula"', runtime_cfg)
            self.assertIn(str(home / "tenants" / "claw-tabula" / "plugins"), runtime_cfg)
            self.assertIn(str(home / "run" / "runtime-token"), runtime_cfg)
            self.assertIn('tenants = ["claw-tabula"]', runtime_cfg)
            self.assertIn('[distro]', runtime_cfg)
            self.assertIn('active = "claw"', runtime_cfg)

    def test_dry_run_refreshes_stale_distro_generation_in_runtime_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_distro(root)
            stale = home / "distrib" / "claw" / "generations" / "0001-stale"
            _write(home / "config" / "runtime.toml", f'''
plugin_dirs = []
skill_dirs = []

[distro]
active = "claw"
dir = "{stale}"
''')
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest(root))

            code, _out, err = self._run_cli(["--home", str(home), "app", "run", str(manifest_path), "--dry-run", "--update"])

            self.assertEqual(code, 0, err)
            runtime_cfg = (home / "config" / "runtime.toml").read_text(encoding="utf-8")
            self.assertIn('[distro]', runtime_cfg)
            self.assertIn('active = "claw"', runtime_cfg)
            self.assertNotIn("0001-stale", runtime_cfg)
            self.assertIn(str(home / "distrib" / "claw" / "generations"), runtime_cfg)

    def test_run_uses_installed_tabula_bin_by_default(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_distro(root)
            _write(home / "bin" / "tabula", "#!/bin/sh\n")
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest(root))

            with mock.patch.object(runmod, "execute", return_value=runmod.RunResult(plan=runmod.RunPlan(
                app_id="claw-tabula",
                kernel_id="claw-tabula",
                kernel_mode="managed",
                kernel_url="ws://127.0.0.1:65530/ws",
                runtime_mode="managed",
                runtime_ids=("local",),
                execution_backends=("bare",),
            ))) as execute:
                code, _out, err = self._run_cli(["--home", str(home), "app", "run", str(manifest_path)])

            self.assertEqual(code, 0, err)
            self.assertEqual(Path(execute.call_args.kwargs["tabula_bin"]).resolve(), (home / "bin" / "tabula").resolve())

    def test_missing_installed_runner_reports_actionable_error(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_distro(root)
            _write(home / "bin" / "tabula", "#!/bin/sh\n")
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest(root))

            code, _out, err = self._run_cli(["--home", str(home), "app", "run", str(manifest_path)])

            self.assertEqual(code, 1)
            self.assertIn("tabula-runner not found", err)
            self.assertIn("reinstall Tabula", err)

    def test_prepare_materializes_without_starting_kernel(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_distro(root)
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest(root))

            with mock.patch.object(runmod, "execute") as execute:
                code, out, err = self._run_cli(["--home", str(home), "app", "prepare", str(manifest_path)])

            self.assertEqual(code, 0, err)
            execute.assert_not_called()
            self.assertIn("prepared app claw-tabula", out)
            self.assertIn("run plan: app=claw-tabula", out)
            self.assertTrue((home / "tenants" / "claw-tabula" / "app.lock.json").is_file())
            runtime_cfg = (home / "config" / "runtime.toml").read_text(encoding="utf-8")
            self.assertIn('id = "claw-tabula"', runtime_cfg)
            self.assertIn(str(home / "tenants" / "claw-tabula" / "plugins"), runtime_cfg)

    def test_install_global_sets_default_binding_without_launching(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_distro(root)
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest(root))

            with mock.patch.object(runmod, "execute") as execute:
                code, out, err = self._run_cli(["--home", str(home), "app", "install", str(manifest_path), "--global"])

            self.assertEqual(code, 0, err)
            execute.assert_not_called()
            self.assertIn("installed app claw-tabula", out)
            bindings = (home / "app-bindings.toml").read_text(encoding="utf-8")
            self.assertIn("[default]", bindings)
            self.assertIn('app = "claw-tabula"', bindings)
            self.assertNotIn("[[directory]]", bindings)
            applied_lock = json.loads((home / "tenants" / "claw-tabula" / "app.lock.json").read_text(encoding="utf-8"))
            self.assertEqual(applied_lock["bindings"]["default"]["app"], "claw-tabula")
            self.assertNotIn("directory", applied_lock["bindings"])
            runtime_cfg = (home / "config" / "runtime.toml").read_text(encoding="utf-8")
            self.assertIn('id = "claw-tabula"', runtime_cfg)

    def test_install_workspace_sets_directory_binding(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            workspace = root / "workspace"
            _make_distro(root)
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest(root))

            code, _out, err = self._run_cli(["--home", str(home), "app", "install", str(manifest_path), "--workspace", str(workspace)])

            self.assertEqual(code, 0, err)
            bindings = (home / "app-bindings.toml").read_text(encoding="utf-8")
            self.assertIn("[[directory]]", bindings)
            self.assertIn(f'root = "{workspace.resolve()}"', bindings)
            self.assertNotIn("[default]", bindings)

    def test_install_no_bind_materializes_without_binding(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_distro(root)
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest(root))

            code, out, err = self._run_cli(["--home", str(home), "app", "install", str(manifest_path), "--no-bind"])

            self.assertEqual(code, 0, err)
            self.assertIn("unchanged", out)
            self.assertTrue((home / "tenants" / "claw-tabula" / "app.lock.json").is_file())
            bindings = (home / "app-bindings.toml").read_text(encoding="utf-8")
            self.assertNotIn("[default]", bindings)
            self.assertNotIn("[[directory]]", bindings)
            applied_lock = json.loads((home / "tenants" / "claw-tabula" / "app.lock.json").read_text(encoding="utf-8"))
            self.assertEqual(applied_lock["bindings"], {})

    def test_dry_run_clears_stale_materializer_config_without_contract(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            tenant = home / "tenants" / "claw-tabula"
            _make_distro(root)
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest(root))
            _write(tenant / "config" / "tenant.toml", '[application]\nprompt_builder = "claw_prompt.builder"\n')

            code, _out, err = self._run_cli(["--home", str(home), "app", "run", str(manifest_path), "--dry-run"])

            self.assertEqual(code, 0, err)
            self.assertFalse((tenant / "config").exists())

            code, out, err = self._run_cli(["--home", str(home), "app", "inspect", "claw-tabula", "--json"])

            self.assertEqual(code, 0, err)
            payload = json.loads(out)
            self.assertFalse(payload["materializer"]["expected"])
            self.assertFalse(payload["materializer"]["ran"])
            self.assertEqual(payload["issues"], [])

    def test_foreground_run_replaces_process_with_tabula_serve(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_distro(root)
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest(root))
            manifest = appmod.load(manifest_path, tabula_home=home)
            appmod.materialize_metadata(manifest, appmod.create_lock(manifest, home), home)
            boot_path = home / "distrib" / "demo" / "generations" / "1"
            tabula_bin = root / "bin" / "tabula"
            _write(tabula_bin, "#!/bin/sh\n")
            _write(root / "bin" / "tabula-runner", "#!/bin/sh\n")
            runmod.write_runtime_config(manifest, home)
            with mock.patch.object(runmod, "kernel_healthy", return_value=False), mock.patch.object(runmod.os, "execvpe") as execvpe:
                runmod.execute(manifest, home, tabula_bin=str(tabula_bin), foreground=True, boot_path=boot_path)

            execvpe.assert_called_once()
            _file, argv, env = execvpe.call_args.args
            self.assertEqual(argv, [str(root / "bin" / "tabula-runner")])
            self.assertEqual(env["TABULA_HOME"], str(home))
            self.assertEqual(env["TABULA_APP_ID"], "claw-tabula")
            self.assertEqual(env["TABULA_TENANT_ID"], "claw-tabula")
            self.assertEqual(env["TABULA_TENANT_DIR"], str(home / "tenants" / "claw-tabula"))
            self.assertEqual(env["TABULA_BOOT"], str(boot_path))
            self.assertNotIn("TABULA_BOOT_PATH", env)

    def test_runtime_config_merges_existing_app_tenants(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_distro(root)
            existing = home / "config" / "runtime.toml"
            _write(existing, f'''
plugin_dirs = []
skill_dirs = []

[[tenant]]
id = "first-app"
plugin_dirs = ["{home / 'tenants' / 'first-app' / 'plugins'}"]
skill_dirs = ["{home / 'tenants' / 'first-app' / 'skills'}"]

[[kernel]]
id = "main"
url = "unix://{home / 'run' / 'runtime.sock'}"
token_file = "{home / 'run' / 'runtime-token'}"
tenants = ["first-app"]
''')
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest(root))
            manifest = appmod.load(manifest_path, tabula_home=home)

            runmod.write_runtime_config(manifest, home)

            runtime_cfg = existing.read_text(encoding="utf-8")
            self.assertIn('id = "first-app"', runtime_cfg)
            self.assertIn('id = "claw-tabula"', runtime_cfg)
            self.assertIn('tenants = ["first-app", "claw-tabula"]', runtime_cfg)
            self.assertIn('[plugin_kinds.gateway]', runtime_cfg)
            self.assertIn('depends_on = ["driver"]', runtime_cfg)

    def test_runtime_config_uses_short_socket_for_long_home(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / ("long" * 30)
            _make_distro(root)
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest(root))
            manifest = appmod.load(manifest_path, tabula_home=home)

            runmod.write_runtime_config(manifest, home)

            runtime_cfg = (home / "config" / "runtime.toml").read_text(encoding="utf-8")
            self.assertIn('/tabula-rt-', runtime_cfg)
            self.assertIn('/runtime.sock', runtime_cfg)

    def test_runner_sets_app_env_from_single_tenant_runtime_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            bin_dir = home / "bin"
            venv_dir = home / ".venv" / "bin"
            run_dir = home / "run"
            config_dir = home / "config"
            bin_dir.mkdir(parents=True)
            venv_dir.mkdir(parents=True)
            run_dir.mkdir(parents=True)
            config_dir.mkdir(parents=True)
            _write(home / ".env", f"TABULA_VENV={home / '.venv'}\n")
            _write(config_dir / "runtime.toml", f'''
plugin_dirs = []
skill_dirs = []

[[tenant]]
id = "claw-tabula"
plugin_dirs = ["{home / 'tenants' / 'claw-tabula' / 'plugins'}"]
skill_dirs = ["{home / 'tenants' / 'claw-tabula' / 'skills'}"]

[[kernel]]
id = "main"
url = "unix://{run_dir / 'runtime.sock'}"
token_file = "{run_dir / 'runtime-token'}"
tenants = ["claw-tabula"]
''')
            _write(venv_dir / "python3", "#!/bin/sh\nexec python3 \"$@\"\n")
            os.chmod(venv_dir / "python3", 0o755)
            _write(bin_dir / "tabula-runtime", "#!/bin/sh\nexit 0\n")
            os.chmod(bin_dir / "tabula-runtime", 0o755)
            _write(bin_dir / "tabula", f'''#!/bin/sh
printf '%s\n' "$TABULA_APP_ID" > "{root / 'app-id.txt'}"
printf '%s\n' "$TABULA_TENANT_ID" > "{root / 'tenant-id.txt'}"
printf '%s\n' "$TABULA_TENANT_DIR" > "{root / 'tenant-dir.txt'}"
touch "{config_dir / 'runtime.toml'}" "{run_dir / 'runtime-token'}" "{run_dir / 'runtime.sock'}"
exit 0
''')
            os.chmod(bin_dir / "tabula", 0o755)

            result = subprocess.run(
                [str(Path(__file__).parents[3] / "bin" / "tabula-runner"), "--runtime-mode", "disabled"],
                env={**os.environ, "TABULA_HOME": str(home)},
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                timeout=5,
            )

            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual((root / "app-id.txt").read_text(encoding="utf-8").strip(), "claw-tabula")
            self.assertEqual((root / "tenant-id.txt").read_text(encoding="utf-8").strip(), "claw-tabula")
            self.assertEqual((root / "tenant-dir.txt").read_text(encoding="utf-8").strip(), str(home / "tenants" / "claw-tabula"))

    def test_managed_non_bare_backend_not_supported_yet(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_distro(root)
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest(root, backend="ssh"))
            manifest = appmod.load(manifest_path, tabula_home=home)

            with self.assertRaisesRegex(runmod.AppRunError, "not implemented yet"):
                runmod.execute(manifest, home, tabula_bin="tabula-does-not-exist", timeout_seconds=0.1)

    def test_external_kernel_must_be_reachable(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_distro(root)
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest(root, kernel_mode="external", runtime_mode="external"))
            manifest = appmod.load(manifest_path, tabula_home=home)

            with self.assertRaisesRegex(runmod.AppRunError, "external kernel is not reachable"):
                runmod.execute(manifest, home, timeout_seconds=0.1)

    def test_kernel_health_falls_back_to_websocket_connect(self):
        class FakeWS:
            sent: list[str] = []

            def send(self, payload: str) -> None:
                self.sent.append(payload)

            def recv(self) -> str:
                return '{"type":"hello_ack"}'

            def close(self) -> None:
                pass

        with mock.patch.object(runmod, "urlopen", side_effect=OSError("http unavailable")):
            with mock.patch.dict("sys.modules", {"websocket": mock.Mock(create_connection=mock.Mock(return_value=FakeWS()))}):
                self.assertTrue(runmod.kernel_healthy("ws://127.0.0.1:65530/ws", timeout_seconds=0.1))

    def test_runtime_ready_requires_matching_tenant_target(self):
        class FakeResponse:
            status = 200

            def __enter__(self):
                return self

            def __exit__(self, *_exc):
                return None

            def read(self) -> bytes:
                return b'{"runtimes":[{"attached":true,"tenants_served":["first-app","claw-tabula"],"targets":[{"id":"fs","tenants":["claw-tabula"],"state":"ready"}]}]}'

        with mock.patch.object(runmod, "urlopen", return_value=FakeResponse()):
            self.assertTrue(runmod._runtime_ready("ws://127.0.0.1:65530/ws", "claw-tabula", timeout_seconds=0.1))
            self.assertFalse(runmod._runtime_ready("ws://127.0.0.1:65530/ws", "first-app", timeout_seconds=0.1))

    def test_runtime_ready_accepts_attached_runtime_before_targets_prime(self):
        class FakeResponse:
            status = 200

            def __enter__(self):
                return self

            def __exit__(self, *_exc):
                return None

            def read(self) -> bytes:
                return b'{"runtimes":[{"attached":true,"tenants_served":["claw-tabula"],"targets":[]}]}'

        with mock.patch.object(runmod, "urlopen", return_value=FakeResponse()):
            self.assertTrue(runmod._runtime_ready("ws://127.0.0.1:65530/ws", "claw-tabula", timeout_seconds=0.1))


if __name__ == "__main__":
    unittest.main()

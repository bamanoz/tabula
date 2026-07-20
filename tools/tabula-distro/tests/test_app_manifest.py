from __future__ import annotations

import json
import sys
import tempfile
import tomllib
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path

from tabula_distro import app_manifest as appmod
from tabula_distro import cli as climod


def _write(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def _make_distro(root: Path) -> Path:
    distro = root / "claw"
    _write(distro / "distro.toml", '[distro]\nid = "tabula.claw"\nname = "claw"\nversion = "0.1.0"\n')
    (distro / "templates").mkdir(parents=True, exist_ok=True)
    return distro


def _make_materializing_distro(root: Path) -> Path:
    distro = root / "claw"
    _write(distro / "distro.toml", '[distro]\nid = "tabula.claw"\nname = "claw"\n[application_contract]\nmaterializer = "python3 materialize.py"\n')
    (distro / "templates").mkdir(parents=True, exist_ok=True)
    _write(distro / "materialize.py", """
from pathlib import Path
import os
tenant = Path(os.environ['TABULA_TENANT_DIR'])
(tenant / 'config').mkdir(parents=True, exist_ok=True)
(tenant / 'config' / 'materialized.txt').write_text(os.environ['TABULA_APP_ID'], encoding='utf-8')
(tenant / 'config' / 'contract.txt').write_text('\\n'.join([
    os.environ['TABULA_APP_LOCK'],
    os.environ['TABULA_APP_LOCK_JSON'],
    os.environ['TABULA_APP_PHASE'],
    os.environ['TABULA_APP_DRY_RUN'],
]), encoding='utf-8')
    """)
    return distro


def _make_defaults_materializing_distro(root: Path) -> Path:
    distro = root / "claw"
    _write(distro / "distro.toml", '[distro]\nid = "tabula.claw"\nname = "claw"\n[application_contract]\nmaterializer = "python3 materialize.py"\n')
    (distro / "templates").mkdir(parents=True, exist_ok=True)
    _write(distro / "plugins" / "demo" / "plugin.toml", '[plugin]\nid = "demo"\nname = "demo"\nversion = "0.1.0"\n')
    _write(distro / "plugins" / "demo" / "plugin.schema.toml", 'id = "demo"\n\n[entry.extra_roots]\ntype = "string_list"\ndefault = []\n')
    _write(distro / "materialize.py", """
from pathlib import Path
import os
tenant = Path(os.environ['TABULA_TENANT_DIR'])
plugin_dir = tenant / 'config' / 'plugins' / 'demo'
plugin_dir.mkdir(parents=True, exist_ok=True)
(plugin_dir / 'defaults.toml').write_text('''extra_roots = ["/runtime/skills"]\n\n[servers.context7]\ntransport = "stdio"\ncommand = ["npx", "context7-default"]\n\n[limits]\ncount = 1\nmode = "default"\n''', encoding='utf-8')
""")
    return distro


def _make_requirements_distro(root: Path) -> Path:
    distro = root / "claw"
    _write(
        distro / "distro.toml",
        '[distro]\nid = "tabula.claw"\nname = "claw"\nversion = "0.1.0"\n'
        '[[runtime_requirements.executables]]\n'
        'name = "definitely-missing-tabula-tool"\n'
        'required = true\n'
        'required_for = ["mcp.demo"]\n'
        'install_hint = "Install demo tool."\n',
    )
    (distro / "templates").mkdir(parents=True, exist_ok=True)
    return distro


def _manifest(source: str) -> str:
    return f'''
[application]
id = "claw-tabula"
name = "Claw for Tabula"

[distro]
source = "{source}"

[values.workspace]
path = "${{project_root}}"

[values.mempalace]
mode = "shared"
path = "${{local.mempalace.path}}"
'''


class AppManifestTests(unittest.TestCase):
    def test_load_expands_project_and_local_values(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            _make_distro(root)
            _write(root / ".tabula" / "local.toml", '[mempalace]\npath = "/tmp/shared-mempalace"\n')
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest("local:./claw"))

            manifest = appmod.load(manifest_path, tabula_home=root / "home")

            self.assertEqual(manifest.application.id, "claw-tabula")
            self.assertEqual(manifest.bindings.directories[0].root, str(root.resolve()))
            self.assertEqual(manifest.values["workspace"]["path"], str(root.resolve()))
            self.assertEqual(manifest.values["mempalace"]["path"], "/tmp/shared-mempalace")

    def test_missing_local_override_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            _make_distro(root)
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest("local:./claw"))

            with self.assertRaisesRegex(appmod.AppManifestError, "missing local override"):
                appmod.load(manifest_path, tabula_home=root / "home")

    def test_manifest_rejects_distro_name_and_id(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            _make_distro(root)
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, '''
[application]
id = "claw-tabula"

[distro]
id = "tabula.claw"
name = "claw"
source = "local:./claw"
''')

            with self.assertRaisesRegex(appmod.AppManifestError, "distro.id, distro.name is not supported"):
                appmod.load(manifest_path, tabula_home=root / "home")

    def test_create_lock_resolves_relative_local_distro(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            _make_distro(root)
            _write(root / ".tabula" / "local.toml", '[mempalace]\npath = "/tmp/shared-mempalace"\n')
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest("local:./claw"))
            manifest = appmod.load(manifest_path, tabula_home=root / "home")

            lock = appmod.create_lock(manifest, root / "home")

            self.assertEqual(lock.application_id, "claw-tabula")
            self.assertEqual(lock.distro_name, "claw")
            self.assertEqual(lock.distro_lock["distro_version"], "0.1.0")
            self.assertEqual(lock.kernel["mode"], "managed")

    def test_normalize_materializer_command_rewrites_python_launchers_on_windows(self):
        original_name = appmod.os.name
        try:
            appmod.os.name = "nt"
            self.assertEqual(
                appmod._normalize_materializer_command(["python3", "materialize.py"]),
                [sys.executable, "materialize.py"],
            )
            self.assertEqual(
                appmod._normalize_materializer_command(["py", "-3", "materialize.py"]),
                [sys.executable, "materialize.py"],
            )
            self.assertEqual(
                appmod._normalize_materializer_command(["node", "materialize.js"]),
                ["node", "materialize.js"],
            )
        finally:
            appmod.os.name = original_name


class AppCLITests(unittest.TestCase):
    def _run(self, argv: list[str]) -> tuple[int, str, str]:
        out = StringIO()
        err = StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            code = climod.main(argv)
        return code, out.getvalue(), err.getvalue()

    def test_app_lock_and_audit_json(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_distro(root)
            _write(root / ".tabula" / "local.toml", '[mempalace]\npath = "/tmp/shared-mempalace"\n')
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest("local:./claw"))

            code, out, err = self._run(["--home", str(home), "app", "lock", str(manifest_path)])
            self.assertEqual(code, 0, err)
            self.assertIn("wrote app lock", out)
            self.assertTrue((root / "tabula.app.lock").is_file())

            code, out, err = self._run(["--home", str(home), "app", "audit", str(manifest_path), "--json"])
            self.assertEqual(code, 0, err)
            payload = json.loads(out)
            self.assertEqual(payload["application"]["id"], "claw-tabula")
            self.assertEqual(payload["runtimes"][0]["exec"]["backend"], "bare")
            self.assertFalse(payload["applied"])
            self.assertIn("missing tenant.toml", payload["issues"])
            self.assertIn("applied app lock is missing", payload["issues"])

    def test_app_commands_discover_workspace_manifest(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_distro(root)
            _write(root / ".tabula" / "local.toml", '[mempalace]\npath = "/tmp/shared-mempalace"\n')
            _write(root / "tabula.app.toml", _manifest("local:./claw"))
            prev_cwd = Path.cwd()
            try:
                import os
                os.chdir(root)
                code, out, err = self._run(["--home", str(home), "app", "lock"])
            finally:
                os.chdir(prev_cwd)

            self.assertEqual(code, 0, err)
            self.assertIn("wrote app lock", out)
            self.assertTrue((root / "tabula.app.lock").is_file())

    def test_app_commands_discover_dot_tabula_manifest(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_distro(root)
            _write(root / ".tabula" / "local.toml", '[mempalace]\npath = "/tmp/shared-mempalace"\n')
            _write(root / ".tabula" / "app.toml", _manifest("local:../claw"))
            prev_cwd = Path.cwd()
            try:
                import os
                os.chdir(root)
                code, out, err = self._run(["--home", str(home), "app", "lock"])
            finally:
                os.chdir(prev_cwd)

            self.assertEqual(code, 0, err)
            self.assertIn("wrote app lock", out)
            self.assertTrue((root / ".tabula" / "app.lock").is_file())

    def test_app_commands_report_missing_default_manifest(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            prev_cwd = Path.cwd()
            try:
                import os
                os.chdir(root)
                code, _out, err = self._run(["--home", str(home), "app", "lock"])
            finally:
                os.chdir(prev_cwd)

            self.assertEqual(code, 1)
            self.assertIn("expected ./tabula.app.toml or ./.tabula/app.toml", err)

    def test_app_audit_reports_runtime_requirements(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_requirements_distro(root)
            _write(root / ".tabula" / "local.toml", '[mempalace]\npath = "/tmp/shared-mempalace"\n')
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest("local:./claw"))

            code, out, err = self._run(["--home", str(home), "app", "audit", str(manifest_path), "--json"])

            self.assertEqual(code, 0, err)
            payload = json.loads(out)
            self.assertEqual(payload["runtime_requirements"]["executables"][0]["name"], "definitely-missing-tabula-tool")
            self.assertFalse(payload["runtime_requirements"]["executables"][0]["found"])
            self.assertIn("missing required runtime executable: definitely-missing-tabula-tool", payload["issues"])

    def test_app_apply_fails_when_required_runtime_executable_missing(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_requirements_distro(root)
            _write(root / ".tabula" / "local.toml", '[mempalace]\npath = "/tmp/shared-mempalace"\n')
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest("local:./claw"))

            code, _out, err = self._run(["--home", str(home), "app", "apply", str(manifest_path)])

            self.assertEqual(code, 1)
            self.assertIn("missing required runtime executables", err)
            self.assertIn("definitely-missing-tabula-tool", err)

    def test_app_inspect_reports_materialized_surface(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_materializing_distro(root)
            _write(root / ".tabula" / "local.toml", '[mempalace]\npath = "/tmp/shared-mempalace"\n')
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest("local:./claw"))

            code, _out, err = self._run(["--home", str(home), "app", "run", str(manifest_path), "--dry-run"])
            self.assertEqual(code, 0, err)
            code, out, err = self._run(["--home", str(home), "app", "inspect", "claw-tabula", "--json"])

            self.assertEqual(code, 0, err)
            payload = json.loads(out)
            self.assertEqual(payload["application"]["id"], "claw-tabula")
            self.assertTrue(payload["present"]["tenant"])
            self.assertTrue(payload["runtime_surface"]["configured"])
            self.assertTrue(payload["materializer"]["ran"])
            self.assertEqual(payload["issues"], [])

            code, out, err = self._run(["--home", str(home), "app", "audit", str(manifest_path), "--json"])
            self.assertEqual(code, 0, err)
            payload = json.loads(out)
            self.assertTrue(payload["applied"])
            self.assertTrue(payload["runtime_surface"]["configured"])
            self.assertEqual(payload["issues"], [])

            (home / "tenants" / "claw-tabula" / "values.toml").unlink()
            code, out, err = self._run(["--home", str(home), "app", "audit", str(manifest_path), "--json"])
            self.assertEqual(code, 0, err)
            payload = json.loads(out)
            self.assertIn("missing values.toml", payload["issues"])

    def test_app_apply_materializes_tenant_metadata(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_distro(root)
            _write(root / ".tabula" / "local.toml", '[mempalace]\npath = "/tmp/shared-mempalace"\n')
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest("local:./claw"))

            code, out, err = self._run(["--home", str(home), "app", "apply", str(manifest_path)])
            self.assertEqual(code, 0, err)
            self.assertIn("applied app claw-tabula", out)
            tenant = home / "tenants" / "claw-tabula"
            self.assertTrue((tenant / "tenant.toml").is_file())
            self.assertTrue((tenant / "app.lock.json").is_file())
            self.assertTrue((tenant / "plugins").is_dir())
            self.assertTrue((tenant / "skills").is_dir())
            self.assertFalse((home / "boot.py").exists())
            self.assertIn("/tmp/shared-mempalace", (tenant / "values.toml").read_text(encoding="utf-8"))

    def test_app_apply_runs_distro_materializer(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_materializing_distro(root)
            _write(root / ".tabula" / "local.toml", '[mempalace]\npath = "/tmp/shared-mempalace"\n')
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest("local:./claw"))

            code, out, err = self._run(["--home", str(home), "app", "apply", str(manifest_path)])
            self.assertEqual(code, 0, err)
            self.assertIn("materializer: ran", out)
            self.assertEqual((home / "tenants" / "claw-tabula" / "config" / "materialized.txt").read_text(encoding="utf-8"), "claw-tabula")
            contract = (home / "tenants" / "claw-tabula" / "config" / "contract.txt").read_text(encoding="utf-8").splitlines()
            self.assertEqual(Path(contract[0]).resolve(), (root / "tabula.app.lock").resolve())
            self.assertEqual(Path(contract[1]).resolve(), (home / "tenants" / "claw-tabula" / "app.lock.json").resolve())
            self.assertEqual(contract[2], "apply")
            self.assertEqual(contract[3], "0")

    def test_app_apply_compiles_effective_plugin_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_defaults_materializing_distro(root)
            _write(root / ".tabula" / "local.toml", '[mempalace]\npath = "/tmp/shared-mempalace"\n')
            _write(
                home / "config" / "plugins" / "demo" / "config.toml",
                'extra_roots = ["/user/skills"]\n\n[servers.context7]\ncommand = ["npx", "context7-user"]\n\n[servers.mycorp]\ntransport = "stdio"\ncommand = ["mycorp"]\n\n[limits]\ncount = 2\n',
            )
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest("local:./claw"))

            code, _out, err = self._run(["--home", str(home), "app", "apply", str(manifest_path)])

            self.assertEqual(code, 0, err)
            compiled = tomllib.loads((home / "tenants" / "claw-tabula" / "config" / "plugins" / "demo" / "config.toml").read_text(encoding="utf-8"))
            self.assertEqual(compiled["servers"]["context7"]["transport"], "stdio")
            self.assertEqual(compiled["servers"]["context7"]["command"], ["npx", "context7-user"])
            self.assertEqual(compiled["servers"]["mycorp"]["command"], ["mycorp"])
            self.assertEqual(compiled["limits"]["count"], 2)
            self.assertEqual(compiled["limits"]["mode"], "default")
            self.assertEqual(compiled["extra_roots"], ["/user/skills", "/runtime/skills"])

    def test_app_apply_migrates_driver_config_to_plugin_surface(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            tenant_dir = home / "tenants" / "claw-tabula"
            _make_defaults_materializing_distro(root)
            _write(root / ".tabula" / "local.toml", '[mempalace]\npath = "/tmp/shared-mempalace"\n')
            _write(home / "config" / "global.toml", '[clients.driver]\nprovider = "externcash"\n\n[clients.driver.providers.externcash]\ntype = "openai"\ndefault_model = "gpt-5.5"\n')
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest("local:./claw"))

            code, _out, err = self._run(["--home", str(home), "app", "apply", str(manifest_path)])

            self.assertEqual(code, 0, err)
            global_cfg = tomllib.loads((home / "config" / "global.toml").read_text(encoding="utf-8"))
            self.assertNotIn("clients", global_cfg)
            self.assertEqual(global_cfg["plugins"]["driver"]["provider"], "externcash")
            self.assertEqual(global_cfg["plugins"]["driver"]["providers"]["externcash"]["default_model"], "gpt-5.5")

    def test_compile_plugin_configs_migrates_tenant_driver_app_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            tenant_dir = home / "tenants" / "claw-tabula"
            _write(home / "plugins" / "driver" / "plugin.toml", '[plugin]\nid = "driver"\n')
            _write(home / "plugins" / "driver" / "plugin.schema.toml", 'id = "driver"\n\n[entry.agents]\ntype = "object"\ndefault = {}\n')
            _write(tenant_dir / "config" / "apps" / "driver" / "config.toml", '[agents.general]\nprovider = "externcash"\n')

            appmod.compile_plugin_configs(home, tenant_dir)

            self.assertFalse((tenant_dir / "config" / "apps" / "driver" / "config.toml").exists())
            tenant_cfg = tomllib.loads((tenant_dir / "config" / "plugins" / "driver" / "config.toml").read_text(encoding="utf-8"))
            self.assertEqual(tenant_cfg["agents"]["general"]["provider"], "externcash")

    def test_frozen_requires_existing_lock(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_distro(root)
            _write(root / ".tabula" / "local.toml", '[mempalace]\npath = "/tmp/shared-mempalace"\n')
            manifest_path = root / "tabula.app.toml"
            _write(manifest_path, _manifest("local:./claw"))

            code, _out, err = self._run(["--home", str(home), "app", "lock", str(manifest_path), "--frozen"])
            self.assertEqual(code, 1)
            self.assertIn("--frozen requires app lockfile", err)


if __name__ == "__main__":
    unittest.main()

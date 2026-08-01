from __future__ import annotations

import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
import os
from pathlib import Path

from tabula_distro import cli as distro_cli
from tabula_distro import install_cli


def _write(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def _make_distro(root: Path) -> Path:
    distro = root / "demo"
    _write(distro / "distro.toml", '[distro]\nid = "tabula.demo"\nname = "demo"\nversion = "0.1.0"\n')
    _write(distro / "templates" / "SYSTEM.md", "system v1\n")
    return distro


def _make_bundle(root: Path, bundle_name: str, component_name: str) -> Path:
    bundle = root / bundle_name
    _write(bundle / component_name / "plugin.toml", f'id = "{component_name}"\n')
    _write(bundle / component_name / "run.py", "# plugin\n")
    _write(bundle / "bundle.toml", f'[bundle]\nname = "{bundle_name}"\ncomponents = ["{component_name}"]\n')
    return bundle


class InstallCliTests(unittest.TestCase):
    def test_tabula_install_distro_install_uses_existing_installer(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_distro(root)
            stdout = StringIO()
            stderr = StringIO()
            with redirect_stdout(stdout), redirect_stderr(stderr):
                code = install_cli.main(["--home", str(home), "distro", "install", str(distro)])
            self.assertEqual(code, 0, stderr.getvalue())
            self.assertTrue((home / "distrib" / "demo" / "distro.lock.json").is_file())
            runtime_toml = (home / "config" / "runtime.toml").read_text(encoding="utf-8")
            self.assertIn('tenants = ["*"]', runtime_toml)

    def test_tabula_distro_legacy_entrypoint_still_installs(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_distro(root)
            stdout = StringIO()
            stderr = StringIO()
            with redirect_stdout(stdout), redirect_stderr(stderr):
                code = distro_cli.main(["--home", str(home), "install", str(distro)])
            self.assertEqual(code, 0, stderr.getvalue())
            self.assertTrue((home / "distrib" / "demo" / "distro.lock.json").is_file())

    def test_tabula_install_distro_reinstall_by_name_uses_saved_source(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_distro(root)
            stdout = StringIO()
            stderr = StringIO()
            with redirect_stdout(stdout), redirect_stderr(stderr):
                code = install_cli.main(["--home", str(home), "distro", "install", str(distro)])
            self.assertEqual(code, 0, stderr.getvalue())

            _write(distro / "templates" / "SYSTEM.md", "system v2\n")
            stdout = StringIO()
            stderr = StringIO()
            with redirect_stdout(stdout), redirect_stderr(stderr):
                code = install_cli.main(["--home", str(home), "distro", "reinstall", "demo"])
            self.assertEqual(code, 0, stderr.getvalue())
            installed_template = (home / "distrib" / "demo" / "templates" / "SYSTEM.md").read_text(encoding="utf-8")
            self.assertEqual(installed_template, "system v2\n")

    def test_tabula_install_distro_reinstall_defaults_to_active(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_distro(root)
            stdout = StringIO()
            stderr = StringIO()
            with redirect_stdout(stdout), redirect_stderr(stderr):
                code = install_cli.main(["--home", str(home), "distro", "install", str(distro)])
            self.assertEqual(code, 0, stderr.getvalue())

            _write(distro / "templates" / "SYSTEM.md", "system v2\n")
            stdout = StringIO()
            stderr = StringIO()
            with redirect_stdout(stdout), redirect_stderr(stderr):
                code = install_cli.main(["--home", str(home), "distro", "reinstall"])
            self.assertEqual(code, 0, stderr.getvalue())
            installed_template = (home / "distrib" / "demo" / "templates" / "SYSTEM.md").read_text(encoding="utf-8")
            self.assertEqual(installed_template, "system v2\n")

    def test_tabula_install_distro_use_discovers_sibling_checkout(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            workspace = root / "workspace"
            repo = workspace / "tabula"
            repo.mkdir(parents=True)
            distro = _make_distro(workspace / "tabula-distrib")
            prev_cwd = Path.cwd()
            os.chdir(repo)
            try:
                stdout = StringIO()
                stderr = StringIO()
                with redirect_stdout(stdout), redirect_stderr(stderr):
                    code = install_cli.main(["--home", str(home), "distro", "use", "demo"])
                self.assertEqual(code, 0, stderr.getvalue())
            finally:
                os.chdir(prev_cwd)
            self.assertTrue((home / "distrib" / "demo" / "distro.lock.json").is_file())

    def test_tabula_install_distro_use_source_root_installs_named_checkout(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distros = root / "distros"
            _make_distro(distros)
            stdout = StringIO()
            stderr = StringIO()
            with redirect_stdout(stdout), redirect_stderr(stderr):
                code = install_cli.main([
                    "--home", str(home), "distro", "use", "demo",
                    "--source-root", str(distros),
                ])
            self.assertEqual(code, 0, stderr.getvalue())
            self.assertTrue((home / "distrib" / "demo" / "distro.lock.json").is_file())

    def test_tabula_install_top_level_use_alias_installs_named_checkout(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distros = root / "distros"
            _make_distro(distros)
            stdout = StringIO()
            stderr = StringIO()
            with redirect_stdout(stdout), redirect_stderr(stderr):
                code = install_cli.main([
                    "--home", str(home), "use", "demo",
                    "--source-root", str(distros),
                ])
            self.assertEqual(code, 0, stderr.getvalue())
            self.assertTrue((home / "distrib" / "demo" / "distro.lock.json").is_file())

    def test_tabula_install_use_overrides_git_source_alias_to_sibling_checkout(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            workspace = root / "workspace"
            repo = workspace / "tabula"
            repo.mkdir(parents=True)
            distro_repo = workspace / "tabula-distrib"
            distro = distro_repo / "demo"
            _write(
                distro / "distro.toml",
                '[distro]\nid = "tabula.demo"\nname = "demo"\nversion = "0.1.0"\n'
                '[sources.bundles]\nsource = "git+https://example.invalid/bundles.git@main"\n'
                '[[bundles]]\nname = "extensions"\nsource = "source:bundles#path=extensions"\ncomponents = ["hello"]\n',
            )
            _make_bundle(workspace / "bundles", "extensions", "hello")
            prev_cwd = Path.cwd()
            os.chdir(repo)
            try:
                stdout = StringIO()
                stderr = StringIO()
                with redirect_stdout(stdout), redirect_stderr(stderr):
                    code = install_cli.main(["--home", str(home), "use", "demo"])
                self.assertEqual(code, 0, stderr.getvalue())
            finally:
                os.chdir(prev_cwd)
            self.assertTrue((home / "plugins" / "hello" / "plugin.toml").is_file())

    def test_missing_required_runtime_executable_blocks_install(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_distro(root)
            _write(
                distro / "distro.toml",
                '[distro]\nid = "tabula.demo"\nname = "demo"\nversion = "0.1.0"\n'
                '[[runtime_requirements.executables]]\n'
                'name = "definitely-missing-tabula-tool"\n'
                'required = true\n'
                'required_for = ["mcp.demo"]\n'
                'install_hint = "Install demo tool."\n',
            )
            stdout = StringIO()
            stderr = StringIO()
            with redirect_stdout(stdout), redirect_stderr(stderr):
                code = install_cli.main(["--home", str(home), "distro", "install", str(distro)])

            self.assertEqual(code, 1)
            self.assertIn("missing required runtime executables", stderr.getvalue())
            self.assertIn("definitely-missing-tabula-tool", stderr.getvalue())
            self.assertIn("Install demo tool.", stderr.getvalue())

    def test_missing_optional_runtime_executable_warns_but_installs(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_distro(root)
            _write(
                distro / "distro.toml",
                '[distro]\nid = "tabula.demo"\nname = "demo"\nversion = "0.1.0"\n'
                '[[runtime_requirements.executables]]\n'
                'name = "definitely-missing-tabula-optional-tool"\n'
                'required = false\n'
                'required_for = ["fs.grep"]\n',
            )
            stdout = StringIO()
            stderr = StringIO()
            with redirect_stdout(stdout), redirect_stderr(stderr):
                code = install_cli.main(["--home", str(home), "distro", "install", str(distro)])

            self.assertEqual(code, 0, stderr.getvalue())
            self.assertIn("optional runtime executable", stdout.getvalue())
            self.assertTrue((home / "distrib" / "demo" / "distro.lock.json").is_file())


if __name__ == "__main__":
    unittest.main()

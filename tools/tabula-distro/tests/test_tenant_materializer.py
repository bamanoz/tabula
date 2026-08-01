from __future__ import annotations

import json
import tempfile
import tomllib
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path

from tabula_distro import config as distro_config
from tabula_distro import install_cli


def _write(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def _make_distro(root: Path, *, failing: bool = False) -> Path:
    distro = root / "demo"
    _write(
        distro / "distro.toml",
        '[distro]\nid = "tabula.demo"\nname = "demo"\nversion = "0.1.0"\n'
        '[tenant_contract]\n'
        'materializer = "python3 materialize.py"\n'
        'values_schema = "values.schema.json"\n'
        'values_defaults = "values.defaults.toml"\n',
    )
    _write(distro / "values.schema.json", '{"version": 1}\n')
    _write(distro / "values.defaults.toml", "# defaults\n")
    _write(
        distro / "plugins" / "demo" / "plugin.toml",
        '[plugin]\nid = "demo"\nname = "demo"\nversion = "0.1.0"\n',
    )
    _write(
        distro / "plugins" / "demo" / "plugin.schema.toml",
        '[entry.extra_roots]\ntype = "string_list"\n',
    )
    if failing:
        _write(distro / "materialize.py", 'raise SystemExit("boom")\n')
    else:
        _write(
            distro / "materialize.py",
            """
import json
import os
from pathlib import Path

tenant = Path(os.environ["TABULA_TENANT_DIR"])
config = tenant / "config" / "plugins" / "demo"
config.mkdir(parents=True, exist_ok=True)
values = Path(os.environ["TABULA_TENANT_VALUES"])
(config / "defaults.toml").write_text(
    f'extra_roots = ["{os.environ["TABULA_TENANT_PROJECT_ROOT"]}"]\\n',
    encoding="utf-8",
)
(tenant / "contract.json").write_text(json.dumps({
    "id": os.environ["TABULA_TENANT_ID"],
    "distro_dir": os.environ["TABULA_TENANT_DISTRO_DIR"],
    "project_root": os.environ["TABULA_TENANT_PROJECT_ROOT"],
    "values": str(values),
    "lock": os.environ["TABULA_TENANT_INSTALL_LOCK"],
}), encoding="utf-8")
""",
        )
    return distro


class TenantMaterializerTests(unittest.TestCase):
    def _run(self, argv: list[str]) -> tuple[int, str, str]:
        stdout = StringIO()
        stderr = StringIO()
        with redirect_stdout(stdout), redirect_stderr(stderr):
            code = install_cli.main(argv)
        return code, stdout.getvalue(), stderr.getvalue()

    def test_tenant_install_materializes_without_project_manifest(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            project = root / "project"
            project.mkdir()
            distro = _make_distro(root)
            values = root / "values.toml"
            _write(values, '[profile]\nmode = "strict"\n')

            code, out, err = self._run([
                "--home", str(home),
                "tenant", "install", str(distro),
                "--id", "project-demo",
                "--root", str(project),
                "--values", str(values),
            ])

            self.assertEqual(code, 0, err)
            self.assertIn("installed tenant project-demo", out)
            installed_path = home / "distrib" / "demo"
            installed = distro_config.load(installed_path)
            self.assertEqual(installed.tenant_values_schema, "values.schema.json")
            self.assertEqual(installed.tenant_values_defaults, "values.defaults.toml")
            tenant = home / "tenants" / "project-demo"
            lock = json.loads((tenant / "install.lock.json").read_text(encoding="utf-8"))
            self.assertEqual(lock["distro"]["distro_source"], str(distro.resolve()))
            self.assertEqual(lock["path"], "distrib/demo")
            self.assertTrue(installed_path.is_dir())
            self.assertTrue((installed_path / "distro.toml").is_file())
            self.assertTrue((tenant / "plugins" / "demo" / "plugin.toml").is_file())
            self.assertEqual(tomllib.loads((tenant / "values.toml").read_text(encoding="utf-8"))["profile"]["mode"], "strict")
            contract = json.loads((tenant / "contract.json").read_text(encoding="utf-8"))
            self.assertEqual(contract["id"], "project-demo")
            self.assertEqual(Path(contract["distro_dir"]).resolve(), installed_path.resolve())
            self.assertEqual(Path(contract["project_root"]).resolve(), project.resolve())
            self.assertEqual(Path(contract["lock"]).resolve(), (tenant / "install.lock.json").resolve())
            compiled = tomllib.loads((tenant / "config" / "plugins" / "demo" / "config.toml").read_text(encoding="utf-8"))
            self.assertEqual(compiled["extra_roots"], [str(project.resolve())])
            runtime = tomllib.loads((home / "config" / "runtime.toml").read_text(encoding="utf-8"))
            self.assertEqual(runtime["plugin_dirs"], [])
            self.assertEqual(runtime["tenant"][0]["id"], "project-demo")
            self.assertEqual([Path(path).resolve() for path in runtime["tenant"][0]["plugin_dirs"]], [(tenant / "plugins").resolve()])
            self.assertEqual(runtime["distro"]["active"], "demo")
            self.assertEqual(Path(runtime["distro"]["dir"]).resolve(), installed_path.resolve())
            bindings = tomllib.loads((home / "bindings.toml").read_text(encoding="utf-8"))
            self.assertEqual(bindings["directory"], [{"root": str(project.resolve()), "tenant": "project-demo"}])

    def test_two_tenants_keep_separate_runtime_surfaces(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            project_a = root / "project-a"
            project_b = root / "project-b"
            project_a.mkdir()
            project_b.mkdir()
            distro_a = _make_distro(root / "alpha")
            distro_b = _make_distro(root / "beta")
            args = ["--home", str(home), "tenant", "install"]

            self.assertEqual(self._run([*args, str(distro_a), "--id", "project-a", "--root", str(project_a)])[0], 0)
            self.assertEqual(self._run([*args, str(distro_b), "--id", "project-b", "--root", str(project_b)])[0], 0)

            runtime = tomllib.loads((home / "config" / "runtime.toml").read_text(encoding="utf-8"))
            tenants = {item["id"]: item for item in runtime["tenant"]}
            self.assertEqual(set(tenants), {"project-a", "project-b"})
            self.assertEqual([Path(path).resolve() for path in tenants["project-a"]["plugin_dirs"]], [(home / "tenants" / "project-a" / "plugins").resolve()])
            self.assertEqual([Path(path).resolve() for path in tenants["project-b"]["plugin_dirs"]], [(home / "tenants" / "project-b" / "plugins").resolve()])
            self.assertEqual(set(runtime["kernel"][0]["tenants"]), {"project-a", "project-b"})
            self.assertEqual(runtime["distro"]["active"], "demo")
            self.assertTrue(Path(runtime["distro"]["dir"]).resolve().is_dir())

    def test_tenant_materialize_updates_values_without_project_manifest(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            project = root / "project"
            extra = root / "extra"
            project.mkdir()
            extra.mkdir()
            distro = _make_distro(root)
            install_args = [
                "--home", str(home), "tenant", "install", str(distro),
                "--id", "project-demo", "--root", str(project),
            ]
            self.assertEqual(self._run(install_args)[0], 0)
            values = root / "updated.toml"
            _write(values, f'[workspace]\npath = "{project}"\nextra_roots = ["{extra}"]\n')

            code, out, err = self._run([
                "--home", str(home), "tenant", "materialize", "project-demo",
                "--root", str(project), "--values", str(values),
            ])

            self.assertEqual(code, 0, err)
            self.assertIn("materialized tenant project-demo", out)
            installed = tomllib.loads(
                (home / "tenants" / "project-demo" / "values.toml").read_text(encoding="utf-8")
            )
            self.assertEqual(installed["workspace"]["extra_roots"], [str(extra)])

    def test_tenant_install_requires_replace_for_conflicting_project_binding(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            project = root / "project"
            project.mkdir()
            distro = _make_distro(root)
            (home / "bindings.toml").parent.mkdir(parents=True)
            (home / "bindings.toml").write_text(
                f'[[directory]]\nroot = "{project.resolve()}"\ntenant = "existing"\n',
                encoding="utf-8",
            )
            args = [
                "--home", str(home), "tenant", "install", str(distro),
                "--id", "replacement", "--root", str(project),
            ]

            code, _out, err = self._run(args)
            self.assertEqual(code, 1)
            self.assertIn("--replace-binding", err)
            self.assertFalse((home / "tenants" / "replacement").exists())

            code, _out, err = self._run([*args, "--replace-binding"])
            self.assertEqual(code, 0, err)
            bindings = tomllib.loads((home / "bindings.toml").read_text(encoding="utf-8"))
            self.assertEqual(bindings["directory"][0]["tenant"], "replacement")

    def test_tenant_install_rejects_contract_file_outside_installed_distro(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            project = root / "project"
            project.mkdir()
            distro = _make_distro(root)
            config = (distro / "distro.toml").read_text(encoding="utf-8")
            (distro / "distro.toml").write_text(
                config.replace('values_schema = "values.schema.json"', 'values_schema = "../schema.json"'),
                encoding="utf-8",
            )

            code, _out, err = self._run([
                "--home", str(home), "tenant", "install", str(distro),
                "--id", "project-demo", "--root", str(project),
            ])

            self.assertEqual(code, 1)
            self.assertIn("must name a file inside the installed distro", err)
            self.assertFalse((home / "tenants" / "project-demo").exists())

    def test_tenant_install_rejects_existing_tenant(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            project = root / "project"
            project.mkdir()
            distro = _make_distro(root)
            args = [
                "--home", str(home), "tenant", "install", str(distro),
                "--id", "project-demo", "--root", str(project),
            ]
            self.assertEqual(self._run(args)[0], 0)

            code, _out, err = self._run(args)
            self.assertEqual(code, 1)
            self.assertIn("tenant already exists", err)

    def test_materializer_failure_removes_new_tenant(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            project = root / "project"
            project.mkdir()
            distro = _make_distro(root, failing=True)

            code, _out, err = self._run([
                "--home", str(home), "tenant", "install", str(distro),
                "--id", "project-demo", "--root", str(project),
            ])

            self.assertEqual(code, 1)
            self.assertIn("tenant materializer failed", err)
            self.assertFalse((home / "tenants" / "project-demo").exists())

    def test_tenant_install_validates_kernel_tenant_id_grammar(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            project = root / "project"
            project.mkdir()
            distro = _make_distro(root)

            code, _out, err = self._run([
                "--home", str(home), "tenant", "install", str(distro),
                "--id", "Project_Demo", "--root", str(project),
            ])

            self.assertEqual(code, 1)
            self.assertIn("invalid tenant id", err)
            self.assertFalse((home / "tenants").exists())


if __name__ == "__main__":
    unittest.main()

from __future__ import annotations

import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path

from tabula_distro import tenant_bindings as bindmod
from tabula_distro import cli as climod


def _write(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")



class BindingRegistryTests(unittest.TestCase):
    def test_nearest_directory_binding_wins(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp).resolve()
            nested = root / "project" / "pkg"
            nested.mkdir(parents=True)
            registry = bindmod.Registry()
            registry = bindmod.bind_directory(registry, str(root), "root-tenant")
            registry = bindmod.bind_directory(registry, str(root / "project"), "project-tenant")

            resolved = bindmod.resolve(registry, nested)

            self.assertIsInstance(resolved, bindmod.DirectoryBinding)
            self.assertEqual(resolved.tenant, "project-tenant")

    def test_round_trip_registry(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            registry = bindmod.bind_default(bindmod.Registry(), "personal")
            registry = bindmod.bind_directory(registry, "/tmp/project", "project")

            bindmod.save(home, registry)
            loaded = bindmod.load(home)

            self.assertEqual(loaded.default.tenant, "personal")
            self.assertEqual(loaded.directories[0].tenant, "project")


class BindingCLITests(unittest.TestCase):
    def _run(self, argv: list[str]) -> tuple[int, str, str]:
        out = StringIO()
        err = StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            code = climod.main(argv, prog="tabula-install")
        return code, out.getvalue(), err.getvalue()

    def test_bind_requires_existing_tenant(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            code, _out, err = self._run([
                "--home", str(home), "tenant", "bind", "missing", "--default",
            ])
            self.assertEqual(code, 1)
            self.assertIn("unknown tenant", err)

    def test_manual_bind_and_unbind(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            tenant_dir = home / "tenants" / "claw-tenant"
            tenant_dir.mkdir(parents=True)
            _write(tenant_dir / "install.lock.json", '{"version": 1}\n')
            other = root / "other"
            other.mkdir()

            code, out, err = self._run([
                "--home", str(home), "tenant", "bind", "claw-tenant", "--directory", str(other),
            ])
            self.assertEqual(code, 0, err)
            self.assertIn("bound", out)

            code, out, err = self._run(["--home", str(home), "tenant", "unbind", "--directory", str(other)])
            self.assertEqual(code, 0, err)
            self.assertIn("unbound", out)

            registry = bindmod.load(home)
            self.assertFalse(any(binding.root == str(other.resolve()) for binding in registry.directories))


if __name__ == "__main__":
    unittest.main()

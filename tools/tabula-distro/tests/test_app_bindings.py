from __future__ import annotations

import json
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path

from tabula_distro import app_bindings as bindmod
from tabula_distro import cli as climod


def _write(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def _make_distro(root: Path) -> None:
    distro = root / "claw"
    _write(distro / "distro.toml", '[distro]\nid = "tabula.claw"\nname = "claw"\n')
    _write(distro / "boot.py", "# boot\n")


def _manifest(root: Path) -> str:
    return f'''
[application]
id = "claw-tabula"

[distro]
id = "tabula.claw"
name = "claw"
source = "local:{root / 'claw'}"

[kernel]
mode = "managed"
id = "claw-tabula"
url = "ws://127.0.0.1:8089/ws"

[[runtimes]]
id = "local"
mode = "managed"
tenants = ["claw-tabula"]

[runtimes.exec]
backend = "bare"

[[bindings.directory]]
root = "${{project_root}}"
app = "claw-tabula"
kernel = "claw-tabula"
'''


class BindingRegistryTests(unittest.TestCase):
    def test_nearest_directory_binding_wins(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp).resolve()
            nested = root / "project" / "pkg"
            nested.mkdir(parents=True)
            registry = bindmod.Registry()
            registry = bindmod.bind_directory(registry, str(root), "root-app", "local")
            registry = bindmod.bind_directory(registry, str(root / "project"), "project-app", "local")

            resolved = bindmod.resolve(registry, nested)

            self.assertIsInstance(resolved, bindmod.DirectoryBinding)
            self.assertEqual(resolved.app, "project-app")

    def test_round_trip_registry(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            registry = bindmod.bind_default(bindmod.Registry(), "personal", "local")
            registry = bindmod.bind_directory(registry, "/tmp/project", "project", "local")

            bindmod.save(home, registry)
            loaded = bindmod.load(home)

            self.assertEqual(loaded.default.app, "personal")
            self.assertEqual(loaded.directories[0].app, "project")


class BindingCLITests(unittest.TestCase):
    def _run(self, argv: list[str]) -> tuple[int, str, str]:
        out = StringIO()
        err = StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            code = climod.main(argv)
        return code, out.getvalue(), err.getvalue()

    def test_apply_materializes_manifest_bindings(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_distro(root)
            manifest = root / "tabula.app.toml"
            _write(manifest, _manifest(root))

            code, _out, err = self._run(["--home", str(home), "app", "apply", str(manifest)])
            self.assertEqual(code, 0, err)

            payload = json.loads(self._run(["--home", str(home), "app", "bindings", "--json"])[1])
            self.assertEqual(payload["directory"][0]["app"], "claw-tabula")
            self.assertEqual(payload["directory"][0]["kernel"], "claw-tabula")

    def test_bind_requires_existing_app(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"

            code, _out, err = self._run([
                "--home", str(home), "app", "bind", "missing", "--default", "--kernel", "local",
            ])

            self.assertEqual(code, 1)
            self.assertIn("unknown app", err)

    def test_manual_bind_and_unbind(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _make_distro(root)
            manifest = root / "tabula.app.toml"
            _write(manifest, _manifest(root))
            self.assertEqual(self._run(["--home", str(home), "app", "apply", str(manifest)])[0], 0)

            other = root / "other"
            other.mkdir()
            code, out, err = self._run([
                "--home", str(home), "app", "bind", "claw-tabula", "--directory", str(other), "--kernel", "local",
            ])
            self.assertEqual(code, 0, err)
            self.assertIn("bound", out)

            code, out, err = self._run(["--home", str(home), "app", "unbind", "--directory", str(other)])
            self.assertEqual(code, 0, err)
            self.assertIn("unbound", out)

            registry = bindmod.load(home)
            self.assertFalse(any(binding.root == str(other.resolve()) for binding in registry.directories))


if __name__ == "__main__":
    unittest.main()

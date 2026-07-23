from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from tabula_distro import tenant_bindings as bindings


class TenantBindingTests(unittest.TestCase):
    def test_longest_directory_match_wins_before_default(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp).resolve()
            nested = root / "project" / "pkg"
            nested.mkdir(parents=True)
            registry = bindings.bind_default(bindings.Registry(), "default")
            registry = bindings.bind_directory(registry, root, "root")
            registry = bindings.bind_directory(registry, root / "project", "project")

            selected = bindings.resolve(registry, nested)

            self.assertIsInstance(selected, bindings.DirectoryBinding)
            self.assertEqual(selected.tenant, "project")
            self.assertEqual(bindings.resolve(registry, root.parent).tenant, "default")

    def test_same_binding_is_idempotent_and_conflict_requires_replace(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp).resolve()
            registry = bindings.bind_directory(bindings.Registry(), root, "one")

            self.assertEqual(bindings.bind_directory(registry, root, "one"), registry)
            with self.assertRaisesRegex(bindings.BindingError, "--replace-binding"):
                bindings.bind_directory(registry, root, "two")
            replaced = bindings.bind_directory(registry, root, "two", replace=True)
            self.assertEqual(replaced.directories[0].tenant, "two")

    def test_round_trip_uses_tenant_only_schema(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            registry = bindings.bind_default(bindings.Registry(), "personal")
            registry = bindings.bind_directory(registry, Path(tmp) / "project", "project")

            bindings.save(home, registry)
            text = (home / "bindings.toml").read_text(encoding="utf-8")

            self.assertNotIn("app", text)
            self.assertNotIn("kernel", text)
            self.assertEqual(bindings.load(home), registry)


if __name__ == "__main__":
    unittest.main()

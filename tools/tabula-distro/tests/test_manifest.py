from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from tabula_distro.manifest import ManifestError, load_bundle_manifest


def _write(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


class BundleManifestTests(unittest.TestCase):
    def test_parses_python_and_typescript_exports_and_dependencies(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            _write(root / "bundle.toml", """
[bundle]
name = "base"
components = ["plugin-sdk"]

[[exports.python_packages]]
name = "tabula_plugin_sdk"
path = "plugin-sdk/sdk/python/src/tabula_plugin_sdk"
public = true
owner = "plugin-sdk"

[[exports.python_packages]]
name = "tabula_artifacts"
path = "tool-result-store/sdk/python/src/tabula_artifacts"
public = false
owner = "tool-result-store"

[[exports.typescript_packages]]
name = "@tabula/skill-sdk"
path = "skills/sdk/typescript"
public = true
owner = "skills"

[[dependencies]]
bundle = "drivers"
python_packages = ["tabula_driver_sdk"]
typescript_packages = ["@tabula/driver-sdk"]
""")

            manifest = load_bundle_manifest(root)

        self.assertEqual([item.name for item in manifest.exports_python_packages], ["tabula_plugin_sdk", "tabula_artifacts"])
        self.assertEqual(manifest.exports_python_packages[1].public, False)
        self.assertEqual(manifest.exports_python_packages[1].owner, "tool-result-store")
        self.assertEqual([item.name for item in manifest.exports_typescript_packages], ["@tabula/skill-sdk"])
        self.assertEqual(manifest.exports_typescript_packages[0].path, "skills/sdk/typescript")
        self.assertEqual(manifest.dependencies[0].python_packages, ("tabula_driver_sdk",))
        self.assertEqual(manifest.dependencies[0].typescript_packages, ("@tabula/driver-sdk",))

    def test_rejects_non_boolean_public(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            _write(root / "bundle.toml", """
[bundle]
name = "bad"

[[exports.python_packages]]
name = "tabula_plugin_sdk"
path = "plugin-sdk/sdk/python/src/tabula_plugin_sdk"
public = "yes"
""")

            with self.assertRaisesRegex(ManifestError, "public must be a boolean"):
                load_bundle_manifest(root)

    def test_rejects_invalid_typescript_export(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            _write(root / "bundle.toml", """
[bundle]
name = "bad"

[[exports.typescript_packages]]
name = "@tabula"
path = "../skill-sdk"
""")

            with self.assertRaisesRegex(ManifestError, "path must stay inside bundle"):
                load_bundle_manifest(root)

    def test_rejects_invalid_typescript_dependency_list(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            _write(root / "bundle.toml", """
[bundle]
name = "bad"

[[dependencies]]
bundle = "base"
typescript_packages = [1]
""")

            with self.assertRaisesRegex(ManifestError, r"typescript_packages must be list\[str\]"):
                load_bundle_manifest(root)


if __name__ == "__main__":
    unittest.main()

"""Plugin manifest enforcement at install time."""
from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from tabula_distro import install as installmod
from tabula_distro.plugin_manifest import (
    PluginManifestError,
    load_plugin_manifest,
)


def _touch(p: Path, content: str = "") -> None:
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(content, encoding="utf-8")


def _make_distro(root: Path, *, body: str) -> Path:
    dist = root / "demo"
    _touch(dist / "boot.py")
    (dist / "templates").mkdir(parents=True, exist_ok=True)
    _touch(dist / "distro.toml", body)
    return dist


def _make_plugin(root: Path, name: str, manifest_body: str) -> Path:
    d = root / name
    _touch(d / "plugin.toml", manifest_body)
    _touch(d / "run.py", "# placeholder\n")
    return d


def _seed_kernel(home: Path, *, version: str = "0.9.0",
                 proto_min: int = 1, proto_max: int = 1) -> None:
    home.mkdir(parents=True, exist_ok=True)
    (home / "VERSION").write_text(version + "\n", encoding="utf-8")
    (home / "PROTOCOL").write_text(
        json.dumps({"plugin_protocol_min": proto_min, "plugin_protocol_max": proto_max}) + "\n",
        encoding="utf-8",
    )


def _seed_sdk_in_bundle(bundle_root: Path, version: str = "0.1.0") -> None:
    """Place a fake tabula-plugin-sdk under ``<bundle>/_lib/python``."""
    init = bundle_root / "_lib" / "python" / "src" / "tabula_plugin_sdk" / "__init__.py"
    _touch(init, f'__version__ = "{version}"\n')


_VALID_REQUIRES = (
    '[plugin]\nid="hello"\nruntime="python"\nentry="run.py"\n'
    '[requires]\nkernel=">=0.9.0,<1.0.0"\nprotocol_version=1\n'
    'sdk="tabula-plugin-sdk>=0.1.0,<0.2.0"\n'
)


class PluginManifestParseTests(unittest.TestCase):
    def test_loads_valid(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            d = _make_plugin(root, "hello", _VALID_REQUIRES)
            m = load_plugin_manifest(d)
            self.assertIsNotNone(m.requires)
            self.assertEqual(m.requires.protocol_versions, (1,))
            self.assertEqual(m.requires.sdk.name, "tabula-plugin-sdk")

    def test_protocol_version_array(self):
        body = _VALID_REQUIRES.replace("protocol_version=1", "protocol_version=[1, 2]")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            d = _make_plugin(root, "hello", body)
            m = load_plugin_manifest(d)
            self.assertEqual(m.requires.protocol_versions, (1, 2))

    def test_missing_requires_block_tolerated(self):
        body = '[plugin]\nid="hello"\nruntime="python"\nentry="run.py"\n'
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            d = _make_plugin(root, "hello", body)
            m = load_plugin_manifest(d)
            self.assertIsNone(m.requires)

    def test_invalid_sdk_no_constraint(self):
        body = _VALID_REQUIRES.replace(
            'sdk="tabula-plugin-sdk>=0.1.0,<0.2.0"',
            'sdk="tabula-plugin-sdk"',
        )
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            d = _make_plugin(root, "hello", body)
            with self.assertRaises(PluginManifestError):
                load_plugin_manifest(d)

    def test_invalid_protocol_negative(self):
        body = _VALID_REQUIRES.replace("protocol_version=1", "protocol_version=0")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            d = _make_plugin(root, "hello", body)
            with self.assertRaises(PluginManifestError):
                load_plugin_manifest(d)


class PluginInstallCompatTests(unittest.TestCase):
    """End-to-end checks via installmod.install() with bundle-sourced plugin."""

    def _build(self, root: Path, *, plugin_body: str, sdk_version: str = "0.1.0",
                kernel_version: str = "0.9.0", proto_min: int = 1, proto_max: int = 1) -> Path:
        home = root / "home"
        _seed_kernel(home, version=kernel_version, proto_min=proto_min, proto_max=proto_max)

        bundle = root / "ext" / "kit"
        _make_plugin(bundle, "hello", plugin_body)
        _touch(bundle / "bundle.toml", '[bundle]\nname="kit"\nversion="0.1.0"\n[requires]\nkernel=">=0.9.0,<1.0.0"\n')
        _seed_sdk_in_bundle(bundle, sdk_version)

        distro = _make_distro(root, body=(
            '[distro]\nname="demo"\n'
            '[[bundles]]\nname="kit"\nsource="local:../ext/kit"\n'
        ))
        return home, distro

    def test_install_passes_with_compatible_plugin(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home, distro = self._build(root, plugin_body=_VALID_REQUIRES)
            installmod.install(distro, home)

    def test_install_fails_when_kernel_too_old(self):
        body = _VALID_REQUIRES.replace(
            'kernel=">=0.9.0,<1.0.0"',
            'kernel=">=2.0.0"',
        )
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home, distro = self._build(root, plugin_body=body)
            with self.assertRaises(installmod.InstallError) as cm:
                installmod.install(distro, home)
            self.assertIn("requires kernel", str(cm.exception))

    def test_install_fails_when_protocol_outside_kernel_range(self):
        body = _VALID_REQUIRES.replace("protocol_version=1", "protocol_version=2")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home, distro = self._build(root, plugin_body=body, proto_min=1, proto_max=1)
            with self.assertRaises(installmod.InstallError) as cm:
                installmod.install(distro, home)
            self.assertIn("protocol_version", str(cm.exception))

    def test_install_fails_when_sdk_out_of_range(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home, distro = self._build(root, plugin_body=_VALID_REQUIRES, sdk_version="0.2.5")
            with self.assertRaises(installmod.InstallError) as cm:
                installmod.install(distro, home)
            self.assertIn("tabula-plugin-sdk", str(cm.exception))

    def test_install_fails_when_protocol_file_absent(self):
        body = _VALID_REQUIRES
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            home.mkdir()
            (home / "VERSION").write_text("0.9.0\n", encoding="utf-8")
            # Note: NO PROTOCOL file.
            bundle = root / "ext" / "kit"
            _make_plugin(bundle, "hello", body)
            _touch(bundle / "bundle.toml", '[bundle]\nname="kit"\n[requires]\nkernel=">=0.9.0,<1.0.0"\n')
            _seed_sdk_in_bundle(bundle)
            distro = _make_distro(root, body=(
                '[distro]\nname="demo"\n'
                '[[bundles]]\nname="kit"\nsource="local:../ext/kit"\n'
            ))
            with self.assertRaises(installmod.InstallError) as cm:
                installmod.install(distro, home)
            self.assertIn("PROTOCOL", str(cm.exception))


if __name__ == "__main__":
    unittest.main()

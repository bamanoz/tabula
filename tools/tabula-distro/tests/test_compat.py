"""Tests for kernel/bundle compatibility enforcement at install time."""
from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from tabula_distro import install as installmod


def _touch(p: Path, content: str = "") -> None:
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(content, encoding="utf-8")


def _make_skill(root: Path, name: str) -> Path:
    d = root / name
    d.mkdir(parents=True, exist_ok=True)
    _touch(d / "SKILL.md", f"# {name}\n")
    return d


def _make_distro(root: Path, *, name: str = "demo", body: str) -> Path:
    dist = root / name
    (dist / "templates").mkdir(parents=True, exist_ok=True)
    (dist / "skills").mkdir(parents=True, exist_ok=True)
    _touch(dist / "distro.toml", body)
    return dist


def _write_kernel_version(home: Path, value: str | None) -> None:
    home.mkdir(parents=True, exist_ok=True)
    if value is None:
        return
    (home / "VERSION").write_text(value + "\n", encoding="utf-8")


class DistroKernelCompatTests(unittest.TestCase):
    def test_matching_kernel_passes(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _write_kernel_version(home, "0.8.0")
            distro = _make_distro(root, body=(
                '[distro]\nid="tabula.demo"\nname="demo"\nversion="0.1.0"\n'
                '[requires]\nkernel=">=0.8.0,<1.0.0"\n'
            ))
            lock = installmod.install(distro, home).lock
            self.assertEqual(lock.kernel_version, "0.8.0")
            self.assertEqual(lock.distro_version, "0.1.0")

    def test_too_old_kernel_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _write_kernel_version(home, "0.7.5")
            distro = _make_distro(root, body=(
                '[distro]\nid="tabula.demo"\nname="demo"\n[requires]\nkernel=">=0.8.0"\n'
            ))
            with self.assertRaises(installmod.InstallError) as cm:
                installmod.install(distro, home)
            self.assertIn("requires kernel", str(cm.exception))

    def test_missing_kernel_version_fails_when_required(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            home.mkdir()  # no VERSION file
            distro = _make_distro(root, body=(
                '[distro]\nid="tabula.demo"\nname="demo"\n[requires]\nkernel=">=0.8.0"\n'
            ))
            with self.assertRaises(installmod.InstallError):
                installmod.install(distro, home)

    def test_no_requirement_skips_check(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            home.mkdir()
            distro = _make_distro(root, body='[distro]\nid="tabula.demo"\nname="demo"\n')
            installmod.install(distro, home)  # no kernel VERSION file, no [requires] -> ok


class BundleKernelCompatTests(unittest.TestCase):
    def test_bundle_requires_kernel_passes(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _write_kernel_version(home, "0.8.0")

            bundle = root / "ext" / "drivers"
            _make_skill(bundle, "driver-anthropic")
            _touch(bundle / "bundle.toml", (
                '[bundle]\nname="drivers"\nversion="0.1.0"\n'
                '[requires]\nkernel=">=0.8.0,<1.0.0"\n'
            ))

            distro = _make_distro(root, body=(
                '[distro]\nid="tabula.demo"\nname="demo"\n'
                '[[bundles]]\nname="drivers"\nsource="local:../ext/drivers"\n'
            ))
            lock = installmod.install(distro, home).lock
            self.assertEqual(lock.bundles["drivers"].version, "0.1.0")

    def test_bundle_requires_kernel_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _write_kernel_version(home, "0.8.0")

            bundle = root / "ext" / "drivers"
            _make_skill(bundle, "driver-anthropic")
            _touch(bundle / "bundle.toml", (
                '[bundle]\nname="drivers"\nversion="0.2.0"\n'
                '[requires]\nkernel=">=1.0.0"\n'
            ))

            distro = _make_distro(root, body=(
                '[distro]\nid="tabula.demo"\nname="demo"\n'
                '[[bundles]]\nname="drivers"\nsource="local:../ext/drivers"\n'
            ))
            with self.assertRaises(installmod.InstallError) as cm:
                installmod.install(distro, home)
            self.assertIn("bundle 'drivers'", str(cm.exception))

    def test_bundle_without_manifest_skips_check(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            _write_kernel_version(home, "0.8.0")

            bundle = root / "ext" / "legacy"
            _make_skill(bundle, "skill-a")  # no bundle.toml

            distro = _make_distro(root, body=(
                '[distro]\nid="tabula.demo"\nname="demo"\n'
                '[[bundles]]\nname="legacy"\nsource="local:../ext/legacy"\n'
            ))
            lock = installmod.install(distro, home).lock
            self.assertIsNone(lock.bundles["legacy"].version)


if __name__ == "__main__":
    unittest.main()

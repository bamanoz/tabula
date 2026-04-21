#!/usr/bin/env python3
"""Tests for scripts/install-distro.py skill materialization behavior."""

from __future__ import annotations

import importlib.util
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
MODULE_PATH = ROOT / "scripts" / "install-distro.py"


def _load_module():
    spec = importlib.util.spec_from_file_location("tabula_install_distro", MODULE_PATH)
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(module)
    return module


install_distro = _load_module()


class TestInstallDistroBundles(unittest.TestCase):
    def test_materialize_linked_skills_flattens_bundle_skill_into_distro(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / "assistant"
            target = root / "installed-assistant"
            skills_dir = source / "skills"
            bundle_root = root / "bundles" / "memory"

            (skills_dir / "memory-save").parent.mkdir(parents=True, exist_ok=True)
            (target / "skills").mkdir(parents=True, exist_ok=True)
            bundle_root.mkdir(parents=True, exist_ok=True)
            (bundle_root / "version.txt").write_text("new\n", encoding="utf-8")
            (bundle_root / "_lib.py").write_text("HELPER = 1\n", encoding="utf-8")

            # The distro references a bundle via a symlink, like the real assistant distro.
            (skills_dir / "memory-save").symlink_to(bundle_root)

            installed_skill = target / "skills" / "memory-save"
            installed_skill.mkdir(parents=True, exist_ok=True)
            (installed_skill / "version.txt").write_text("old\n", encoding="utf-8")

            install_distro.materialize_linked_skills(source, target)

            self.assertFalse(installed_skill.is_symlink())
            self.assertEqual((installed_skill / "version.txt").read_text(encoding="utf-8"), "new\n")
            self.assertEqual((installed_skill / "_lib.py").read_text(encoding="utf-8"), "HELPER = 1\n")


if __name__ == "__main__":
    unittest.main()

#!/usr/bin/env python3
"""Smoke test that the legacy install-distro.py shim still works end-to-end.

Detailed unit tests live in tools/tabula-distro/tests/.
"""
from __future__ import annotations

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SHIM = ROOT / "scripts" / "install-distro.py"


def _make_distro(root: Path, name: str = "demo") -> Path:
    d = root / name
    (d / "skills").mkdir(parents=True, exist_ok=True)
    (d / "templates").mkdir(parents=True, exist_ok=True)
    (d / "boot.py").write_text("# boot\n", encoding="utf-8")
    (d / "templates" / "SYSTEM.md").write_text("hi\n", encoding="utf-8")
    return d


class LegacyShimTests(unittest.TestCase):
    def test_install_via_shim_creates_active_symlink(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_distro(root, "demo")
            res = subprocess.run(
                [sys.executable, str(SHIM), "--home", str(home), str(distro)],
                check=True, capture_output=True, text=True,
            )
            self.assertIn("installed distro demo", res.stdout)
            self.assertTrue((home / "boot.py").is_symlink())
            self.assertTrue((home / "distrib" / "active").is_symlink())


if __name__ == "__main__":
    unittest.main()

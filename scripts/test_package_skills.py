#!/usr/bin/env python3
from __future__ import annotations

import subprocess
import tarfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
PACKAGE_SKILLS = ROOT / "scripts" / "package-skills.sh"


class PackageSkillsTests(unittest.TestCase):
    def test_global_config_is_packaged_as_example(self) -> None:
        version = "test-global-config"
        archive = ROOT / "extra" / f"tabula-skills-{version}.tar.gz"
        try:
            subprocess.run([str(PACKAGE_SKILLS), version], cwd=ROOT, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True)
            with tarfile.open(archive, "r:gz") as tar:
                names = {item.name.removeprefix("./") for item in tar.getmembers()}
        finally:
            archive.unlink(missing_ok=True)

        self.assertIn("config/global.toml.example", names)
        self.assertNotIn("config/global.toml", names)
        self.assertFalse(any(".egg-info" in name for name in names))


if __name__ == "__main__":
    unittest.main()

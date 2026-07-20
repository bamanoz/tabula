#!/usr/bin/env python3
from __future__ import annotations

import os
import shutil
import subprocess
import tarfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
PACKAGE_SKILLS = ROOT / "scripts" / "package-skills.sh"


def _bash() -> str:
    if os.name == "nt":
        git_bash = Path("C:/Program Files/Git/bin/bash.exe")
        if git_bash.is_file():
            return str(git_bash)
    executable = shutil.which("bash")
    if not executable:
        raise RuntimeError("bash is required to test package-skills.sh")
    return executable


class PackageSkillsTests(unittest.TestCase):
    def test_global_config_is_packaged_as_example(self) -> None:
        version = "test-global-config"
        archive = ROOT / "extra" / f"tabula-skills-{version}.tar.gz"
        try:
            subprocess.run([_bash(), str(PACKAGE_SKILLS), version], cwd=ROOT, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True)
            with tarfile.open(archive, "r:gz") as tar:
                names = {item.name.removeprefix("./") for item in tar.getmembers()}
        finally:
            archive.unlink(missing_ok=True)

        self.assertIn("config/global.toml.example", names)
        self.assertIn("libexec/install_payload.py", names)
        self.assertNotIn("config/global.toml", names)
        self.assertFalse(any(".egg-info" in name for name in names))


if __name__ == "__main__":
    unittest.main()

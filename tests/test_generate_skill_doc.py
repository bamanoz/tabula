#!/usr/bin/env python3
"""Tests for scripts/generate-skill-doc.py."""

from __future__ import annotations

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts" / "generate-skill-doc.py"


class TestGenerateSkillDoc(unittest.TestCase):
    def test_generates_scaffold_with_configuration_table(self):
        with tempfile.TemporaryDirectory() as tmp:
            skill_dir = Path(tmp) / "demo-skill"
            skill_dir.mkdir(parents=True)
            (skill_dir / "SKILL.config.json").write_text(
                json.dumps(
                    {
                        "id": "demo-skill",
                        "config": {
                            "entries": [
                                {
                                    "key": "api_key",
                                    "type": "string",
                                    "secret": True,
                                    "required": True,
                                    "env": "TABULA_SKILL_DEMO_SKILL_API_KEY",
                                    "env_aliases": ["DEMO_API_KEY"],
                                    "store_id": "demo-skill.api_key",
                                },
                                {
                                    "key": "model",
                                    "type": "string",
                                    "default": "demo-model",
                                    "env": "TABULA_SKILL_DEMO_SKILL_MODEL",
                                },
                            ]
                        },
                    },
                    indent=2,
                ),
                encoding="utf-8",
            )
            (skill_dir / "SKILL.md").write_text(
                "---\nname: demo-skill\ndescription: \"Demo description\"\n---\n\n# Demo Skill\n",
                encoding="utf-8",
            )

            result = subprocess.check_output([sys.executable, str(SCRIPT), str(skill_dir)], text=True)

            self.assertIn("# Demo Skill", result)
            self.assertIn("## Run", result)
            self.assertIn("## Config File", result)
            self.assertIn("## Secrets", result)
            self.assertIn("## Configuration", result)
            self.assertIn("## Runtime Environment", result)
            self.assertIn("## Precedence", result)
            self.assertIn("`api_key`", result)
            self.assertIn("`TABULA_SKILL_DEMO_SKILL_API_KEY`", result)
            self.assertIn("`DEMO_API_KEY`", result)
            self.assertIn("store id: `demo-skill.api_key`", result)

    def test_writes_output_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            skill_dir = Path(tmp) / "demo-skill"
            skill_dir.mkdir(parents=True)
            (skill_dir / "SKILL.config.json").write_text(
                json.dumps({"id": "demo-skill", "config": {"entries": []}}, indent=2),
                encoding="utf-8",
            )
            out_path = skill_dir / "SKILL.generated.md"

            subprocess.check_call([sys.executable, str(SCRIPT), str(skill_dir), "--output", str(out_path)])

            self.assertTrue(out_path.is_file())
            content = out_path.read_text(encoding="utf-8")
            self.assertIn("# demo-skill", content)
            self.assertIn("## Configuration", content)


if __name__ == "__main__":
    unittest.main()

#!/usr/bin/env python3
"""Tests for shared provider selection logic."""

from __future__ import annotations

import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in os.sys.path:
    os.sys.path.insert(0, str(ROOT))

from skills.lib.provider_selection import (
    ProviderSelectionError,
    build_driver_command,
    resolve_driver_command,
    resolve_provider,
)


class TestProviderSelection(unittest.TestCase):
    def _make_driver(self, home: Path, provider: str):
        driver = home / "skills" / f"driver-{provider}" / "run.py"
        driver.parent.mkdir(parents=True, exist_ok=True)
        driver.write_text("#!/usr/bin/env python3\n", encoding="utf-8")
        api_prefix = "OPENAI" if provider == "openai" else provider.upper()
        (driver.parent / "SKILL.config.json").write_text(
            json.dumps(
                {
                    "id": f"driver-{provider}",
                    "config": {
                        "entries": [
                            {
                                "key": "api_key",
                                "type": "string",
                                "secret": True,
                                "required": True,
                                "env": f"TABULA_SKILL_DRIVER_{provider.upper()}_API_KEY",
                                "env_aliases": [f"{api_prefix}_API_KEY"],
                                "store_id": f"driver-{provider}.api_key",
                            }
                        ]
                    },
                }
            ),
            encoding="utf-8",
        )

    def test_resolve_provider_normalizes_alias(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            self._make_driver(home, "openai")

            provider = resolve_provider("gpt", tabula_home=home, require_ready=False)

            self.assertEqual(provider, "openai")

    def test_resolve_provider_requires_installed_driver(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            with self.assertRaises(ProviderSelectionError) as ctx:
                resolve_provider("openai", tabula_home=home, require_ready=False)

            self.assertIn("not installed", str(ctx.exception))

    def test_resolve_provider_requires_ready_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            self._make_driver(home, "openai")

            with patch.dict(os.environ, {}, clear=True):
                with self.assertRaises(ProviderSelectionError) as ctx:
                    resolve_provider("openai", tabula_home=home, require_ready=True)

            self.assertIn("not configured", str(ctx.exception))

    def test_resolve_provider_requires_configured_default_when_not_explicit(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            self._make_driver(home, "anthropic")

            with self.assertRaises(ProviderSelectionError) as ctx:
                resolve_provider(None, tabula_home=home, require_ready=False)

            self.assertIn("no provider configured", str(ctx.exception))

    def test_resolve_driver_command_uses_ready_provider(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            self._make_driver(home, "openai")
            (home / "config").mkdir(parents=True)
            (home / "config" / "global.toml").write_text(
                '[driver.openai]\nmodel = "gpt-5.4"\nbase_url = "https://api.openai.com/v1"\napi_key = { source = "store", id = "driver-openai.api_key" }\n',
                encoding="utf-8",
            )
            (home / "secrets.json").write_text(json.dumps({"driver-openai.api_key": "sk-openai"}) + "\n", encoding="utf-8")

            provider, command = resolve_driver_command("openai", tabula_home=home, python_executable="python3")

            self.assertEqual(provider, "openai")
            self.assertIn("python3", command)
            self.assertIn(str(home / "skills" / "driver-openai" / "run.py"), command)

    def test_build_driver_command_reports_missing_script(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            with self.assertRaises(ProviderSelectionError):
                build_driver_command("anthropic", tabula_home=home, python_executable="python3")


if __name__ == "__main__":
    unittest.main()

from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

import tomllib

from tabula_distro import runtime_config


class RuntimeConfigWriteTests(unittest.TestCase):
    def test_initial_write_produces_valid_toml(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            path = runtime_config.write(home, ["/p/a", "/p/b"])
            data = tomllib.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(data["plugin_dirs"], ["/p/a", "/p/b"])
            self.assertEqual(data["skill_dirs"], [str(home / "skills")])
            self.assertEqual(len(data["kernel"]), 1)
            self.assertEqual(data["kernel"][0]["id"], "main")
            self.assertEqual(data["kernel"][0]["tenants"], ["*"])
            self.assertEqual(data["pool"]["cold_workers_per_tenant_max"], 16)

    def test_rewrite_preserves_user_comments_and_unknown_sections(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            path = runtime_config.write(home, ["/p/a"])
            # User adds comments and a section we do not own.
            existing = path.read_text(encoding="utf-8")
            doctored = (
                "# user-owned: keep me\n"
                "# trailing trivia\n"
                + existing
                + "\n[my_custom]\nflag = true\n"
            )
            path.write_text(doctored, encoding="utf-8")

            # Reinstall with a different plugin list.
            runtime_config.write(home, ["/p/a", "/p/b"])

            result = path.read_text(encoding="utf-8")
            self.assertIn("# user-owned: keep me", result)
            self.assertIn("# trailing trivia", result)
            self.assertIn("[my_custom]", result)
            self.assertIn("flag = true", result)

            data = tomllib.loads(result)
            self.assertEqual(data["plugin_dirs"], ["/p/a", "/p/b"])
            self.assertTrue(data["my_custom"]["flag"])

    def test_rewrite_updates_owned_keys(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            runtime_config.write(home, ["/old"])
            path = runtime_config.write(home, ["/new1", "/new2"])
            data = tomllib.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(data["plugin_dirs"], ["/new1", "/new2"])
            # Still exactly one kernel entry, not appended.
            self.assertEqual(len(data["kernel"]), 1)

    def test_default_plugin_dir_when_payload_empty(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            path = runtime_config.write(home, [str(home / "plugins")])
            data = tomllib.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(data["plugin_dirs"], [str(home / "plugins")])


if __name__ == "__main__":
    unittest.main()

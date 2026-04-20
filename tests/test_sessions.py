#!/usr/bin/env python3
"""Unit tests for the sessions skill."""

from __future__ import annotations

import importlib.util
import os
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SESSIONS_PATH = ROOT / "distrib" / "assistant" / "skills" / "sessions" / "run.py"

if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))


def _load_sessions_module():
    return _load_sessions_module_for_home(str(ROOT))


def _load_sessions_module_for_home(home: str, extra_env: dict[str, str] | None = None):
    env = {"TABULA_HOME": home}
    if extra_env:
        env.update(extra_env)
    with unittest.mock.patch.dict(os.environ, env, clear=True):
        spec = importlib.util.spec_from_file_location("tabula_sessions_run", SESSIONS_PATH)
        mod = importlib.util.module_from_spec(spec)
        assert spec.loader is not None
        spec.loader.exec_module(mod)
        return mod


class TestSessionRegistry(unittest.TestCase):
    def test_emit_uses_message_protocol_type(self):
        mod = _load_sessions_module()

        class FakeConn:
            def __init__(self):
                self.sent = []

            def send(self, msg):
                self.sent.append(msg)

        registry = mod.SessionRegistry()
        registry.conn = FakeConn()

        registry._emit("created", "s1", {"clients": ["driver"]})

        self.assertEqual(len(registry.conn.sent), 1)
        self.assertEqual(registry.conn.sent[0]["type"], mod.MSG_MESSAGE)
        self.assertEqual(registry.conn.sent[0]["session"], "_system")


class TestSessionsConfigImport(unittest.TestCase):
    def test_module_import_reads_skill_config_defaults(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            cfg_dir = home / "config"
            cfg_dir.mkdir(parents=True)
            (cfg_dir / "global.toml").write_text('[sessions]\nidle_timeout = 123\npoll_interval = 4.5\n', encoding='utf-8')

            mod = _load_sessions_module_for_home(str(home))

            self.assertEqual(mod.IDLE_TIMEOUT, 123)
            self.assertEqual(mod.POLL_INTERVAL, 4.5)

    def test_module_import_env_overrides_skill_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            cfg_dir = home / "config"
            cfg_dir.mkdir(parents=True)
            (cfg_dir / "global.toml").write_text('[sessions]\nidle_timeout = 123\npoll_interval = 4.5\n', encoding='utf-8')

            mod = _load_sessions_module_for_home(
                str(home),
                {
                    "TABULA_SKILL_SESSIONS_IDLE_TIMEOUT": "10",
                    "TABULA_SESSION_POLL_SEC": "1.25",
                },
            )

            self.assertEqual(mod.IDLE_TIMEOUT, 10)
            self.assertEqual(mod.POLL_INTERVAL, 1.25)


if __name__ == "__main__":
    unittest.main()

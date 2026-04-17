#!/usr/bin/env python3
"""Unit tests for the sessions skill."""

from __future__ import annotations

import importlib.util
import os
import sys
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SESSIONS_PATH = ROOT / "skills" / "sessions" / "run.py"

if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))


def _load_sessions_module():
    old_home = os.environ.get("TABULA_HOME")
    os.environ["TABULA_HOME"] = str(ROOT)
    try:
        spec = importlib.util.spec_from_file_location("tabula_sessions_run", SESSIONS_PATH)
        mod = importlib.util.module_from_spec(spec)
        assert spec.loader is not None
        spec.loader.exec_module(mod)
        return mod
    finally:
        if old_home is None:
            os.environ.pop("TABULA_HOME", None)
        else:
            os.environ["TABULA_HOME"] = old_home


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


if __name__ == "__main__":
    unittest.main()

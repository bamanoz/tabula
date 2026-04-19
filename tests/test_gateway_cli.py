#!/usr/bin/env python3
"""Tests for gateway-cli driver startup behavior."""

from __future__ import annotations

import importlib.util
import os
import sys
import unittest
from pathlib import Path
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[1]
GATEWAY_CLI_PATH = ROOT / "skills" / "gateway-cli" / "run.py"

if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))


def _load_gateway_cli_module():
    spec = importlib.util.spec_from_file_location("tabula_gateway_cli_run", GATEWAY_CLI_PATH)
    mod = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(mod)
    return mod


class _FakeConn:
    def __init__(self, responses):
        self.responses = list(responses)
        self.sent = []

    def send(self, msg):
        self.sent.append(msg)

    def recv(self, timeout=None):
        if self.responses:
            return self.responses.pop(0)
        return None


class TestGatewayCliDriverSpawn(unittest.TestCase):
    def test_spawn_driver_requires_member_joined_after_pid(self):
        mod = _load_gateway_cli_module()
        with patch.object(mod, "KernelConnection", return_value=_FakeConn([])):
            gateway = mod.Gateway(driver_cmd="python3 skills/driver-anthropic/run.py")
        gateway.session_id = "sess-test"
        gateway.conn = _FakeConn([
            {"type": mod.MSG_TOOL_RESULT, "id": "spawn-driver", "output": "PID 12345"},
            None,
        ])

        with self.assertRaises(RuntimeError) as ctx:
            gateway._spawn_driver()

        self.assertIn("did not join session", str(ctx.exception))
        self.assertEqual(gateway.driver_pid, 12345)

    def test_spawn_driver_succeeds_after_pid_and_member_joined(self):
        mod = _load_gateway_cli_module()
        with patch.object(mod, "KernelConnection", return_value=_FakeConn([])):
            gateway = mod.Gateway(driver_cmd="python3 skills/driver-anthropic/run.py")
        gateway.session_id = "sess-test"
        gateway.conn = _FakeConn([
            {"type": mod.MSG_TOOL_RESULT, "id": "spawn-driver", "output": "PID 12345"},
            {"type": mod.MSG_MEMBER_JOINED},
        ])

        gateway._spawn_driver()

        self.assertEqual(gateway.driver_pid, 12345)


if __name__ == "__main__":
    unittest.main()

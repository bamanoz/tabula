#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import tomllib
import unittest


class DriverConfigSmoke(unittest.TestCase):
    tabula_home = ""

    def test_installed_driver_sdk_reads_nested_global_config(self):
        home = Path(self.tabula_home)
        global_config = home / "config" / "global.toml"
        global_config.parent.mkdir(parents=True, exist_ok=True)
        global_config.write_text(
            """
[plugins.driver]
provider = "openai"

[plugins.driver.providers.openai]
api_key = "test-key"
base_url = "https://api.openai.com/v1"
default_model = "gpt-5.4"
api = "responses"

[plugins.driver.providers.openai.models."gpt-5.4"]
effort = "xhigh"
reasoning_summary = "auto"

[plugins.driver.providers.openai.models."gpt-5.4-mini"]
effort = "low"
""".strip()
            + "\n",
            encoding="utf-8",
        )
        python = home / ".venv" / "bin" / "python3"
        if not python.is_file():
            python = Path(sys.executable)
        code = """
import json
from tabula_driver_sdk import driver_catalog
from tabula_drivers.provider_factory import load_provider_settings

catalog = driver_catalog()
settings = load_provider_settings('openai')
print(json.dumps({
    'catalog': catalog,
    'settings': {
        'model': settings.model,
        'api': settings.api,
        'reasoning_effort': settings.reasoning_effort,
        'reasoning_summary': settings.reasoning_summary,
    },
}, sort_keys=True))
"""
        env = os.environ.copy()
        env["TABULA_HOME"] = str(home)
        result = subprocess.run([str(python), "-c", code], env=env, text=True, capture_output=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stderr)
        payload = json.loads(result.stdout)
        self.assertEqual(payload["catalog"]["default_provider"], "openai")
        self.assertIn({"id": "xhigh"}, payload["catalog"]["efforts"])
        openai = next(item for item in payload["catalog"]["providers"] if item["id"] == "openai")
        self.assertEqual(openai["default_model"], "gpt-5.4")
        self.assertEqual(next(item for item in openai["models"] if item["id"] == "gpt-5.4")["default_effort"], "xhigh")
        self.assertEqual(payload["settings"]["model"], "gpt-5.4")
        self.assertEqual(payload["settings"]["api"], "responses")
        self.assertEqual(payload["settings"]["reasoning_effort"], "xhigh")
        self.assertEqual(payload["settings"]["reasoning_summary"], "auto")

    def test_installed_driver_worker_protocol_lifecycle(self):
        home = Path(self.tabula_home)
        plugin_dir = home / "plugins" / "driver"
        manifest = tomllib.loads((plugin_dir / "plugin.toml").read_text(encoding="utf-8"))
        self.assertEqual(manifest["kind"], {"name": "driver", "singleton": True})
        self.assertEqual(manifest["worker"], {"command": ["python3", "run.py"], "mode": "warm", "scope": "session"})

        python = home / ".venv" / "bin" / "python3"
        if not python.is_file():
            python = Path(sys.executable)
        init = {
            "op": "init",
            "kernel_id": "testbed-kernel",
            "tenant_id": "testbed",
            "session_id": "test-session",
            "target_id": "driver",
            "agent_spec_revision": "sha256:testbed-agent",
            "desired_generation": 1,
            "driver_instance_id": "testbed-driver-instance",
            "manifest": manifest,
            "env": {},
        }
        frames = "".join(json.dumps(frame, separators=(",", ":")) + "\n" for frame in (init, {"op": "shutdown", "reason": "test complete"}))
        env = os.environ.copy()
        env["TABULA_HOME"] = str(home)
        result = subprocess.run(
            [str(python), "run.py"],
            cwd=plugin_dir,
            env=env,
            input=frames,
            text=True,
            capture_output=True,
            timeout=30,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        output = [json.loads(line) for line in result.stdout.splitlines()]
        self.assertEqual([frame["op"] for frame in output], ["init_ack", "register"])
        self.assertTrue(output[0]["ready"])
        self.assertEqual(output[1]["session_id"], "test-session")
        self.assertEqual(output[1]["desired_generation"], 1)

    def test_installed_driver_loop_repair_helpers(self):
        home = Path(self.tabula_home)
        python = home / ".venv" / "bin" / "python3"
        if not python.is_file():
            python = Path(sys.executable)
        code = r'''
import json
from tabula_drivers.provider_history_repair import repair_openai_chat_messages
from tabula_drivers.tool_input_repair import parse_tool_input
from tabula_drivers.turn_exit import final_response_exit
from tabula_drivers.turn_retry_state import TurnRetryState

tool_input = parse_tool_input('{"path":"README.md",}')
messages = [
    {"role": "assistant", "content": None, "tool_calls": [{"id": "call-1", "type": "function", "function": {"name": "fs_read", "arguments": '{"path":"README.md",}'}}]},
    {"role": "tool", "tool_call_id": "missing", "content": "orphan"},
]
events = repair_openai_chat_messages(messages)
retry_state = TurnRetryState()
retry_state.mark_partial_tool_stream_recovery()
exit_diag = final_response_exit(final_text='', tool_calls=[])
print(json.dumps({
    "tool_input": tool_input.input,
    "tool_input_reason": tool_input.reason,
    "events": events,
    "messages": messages,
    "retry_state": retry_state.to_dict(),
    "turn_exit": exit_diag.to_event() if exit_diag else {},
}, sort_keys=True))
'''
        result = subprocess.run([str(python), "-c", code], text=True, capture_output=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stderr)
        payload = json.loads(result.stdout)
        self.assertEqual(payload["tool_input"], {"path": "README.md"})
        self.assertEqual(payload["tool_input_reason"], "trailing_comma")
        self.assertTrue(payload["retry_state"]["partial_tool_stream_recovery_used"])
        self.assertEqual(payload["turn_exit"]["reason"], "empty_response")
        self.assertEqual(payload["messages"][0]["tool_calls"][0]["function"]["arguments"], '{"path":"README.md"}')
        self.assertTrue(any(event["kind"] == "drop_stray_tool_result" for event in payload["events"]))


def main() -> int:
    parser = argparse.ArgumentParser(description="Run driver config testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    DriverConfigSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(DriverConfigSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

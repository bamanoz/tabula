#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import sys
import unittest

from tabula_testbed import TestbedClient


RESULT = {
    "outcome": {"summary": "Installed reflection completed", "status": "succeeded"},
    "lessons": [{"summary": "Use installed orchestration"}],
    "patterns": [{"summary": "Persist structured reflection"}],
    "mistakes": [],
    "follow_ups": [{"summary": "Review proposal manually"}],
    "memory_candidates": [{"summary": "Candidate only", "reason": "No hidden mutation"}],
    "identity_candidates": [],
}


class ReflectionInstalled(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def configure_fake_acp(self) -> None:
        home = Path(self.tabula_home)
        script = home / "run" / "reflection-fake-acp.py"
        script.parent.mkdir(parents=True, exist_ok=True)
        result_json = json.dumps(RESULT, ensure_ascii=False)
        script.write_text(
            f'''import json
import sys
import uuid

RESULT = {result_json!r}
for raw in sys.stdin:
    message = json.loads(raw)
    method = message.get("method")
    if method == "initialize":
        result = {{"protocolVersion": 1, "agentInfo": {{"name": "reflection-fixture", "version": "1"}}, "agentCapabilities": {{}}}}
    elif method == "session/new":
        result = {{"sessionId": "reflection-" + uuid.uuid4().hex[:8]}}
    elif method == "session/prompt":
        update = {{
            "jsonrpc": "2.0",
            "method": "session/update",
            "params": {{
                "sessionId": "fixture",
                "update": {{
                    "sessionUpdate": "agent_message_chunk",
                    "content": {{"type": "text", "text": RESULT}},
                }},
            }},
        }}
        sys.stdout.write(json.dumps(update) + "\\n")
        sys.stdout.flush()
        result = {{"stopReason": "end_turn"}}
    else:
        result = {{}}
    sys.stdout.write(json.dumps({{"jsonrpc": "2.0", "id": message.get("id"), "result": result}}) + "\\n")
    sys.stdout.flush()
''',
            encoding="utf-8",
        )
        config = home / "tenants" / "default" / "config" / "plugins" / "reflection" / "config.toml"
        config.parent.mkdir(parents=True, exist_ok=True)
        command = f"{sys.executable} {script}"
        config.write_text(
            f'subagent_type = "acp"\nacp_command = {json.dumps(command)}\ntimeout_seconds = 30\n',
            encoding="utf-8",
        )

    def test_installed_reflection_runs_subagent_and_reads_persisted_result(self) -> None:
        home = Path(self.tabula_home)
        self.assertTrue((home / "plugins" / "reflection" / "plugin.toml").is_file())
        self.assertTrue((home / "packages" / "python" / "src" / "tabula_reflection_sdk").is_dir())
        for excluded in ("continuity", "activity", "initiative", "evolution", "mempalace"):
            self.assertFalse((home / "plugins" / excluded).exists(), f"reflection suite must not install {excluded}")
        self.configure_fake_acp()

        with TestbedClient(self.url, name="reflection-installed") as client:
            client.connect_join("reflection-installed")
            client.wait_tools({"reflection_run", "reflection_get", "reflection_list", "reflection_artifact_read"}, session="reflection-installed")
            request = {
                "request_id": "installed-reflection",
                "task_goal": "Complete installed reflection test",
                "session_source": {"session": "reflection-installed", "transcript": "Work completed and tests passed."},
                "evidence": [{"kind": "test", "summary": "installed tool executed"}],
                "reviewer_findings": [{"severity": "low", "summary": "retain proposal boundary"}],
            }
            first = client.call_tool("reflection_run", request, timeout=90).json()
            self.assertNotIn("error", first, first)
            record = first["reflection"]
            self.assertEqual(record["status"], "completed")
            self.assertEqual(record["result"]["outcome"]["summary"], "Installed reflection completed")
            self.assertEqual(record["result_artifact"]["owner_plugin"], "reflection")

            duplicate = client.call_tool("reflection_run", request, timeout=20).json()
            self.assertTrue(duplicate["cached"])
            fetched = client.call_tool(
                "reflection_get", {"request_id": "installed-reflection"}, timeout=20
            ).json()["reflection"]
            self.assertEqual(fetched["result"], record["result"])
            listed = client.call_tool("reflection_list", {"status": "completed"}, timeout=20).json()
            self.assertEqual([item["request_id"] for item in listed["items"]], ["installed-reflection"])
            raw = client.call_tool(
                "reflection_artifact_read", {"ref": record["result_artifact"]["ref"]}, timeout=20
            ).json()
            self.assertEqual(json.loads(raw["content"]), RESULT)

        state = home / "tenants" / "default" / "state" / "plugins" / "reflection"
        self.assertTrue((state / "requests" / "installed-reflection.json").is_file())
        self.assertEqual(len((state / "patterns.jsonl").read_text(encoding="utf-8").splitlines()), 1)
        self.assertEqual(len((state / "proposals.jsonl").read_text(encoding="utf-8").splitlines()), 2)


def main() -> int:
    parser = argparse.ArgumentParser(description="Run installed reflection capability test")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    ReflectionInstalled.url = args.url
    ReflectionInstalled.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(
        unittest.defaultTestLoader.loadTestsFromTestCase(ReflectionInstalled)
    )
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

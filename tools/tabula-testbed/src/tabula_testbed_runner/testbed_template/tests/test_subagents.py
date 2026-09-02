#!/usr/bin/env python3
from __future__ import annotations

import argparse
import time
import os
from pathlib import Path
import json
import signal
import subprocess
import sys
import tomllib
import unittest

from tabula_testbed import TestbedClient


FAKE_ACP = """#!/usr/bin/env python3
import json
import os
import sys
from uuid import uuid4

session_id = None

for raw in sys.stdin:
    if not raw.strip():
        continue
    message = json.loads(raw)
    method = message.get('method')
    if method == 'initialize':
        payload = {
            'jsonrpc': '2.0',
            'id': message['id'],
            'result': {
                'protocolVersion': 1,
                'agentInfo': {'name': 'fake-acp', 'version': '0.1.0'},
                'agentCapabilities': {'loadSession': True, 'promptCapabilities': {'embeddedContext': True}, 'sessionCapabilities': {'close': {}, 'list': {}, 'resume': {}}},
                'authMethods': [],
            },
        }
        sys.stdout.write(json.dumps(payload) + '\\n')
        sys.stdout.flush()
    elif method == 'session/new':
        session_id = f'acp-{uuid4().hex[:8]}'
        payload = {'jsonrpc': '2.0', 'id': message['id'], 'result': {'sessionId': session_id}}
        sys.stdout.write(json.dumps(payload) + '\\n')
        sys.stdout.flush()
    elif method == 'session/prompt':
        params = message.get('params') or {}
        prompt = params.get('prompt') or []
        text = ''
        for block in prompt:
            if isinstance(block, dict) and block.get('type') == 'text':
                text += str(block.get('text') or '')
        for line in text.splitlines():
            if line.startswith('CAPTURE_PATH='):
                capture_path = line.split('=', 1)[1].strip()
                with open(capture_path, 'w', encoding='utf-8') as fh:
                    json.dump({'cwd': os.getcwd(), 'project_root': os.environ.get('TABULA_PROJECT_ROOT'), 'prompt': text}, fh)
            elif line.startswith('MUTATE_FILE='):
                target = line.split('=', 1)[1].strip()
                with open(target, 'w', encoding='utf-8') as fh:
                    fh.write('child\\n')
        response = f'FAKE ACP: {text}'
        note = {'jsonrpc': '2.0', 'method': 'session/update', 'params': {'sessionId': params.get('sessionId'), 'update': {'sessionUpdate': 'agent_message_chunk', 'content': {'type': 'text', 'text': response}}}}
        sys.stdout.write(json.dumps(note) + '\\n')
        sys.stdout.write(json.dumps({'jsonrpc': '2.0', 'id': message['id'], 'result': {'stopReason': 'end_turn'}}) + '\\n')
        sys.stdout.flush()
    elif method in {'session/cancel', 'session/close'}:
        sys.stdout.write(json.dumps({'jsonrpc': '2.0', 'id': message['id'], 'result': {}}) + '\\n')
        sys.stdout.flush()
    else:
        sys.stdout.write(json.dumps({'jsonrpc': '2.0', 'id': message.get('id'), 'error': {'code': -32601, 'message': f'method not found: {method}'}}) + '\\n')
        sys.stdout.flush()
"""


ONE_SHOT_ACP = """#!/usr/bin/env python3
import json
import sys
from uuid import uuid4

session_id = None

for raw in sys.stdin:
    if not raw.strip():
        continue
    message = json.loads(raw)
    method = message.get('method')
    if method == 'initialize':
        sys.stdout.write(json.dumps({'jsonrpc': '2.0', 'id': message['id'], 'result': {'protocolVersion': 1, 'agentInfo': {'name': 'one-shot-acp', 'version': '0.1.0'}, 'agentCapabilities': {'loadSession': True, 'promptCapabilities': {'embeddedContext': True}, 'sessionCapabilities': {'close': {}, 'list': {}, 'resume': {}}}, 'authMethods': []}}) + '\\n')
        sys.stdout.flush()
    elif method == 'session/new':
        session_id = f'acp-{uuid4().hex[:8]}'
        sys.stdout.write(json.dumps({'jsonrpc': '2.0', 'id': message['id'], 'result': {'sessionId': session_id}}) + '\\n')
        sys.stdout.flush()
    elif method == 'session/prompt':
        params = message.get('params') or {}
        text = ''.join(str(block.get('text') or '') for block in (params.get('prompt') or []) if isinstance(block, dict) and block.get('type') == 'text')
        response = f'FAKE ACP: {text}'
        sys.stdout.write(json.dumps({'jsonrpc': '2.0', 'method': 'session/update', 'params': {'sessionId': params.get('sessionId'), 'update': {'sessionUpdate': 'agent_message_chunk', 'content': {'type': 'text', 'text': response}}}}) + '\\n')
        sys.stdout.write(json.dumps({'jsonrpc': '2.0', 'id': message['id'], 'result': {'stopReason': 'end_turn'}}) + '\\n')
        sys.stdout.flush()
        break
    elif method in {'session/cancel', 'session/close'}:
        sys.stdout.write(json.dumps({'jsonrpc': '2.0', 'id': message['id'], 'result': {}}) + '\\n')
        sys.stdout.flush()
"""


HANGING_ACP = """#!/usr/bin/env python3
import json
import sys
import time
from uuid import uuid4

session_id = None

for raw in sys.stdin:
    if not raw.strip():
        continue
    message = json.loads(raw)
    method = message.get('method')
    if method == 'initialize':
        sys.stdout.write(json.dumps({
            'jsonrpc': '2.0',
            'id': message['id'],
            'result': {
                'protocolVersion': 1,
                'agentInfo': {'name': 'hanging-acp', 'version': '0.1.0'},
                'agentCapabilities': {'loadSession': True, 'promptCapabilities': {'embeddedContext': True}, 'sessionCapabilities': {'close': {}, 'list': {}, 'resume': {}}},
                'authMethods': [],
            },
        }) + '\\n')
        sys.stdout.flush()
    elif method == 'session/new':
        session_id = f'acp-{uuid4().hex[:8]}'
        sys.stdout.write(json.dumps({'jsonrpc': '2.0', 'id': message['id'], 'result': {'sessionId': session_id}}) + '\\n')
        sys.stdout.flush()
    elif method == 'session/prompt':
        time.sleep(300)
    elif method in {'session/cancel', 'session/close'}:
        sys.stdout.write(json.dumps({'jsonrpc': '2.0', 'id': message['id'], 'result': {}}) + '\\n')
        sys.stdout.flush()
    else:
        sys.stdout.write(json.dumps({'jsonrpc': '2.0', 'id': message.get('id'), 'error': {'code': -32601, 'message': f'method not found: {method}'}}) + '\\n')
        sys.stdout.flush()
"""


class SubagentsPluginSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def make_client(self, name: str) -> TestbedClient:
        client = TestbedClient(self.url, name=name)
        client.connect()
        client.create_session("testbed-subagents")
        return client

    @classmethod
    def tabula_bin(cls) -> str:
        candidate = Path(cls.tabula_home) / "bin" / ("tabula.exe" if os.name == "nt" else "tabula")
        return str(candidate) if candidate.is_file() else "tabula"

    @classmethod
    def ensure_tenant(cls, tenant_id: str) -> None:
        env = os.environ.copy()
        env["TABULA_HOME"] = cls.tabula_home
        subprocess.run([cls.tabula_bin(), "tenant", "create", tenant_id, "--exists-ok"], env=env, check=True, timeout=30, stdout=subprocess.DEVNULL)

    def make_git_repo(self, name: str) -> Path:
        repo = Path(self.tabula_home) / "data" / "testbed" / name
        repo.mkdir(parents=True, exist_ok=True)
        subprocess.run(["git", "init"], cwd=repo, check=True, timeout=30, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        subprocess.run(["git", "config", "user.email", "test@example.com"], cwd=repo, check=True, timeout=30, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        subprocess.run(["git", "config", "user.name", "Test"], cwd=repo, check=True, timeout=30, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        (repo / "file.txt").write_text("parent\n", encoding="utf-8")
        subprocess.run(["git", "add", "file.txt"], cwd=repo, check=True, timeout=30, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        subprocess.run(["git", "commit", "-m", "init"], cwd=repo, check=True, timeout=30, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        return repo

    def python_bin(self) -> Path:
        home = Path(self.tabula_home)
        candidates = [
            home / ".venv" / ("Scripts" if os.name == "nt" else "bin") / ("python.exe" if os.name == "nt" else "python3"),
            home / ".venv" / "bin" / "python3",
            home / ".venv" / "Scripts" / "python.exe",
            Path(sys.executable),
        ]
        for candidate in candidates:
            if candidate.is_file():
                return candidate
        return Path(sys.executable)

    def fake_acp_command(self, name: str, script: str = FAKE_ACP) -> list[str]:
        home = Path(self.tabula_home)
        fake_acp = home / "data" / "testbed" / name
        fake_acp.parent.mkdir(parents=True, exist_ok=True)
        fake_acp.write_text(script, encoding="utf-8")
        return [str(self.python_bin()), str(fake_acp)]

    def plugin_cli(self, *args: str) -> dict:
        env = os.environ.copy()
        env["TABULA_HOME"] = self.tabula_home
        env["TABULA_TENANT_ID"] = "default"
        env["TABULA_TENANT_DIR"] = str(Path(self.tabula_home) / "tenants" / "default")
        run_py = Path(self.tabula_home) / "plugins" / "subagents" / "run.py"
        result = subprocess.run(
            [str(self.python_bin()), str(run_py), *args],
            env=env,
            check=True,
            timeout=30,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        return json.loads(result.stdout)

    def test_subagents_plugin_and_client_are_installed(self):
        home = Path(self.tabula_home)
        self.assertTrue((home / "plugins" / "subagents" / "plugin.toml").is_file())
        self.assertTrue((home / "apps" / "subagent" / "app.toml").is_file())
        self.assertTrue((home / "apps" / "subagent-acp" / "app.toml").is_file())
        self.assertFalse((home / "skills" / "_subagent_types").exists())
        self.assertFalse((home / "plugins" / "_subagent_types").exists())
        self.assertFalse((home / "state" / "subagents").exists())
        self.assertFalse((home / "logs" / "subagents").exists())
        self.assertTrue((home / "plugins" / "subagents" / "types" / "general.toml").is_file())

    def test_subagents_tools_are_plugin_tools(self):
        with self.make_client("testbed-subagents") as client:
            required = {"subagent_spawn", "subagent_batch", "subagent_batch_wait", "subagent_send", "subagent_steer", "subagent_wait", "subagent_list", "subagent_kill"}

            listed = client.call_tool("subagent_list", {}, timeout=10).json()
            self.assertIn("items", listed)

    def test_subagent_list_defaults_to_runtime_context_session(self):
        home = Path(self.tabula_home)
        registry = home / "tenants" / "default" / "state" / "plugins" / "subagents"
        registry.mkdir(parents=True, exist_ok=True)
        (registry / "sa-context-list.json").write_text(json.dumps({
            "version": 1,
            "id": "sa-context-list",
            "session": "subagent-sa-context-list",
            "parent_session": "testbed-subagents-context",
            "status": "running",
            "pid": os.getpid(),
            "allowed_tools": [],
        }), encoding="utf-8")
        (registry / "sa-other-list.json").write_text(json.dumps({
            "version": 1,
            "id": "sa-other-list",
            "session": "subagent-sa-other-list",
            "parent_session": "testbed-subagents-other",
            "status": "running",
            "pid": os.getpid(),
            "allowed_tools": [],
        }), encoding="utf-8")

        client = TestbedClient(self.url, name="testbed-subagents-context-list")
        try:
            client.connect()
            client.create_session("testbed-subagents-context")

            listed = client.call_tool("subagent_list", {"status": "running"}, timeout=10).json()
        finally:
            client.close()

        self.assertEqual([item.get("id") for item in listed.get("items", [])], ["sa-context-list"])

    def test_subagent_spawn_schema_exposes_provider_override(self):
        with self.make_client("testbed-subagents-schema") as client:

            spawn = client.assert_tool("subagent_spawn")
            properties = spawn.get("params") or {}
            self.assertIn("provider", properties)
            self.assertIn("name", properties)

    def test_subagent_manifest_uses_long_running_deadlines(self):
        manifest_path = Path(self.tabula_home) / "plugins" / "subagents" / "plugin.toml"
        manifest = tomllib.loads(manifest_path.read_text(encoding="utf-8"))
        tools = {tool.get("name"): tool for tool in manifest.get("tools", [])}
        for name in ("subagent_spawn", "subagent_wait", "subagent_batch_wait"):
            self.assertIn(name, tools)
            self.assertGreaterEqual(tools[name].get("deadline_ms", 0), 900000)
        self.assertIn("subagent_batch", tools)

    def test_subagent_spawn_rejects_unknown_provider_override(self):
        with self.make_client("testbed-subagents-provider") as client:

            payload = client.call_tool(
                "subagent_spawn",
                {
                    "type": "general",
                    "task": "provider validation",
                    "mode": "async",
                    "id": "sa-bad-provider",
                    "provider": "definitely-missing-provider",
                },
                timeout=10,
            ).json()
            self.assertFalse(payload.get("ok"), payload)
            self.assertIn("provider", str(payload.get("error", "")).lower())

    def test_subagents_plugin_enforces_allowed_tools(self):
        home = Path(self.tabula_home)
        entry = home / "tenants" / "default" / "state" / "plugins" / "subagents" / "sa-testbed.json"
        entry.parent.mkdir(parents=True, exist_ok=True)
        entry.write_text(json.dumps({
            "version": 1,
            "id": "sa-testbed",
            "session": "subagent-sa-testbed",
            "parent_session": "testbed-subagents",
            "status": "running",
            "pid": 0,
            "allowed_tools": ["session_info"],
        }), encoding="utf-8")
        with self.make_client("testbed-subagent-child") as client:

            allowed = client.call_tool("session_info", {"session": "subagent-sa-testbed"}, timeout=10).json()
            self.assertIn("info", allowed)
            blocked = client.call_tool("subagent_list", {}, timeout=10).json()
            self.assertFalse(blocked.get("ok"), blocked)
            self.assertEqual(blocked.get("error"), "not_invoked")
            self.assertEqual((blocked.get("hook") or {}).get("reply_action"), "block")

    def test_subagents_empty_allowed_tools_does_not_block_child_tools(self):
        home = Path(self.tabula_home)
        entry = home / "tenants" / "default" / "state" / "plugins" / "subagents" / "sa-empty-tools.json"
        entry.parent.mkdir(parents=True, exist_ok=True)
        entry.write_text(json.dumps({
            "version": 1,
            "id": "sa-empty-tools",
            "session": "subagent-sa-empty-tools",
            "parent_session": "testbed-subagents",
            "status": "running",
            "pid": 0,
            "allowed_tools": [],
        }), encoding="utf-8")
        with self.make_client("testbed-subagent-empty-tools") as client:

            listed = client.call_tool("subagent_list", {}, timeout=10).json()
            self.assertIn("items", listed)



    def test_subagent_wait_timeout_is_structured_and_capped(self):
        home = Path(self.tabula_home)
        sid = "sa-wait-timeout"
        entry = home / "tenants" / "default" / "state" / "plugins" / "subagents" / f"{sid}.json"
        entry.parent.mkdir(parents=True, exist_ok=True)
        entry.write_text(json.dumps({
            "version": 1,
            "id": sid,
            "session": f"subagent-{sid}",
            "parent_session": "testbed-subagents",
            "status": "running",
            "pid": os.getpid(),
            "allowed_tools": [],
        }), encoding="utf-8")
        with self.make_client("testbed-subagent-wait-timeout") as client:

            payload = client.call_tool("subagent_wait", {"id": sid, "timeout": 0}, timeout=10).json()
            self.assertTrue(payload.get("ok"), payload)
            self.assertTrue(payload.get("timeout"), payload)
            self.assertTrue(payload.get("continues_running"), payload)



    def test_subagents_writes_plugin_owned_state_and_logs(self):
        home = Path(self.tabula_home)
        fake_acp = home / "data" / "testbed" / "fake-acp-agent.py"
        fake_acp.parent.mkdir(parents=True, exist_ok=True)
        fake_acp.write_text(FAKE_ACP, encoding="utf-8")
        python = home / ".venv" / "bin" / "python3"
        if not python.is_file():
            python = Path(sys.executable)
        with self.make_client("testbed-subagents-layout") as client:

            spawned = client.call_tool("subagent_spawn", {
                "type": "acp",
                "task": "layout smoke",
                "id": "sa-layout",
                "mode": "async",
                "timeout": 30,
                "acp_command": [str(python), str(fake_acp)],
            }, timeout=10).json()
            self.assertEqual(spawned.get("id"), "sa-layout")
            self.assertTrue((home / "tenants" / "default" / "state" / "plugins" / "subagents" / "sa-layout.json").is_file())
            self.assertTrue((home / "logs" / "plugins" / "subagents" / "sa-layout.log").is_file())
            self.assertFalse((home / "state" / "subagents").exists())
            self.assertFalse((home / "logs" / "subagents").exists())
            client.call_tool("subagent_kill", {"id": "sa-layout"}, timeout=10)



    def test_subagent_batch_wait_runs_acp_jobs_and_cli_reports_batch(self):
        suffix = int(time.time() * 1000)
        batch_id = f"batch-acp-{suffix}"
        sid_one = f"sa-batch-one-{suffix}"
        sid_two = f"sa-batch-two-{suffix}"
        parent_session = f"testbed-subagents-batch-{suffix}"
        acp_command = self.fake_acp_command("one-shot-acp-batch.py", ONE_SHOT_ACP)
        client = TestbedClient(self.url, name="testbed-subagents-batch")
        try:
            client.connect()
            client.create_session(parent_session)

            spawned = client.call_tool(
                "subagent_batch",
                {
                    "batch_id": batch_id,
                    "type": "acp",
                    "mode": "parallel",
                    "timeout": 30,
                    "acp_command": acp_command,
                    "tasks": [
                        {"id": sid_one, "name": "Batch one", "task": "BATCH-ONE"},
                        {"id": sid_two, "name": "Batch two", "task": "BATCH-TWO"},
                    ],
                },
                timeout=20,
            ).json()
            self.assertTrue(spawned.get("ok"), spawned)
            self.assertEqual(spawned.get("event"), "spawned")
            self.assertEqual(spawned.get("batch_id"), batch_id)
            self.assertEqual((spawned.get("counts") or {}).get("total"), 2)
            self.assertIn("subagent_batch_wait", str(spawned.get("next_action") or ""))

            waited = client.call_tool("subagent_batch_wait", {"batch_id": batch_id, "timeout": 0}, timeout=10).json()
            self.assertTrue(waited.get("ok"), waited)
            self.assertEqual(waited.get("status"), "running")
            self.assertTrue(waited.get("timeout"), waited)
            self.assertTrue(waited.get("continues_running"), waited)
            self.assertEqual((waited.get("counts") or {}).get("total"), 2)
            self.assertEqual((waited.get("counts") or {}).get("running"), 2)
            self.assertEqual({job.get("id") for job in waited.get("still_running", [])}, {sid_one, sid_two})
            cli_batch = self.plugin_cli("batch", batch_id)
            self.assertEqual(cli_batch.get("status"), "running", cli_batch)
            self.assertEqual((cli_batch.get("counts") or {}).get("running"), 2)
            cli_jobs = self.plugin_cli("jobs", f"--parent-session={parent_session}", "--status=running")
            self.assertTrue({sid_one, sid_two}.issubset({item.get("id") for item in cli_jobs.get("items", [])}), cli_jobs)
            for sid in (sid_one, sid_two):
                client.call_tool("subagent_kill", {"id": sid}, timeout=10)
        finally:
            client.close()



    def test_acp_subagent_send_wait_returns_new_result(self):
        home = Path(self.tabula_home)
        fake_acp = home / "data" / "testbed" / "fake-acp-agent.py"
        fake_acp.parent.mkdir(parents=True, exist_ok=True)
        fake_acp.write_text(FAKE_ACP, encoding="utf-8")
        python = home / ".venv" / "bin" / "python3"
        if not python.is_file():
            python = Path(sys.executable)
        with self.make_client("testbed-subagents-acp") as client:

            spawned = client.call_tool(
                "subagent_spawn",
                {
                    "type": "acp",
                    "task": "FIRST",
                    "id": "sa-acp-history",
                    "mode": "async",
                    "timeout": 30,
                    "acp_command": [str(python), str(fake_acp)],
                },
                timeout=20,
            ).json()
            self.assertEqual(spawned.get("status"), "running")
            waited_first = client.call_tool("subagent_wait", {"id": "sa-acp-history", "timeout": 30}, timeout=35).json()
            self.assertTrue(waited_first.get("ok"), waited_first)
            self.assertIn("FAKE ACP:", str(waited_first.get("result", "")))
            self.assertIn("FIRST", str(waited_first.get("result", "")))
            delivered = client.call_tool("subagent_send", {"id": "sa-acp-history", "message": "SECOND"}, timeout=10).json()
            self.assertTrue(delivered.get("delivered"), delivered)
            waited = client.call_tool("subagent_wait", {"id": "sa-acp-history", "timeout": 30}, timeout=35).json()
            self.assertTrue(waited.get("ok"), waited)
            self.assertIn("FAKE ACP: SECOND", str(waited.get("result", "")))
            steered = client.call_tool("subagent_steer", {"id": "sa-acp-history", "instruction": "THIRD"}, timeout=10).json()
            self.assertTrue(steered.get("delivered"), steered)
            waited_steer = client.call_tool("subagent_wait", {"id": "sa-acp-history", "timeout": 30}, timeout=35).json()
            self.assertTrue(waited_steer.get("ok"), waited_steer)
            self.assertIn("FAKE ACP: [steering] THIRD", str(waited_steer.get("result", "")))
            killed = client.call_tool("subagent_kill", {"id": "sa-acp-history"}, timeout=10).json()
            self.assertIn(killed.get("status"), {"killed", "completed"})

    def test_async_acp_kill_preserves_completion(self):
        home = Path(self.tabula_home)
        fake_acp = home / "data" / "testbed" / "fake-acp-agent.py"
        fake_acp.parent.mkdir(parents=True, exist_ok=True)
        fake_acp.write_text(FAKE_ACP, encoding="utf-8")
        python = home / ".venv" / "bin" / "python3"
        if not python.is_file():
            python = Path(sys.executable)
        with self.make_client("testbed-subagents-result-ref") as client:

            spawned = client.call_tool(
                "subagent_spawn",
                {
                    "type": "acp",
                    "task": "RESULT-REF",
                    "id": "sa-acp-result-ref",
                    "mode": "async",
                    "timeout": 0,
                    "acp_command": [str(python), str(fake_acp)],
                },
                timeout=20,
            ).json()
            self.assertEqual(spawned.get("status"), "running")
            time.sleep(0.5)
            killed = client.call_tool("subagent_kill", {"id": "sa-acp-result-ref"}, timeout=10).json()
            self.assertEqual(killed.get("status"), "completed", killed)
            self.assertEqual(killed.get("notification", {}).get("kind"), "subagent.completed")
            self.assertIsNone(killed.get("cancellation"), killed)
            self.assertFalse(killed.get("result_ref"))
            self.assertIn("RESULT-REF", str(killed.get("result", "")))

    def test_sync_acp_subagent_can_use_isolated_git_worktree(self):
        home = Path(self.tabula_home)
        fake_acp = home / "data" / "testbed" / "fake-acp-agent.py"
        fake_acp.parent.mkdir(parents=True, exist_ok=True)
        fake_acp.write_text(FAKE_ACP, encoding="utf-8")
        python = home / ".venv" / "bin" / "python3"
        if not python.is_file():
            python = Path(sys.executable)
        try:
            repo = self.make_git_repo("subagent-worktree-keep")
        except Exception as exc:
            self.skipTest(f"git unavailable: {exc}")
        capture = home / "data" / "testbed" / "worktree-capture.json"
        with self.make_client("testbed-subagents-worktree-keep") as client:

            payload = client.call_tool(
                "subagent_spawn",
                {
                    "type": "acp",
                    "task": f"CAPTURE_PATH={capture}\nMUTATE_FILE=file.txt",
                    "id": "sa-worktree-keep",
                    "mode": "sync",
                    "timeout": 30,
                    "acp_command": [str(python), str(fake_acp)],
                    "worktree": {"source": str(repo), "keep": True},
                },
                timeout=35,
            ).json()
        self.assertTrue(payload.get("ok"), payload)
        worktree = (payload.get("job") or {}).get("worktree") or {}
        captured = json.loads(capture.read_text(encoding="utf-8"))
        self.assertEqual(captured.get("cwd"), worktree.get("path"))
        self.assertEqual(captured.get("project_root"), worktree.get("path"))
        self.assertEqual((repo / "file.txt").read_text(encoding="utf-8"), "parent\n")
        self.assertEqual(worktree.get("cleanup_state"), "kept")

    def test_sync_acp_subagent_worktree_keep_false_removes_clean_tree(self):
        home = Path(self.tabula_home)
        fake_acp = home / "data" / "testbed" / "fake-acp-agent.py"
        fake_acp.parent.mkdir(parents=True, exist_ok=True)
        fake_acp.write_text(FAKE_ACP, encoding="utf-8")
        python = home / ".venv" / "bin" / "python3"
        if not python.is_file():
            python = Path(sys.executable)
        try:
            repo = self.make_git_repo("subagent-worktree-remove")
        except Exception as exc:
            self.skipTest(f"git unavailable: {exc}")
        capture = home / "data" / "testbed" / "worktree-remove-capture.json"
        with self.make_client("testbed-subagents-worktree-remove") as client:

            payload = client.call_tool(
                "subagent_spawn",
                {
                    "type": "acp",
                    "task": f"CAPTURE_PATH={capture}",
                    "id": "sa-worktree-remove",
                    "mode": "sync",
                    "timeout": 30,
                    "acp_command": [str(python), str(fake_acp)],
                    "worktree": {"source": str(repo), "keep": False},
                },
                timeout=35,
            ).json()
        self.assertTrue(payload.get("ok"), payload)
        worktree = (payload.get("job") or {}).get("worktree") or {}
        self.assertEqual(worktree.get("cleanup_state"), "removed")
        self.assertFalse(Path(str(worktree.get("path") or "")).exists())

    def test_sync_acp_subagent_worktree_keep_false_keeps_changed_tree(self):
        home = Path(self.tabula_home)
        fake_acp = home / "data" / "testbed" / "fake-acp-agent.py"
        fake_acp.parent.mkdir(parents=True, exist_ok=True)
        fake_acp.write_text(FAKE_ACP, encoding="utf-8")
        python = home / ".venv" / "bin" / "python3"
        if not python.is_file():
            python = Path(sys.executable)
        try:
            repo = self.make_git_repo("subagent-worktree-changed")
        except Exception as exc:
            self.skipTest(f"git unavailable: {exc}")
        capture = home / "data" / "testbed" / "worktree-changed-capture.json"
        with self.make_client("testbed-subagents-worktree-changed") as client:

            payload = client.call_tool(
                "subagent_spawn",
                {
                    "type": "acp",
                    "task": f"CAPTURE_PATH={capture}\nMUTATE_FILE=file.txt",
                    "id": "sa-worktree-changed",
                    "mode": "sync",
                    "timeout": 30,
                    "acp_command": [str(python), str(fake_acp)],
                    "worktree": {"source": str(repo), "keep": False},
                },
                timeout=35,
            ).json()
        self.assertTrue(payload.get("ok"), payload)
        worktree = (payload.get("job") or {}).get("worktree") or {}
        self.assertEqual(worktree.get("cleanup_state"), "kept_due_to_changes")
        self.assertTrue(Path(str(worktree.get("path") or "")).exists())


def main() -> int:
    parser = argparse.ArgumentParser(description="Run subagents testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    SubagentsPluginSmoke.url = args.url
    SubagentsPluginSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(SubagentsPluginSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

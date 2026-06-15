#!/usr/bin/env python3
from __future__ import annotations

import argparse
import time
import os
from pathlib import Path
import json
import subprocess
import sys
import unittest
import websocket

from tabula_testbed import TestbedClient


def kernel_auth_token() -> str:
    token = os.environ.get("TABULA_KERNEL_TOKEN", "").strip()
    if token:
        return token
    root = os.environ.get("TABULA_HOME", "").strip()
    if not root:
        return ""
    try:
        return (Path(root) / "run" / "kernel-client-token").read_text(encoding="utf-8").strip()
    except OSError:
        return ""


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


class SubagentsPluginSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def make_client(self, name: str) -> TestbedClient:
        client = TestbedClient(self.url, name=name)
        client.connect_join("testbed-subagents")
        return client

    @classmethod
    def tabula_bin(cls) -> str:
        candidate = Path(cls.tabula_home) / "bin" / "tabula"
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

    def test_before_prompt_build_hooks_do_not_block_join_ack(self):
        hook = websocket.create_connection(self.url, timeout=5)
        client = websocket.create_connection(self.url, timeout=5)
        try:
            hook.send(json.dumps({
                "v": 3,
                "type": "hello",
                "data": {
                    "name": "testbed-slow-before-prompt-build",
                    "send_topics": ["hook_reply"],
                    "receive_topics": ["hook"],
                    "auth_token": kernel_auth_token(),
                    "hooks": [{"event": "before_prompt_build", "priority": 100, "timeout_ms": 2000}],
                },
            }))
            self.assertEqual(json.loads(hook.recv()).get("type"), "hello_ack")

            client.send(json.dumps({
                "v": 3,
                "type": "hello",
                "data": {
                    "name": "testbed-subagent-join-ack",
                    "send_topics": ["message.user", "tool.call"],
                    "receive_topics": ["session.init", "message.user", "tool.result", "error"],
                    "auth_token": kernel_auth_token(),
                },
            }))
            self.assertEqual(json.loads(client.recv()).get("type"), "hello_ack")

            started = time.monotonic()
            client.send(json.dumps({"v": 3, "type": "join", "session": "subagent-testbed-join-ack", "tenant_id": "default"}))
            joined = json.loads(client.recv())
            elapsed = time.monotonic() - started
            self.assertEqual(joined.get("type"), "joined")
            self.assertLess(elapsed, 1.0, f"joined was blocked by before_prompt_build for {elapsed:.3f}s")

            init = json.loads(client.recv())
            self.assertEqual(init.get("type"), "event")
            self.assertEqual(init.get("topic"), "session.init")
        finally:
            hook.close()
            client.close()

    def test_subagents_plugin_and_client_are_installed(self):
        home = Path(self.tabula_home)
        self.assertTrue((home / "plugins" / "subagents" / "plugin.toml").is_file())
        self.assertTrue((home / "clients" / "subagent" / "client.toml").is_file())
        self.assertTrue((home / "clients" / "subagent-acp" / "client.toml").is_file())
        self.assertFalse((home / "skills" / "_subagent_types").exists())
        self.assertFalse((home / "plugins" / "_subagent_types").exists())
        self.assertFalse((home / "state" / "subagents").exists())
        self.assertFalse((home / "logs" / "subagents").exists())
        self.assertTrue((home / "plugins" / "subagents" / "types" / "general.toml").is_file())

    def test_subagents_tools_are_plugin_tools(self):
        with self.make_client("testbed-subagents") as client:
            required = {"subagent_spawn", "subagent_send", "subagent_steer", "subagent_wait", "subagent_list", "subagent_kill"}
            client.wait_tools(required, session="testbed-subagents")
            self.assertFalse(client.has_tool("process_spawn"))
            self.assertFalse(client.has_tool("process_kill"))
            self.assertFalse(client.has_tool("process_list"))
            listed = client.call_tool("subagent_list", {}, timeout=10).json()
            self.assertIn("items", listed)

    def test_subagent_spawn_schema_exposes_provider_override(self):
        with self.make_client("testbed-subagents-schema") as client:
            client.wait_tools({"subagent_spawn"}, session="testbed-subagents")
            spawn = client.assert_tool("subagent_spawn")
            properties = spawn.get("params") or {}
            self.assertIn("provider", properties)

    def test_subagent_manifest_uses_long_running_deadlines(self):
        manifest = (Path(self.tabula_home) / "plugins" / "subagents" / "plugin.toml").read_text(encoding="utf-8")
        self.assertIn('name = "subagent_spawn"', manifest)
        self.assertIn('name = "subagent_wait"', manifest)
        self.assertGreaterEqual(manifest.count("deadline_ms = 900000"), 2)

    def test_subagent_spawn_rejects_unknown_provider_override(self):
        with self.make_client("testbed-subagents-provider") as client:
            client.wait_tools({"subagent_spawn"}, session="testbed-subagents")
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
            "allowed_tools": ["session_list"],
        }), encoding="utf-8")
        with self.make_client("testbed-subagent-child") as client:
            client.refresh_init("subagent-sa-testbed")
            allowed = client.call_tool("session_list", {}, timeout=10).json()
            self.assertIn("sessions", allowed)
            blocked = client.call_tool("subagent_list", {}, timeout=10).output
            self.assertIn("blocked", blocked)

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
            client.refresh_init("subagent-sa-empty-tools")
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
            client.refresh_init("testbed-subagents")
            payload = client.call_tool("subagent_wait", {"id": sid, "timeout": 0}, timeout=10).json()
            self.assertFalse(payload.get("ok"), payload)
            self.assertTrue(payload.get("timeout"), payload)
            self.assertTrue(payload.get("continues_running"), payload)

            too_long = client.call_tool("subagent_wait", {"id": sid, "timeout": 901}, timeout=10).json()
            self.assertFalse(too_long.get("ok"), too_long)
            self.assertIn("900s or less", too_long.get("error", ""))

    def test_native_subagent_send_delivers_to_child_tenant_session(self):
        tenant_id = "alpha-subagents"
        self.ensure_tenant(tenant_id)
        home = Path(self.tabula_home)
        sid = "sa-native-delivery"
        result_file = home / "tenants" / tenant_id / "state" / "plugins" / "subagents" / f"{sid}.result.txt"
        log_file = home / "logs" / "plugins" / "subagents" / f"{sid}.log"
        registry = home / "tenants" / tenant_id / "state" / "plugins" / "subagents" / f"{sid}.json"
        registry.parent.mkdir(parents=True, exist_ok=True)
        result_file.write_text("READY", encoding="utf-8")
        registry.write_text(json.dumps({
            "version": 1,
            "id": sid,
            "task_id": sid,
            "session": f"subagent-{sid}",
            "parent_session": "testbed-subagents-alpha",
            "owner_session": "testbed-subagents-alpha",
            "pid": os.getpid(),
            "status": "running",
            "transport": "native",
            "result_file": str(result_file),
            "log_file": str(log_file),
            "current_activity": "completed",
            "updated_at": time.time(),
        }), encoding="utf-8")

        child = TestbedClient(self.url, name="testbed-subagents-native-child")
        parent = TestbedClient(self.url, name="testbed-subagents-native-parent")
        try:
            child.connect_join(f"subagent-{sid}", tenant_id=tenant_id, sends=["turn.done"], receives=["session.init", "message.user"])
            parent.connect_join("testbed-subagents-alpha", tenant_id=tenant_id)
            parent.wait_tools({"subagent_send"}, session="testbed-subagents-alpha", tenant_id=tenant_id)
            delivered = parent.call_tool("subagent_send", {"id": sid, "message": "SECOND"}, timeout=10).json()
            self.assertTrue(delivered.get("delivered"), delivered)
            msg = child.recv(type="message.user", timeout=5)
            self.assertEqual((msg.get("data") or {}).get("text"), "SECOND")
        finally:
            child.close()
            parent.close()

    def test_subagents_writes_plugin_owned_state_and_logs(self):
        home = Path(self.tabula_home)
        fake_acp = home / "data" / "testbed" / "fake-acp-agent.py"
        fake_acp.parent.mkdir(parents=True, exist_ok=True)
        fake_acp.write_text(FAKE_ACP, encoding="utf-8")
        python = home / ".venv" / "bin" / "python3"
        if not python.is_file():
            python = Path(sys.executable)
        with self.make_client("testbed-subagents-layout") as client:
            client.wait_tools({"subagent_spawn", "subagent_kill"}, session="testbed-subagents")
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

    def test_async_subagent_send_wait_returns_new_result(self):
        self.skipTest("async subagent integration depends on live provider credentials; covered by subagent unit tests")
        with self.make_client("testbed-subagents-async") as client:
            client.wait_tools({"subagent_spawn", "subagent_send", "subagent_wait", "subagent_kill"}, session="testbed-subagents")
            spawned = client.call_tool("subagent_spawn", {
                "type": "general",
                "task": "Reply exactly with FIRST.",
                "id": "sa-async-history",
                "timeout": 30,
            }, timeout=20).json()
            self.assertEqual(spawned.get("status"), "running")
            delivered = client.call_tool("subagent_send", {"id": "sa-async-history", "message": "Reply exactly with SECOND."}, timeout=10).json()
            self.assertTrue(delivered.get("delivered"), delivered)
            waited = client.call_tool("subagent_wait", {"id": "sa-async-history", "timeout": 30}, timeout=35).json()
            self.assertTrue(waited.get("ok"), waited)
            self.assertIn("SECOND", str(waited.get("result", "")))
            msg = client.recv(type="message.user", timeout=5)
            self.assertEqual(msg.get("id"), "sa-async-history")
            self.assertIn('<subagent_async_result id="sa-async-history"', msg.get("text", ""))
            self.assertEqual(msg.get("meta", {}).get("source"), "subagent")
            self.assertEqual(msg.get("meta", {}).get("subagent_id"), "sa-async-history")
            client.call_tool("subagent_kill", {"id": "sa-async-history"}, timeout=10)

    def test_acp_subagent_send_wait_returns_new_result(self):
        home = Path(self.tabula_home)
        fake_acp = home / "data" / "testbed" / "fake-acp-agent.py"
        fake_acp.parent.mkdir(parents=True, exist_ok=True)
        fake_acp.write_text(FAKE_ACP, encoding="utf-8")
        python = home / ".venv" / "bin" / "python3"
        if not python.is_file():
            python = Path(sys.executable)
        with self.make_client("testbed-subagents-acp") as client:
            client.wait_tools({"subagent_spawn", "subagent_send", "subagent_wait", "subagent_kill"}, session="testbed-subagents")
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
            client.wait_tools({"subagent_spawn", "subagent_kill"}, session="testbed-subagents")
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
            client.wait_tools({"subagent_spawn"}, session="testbed-subagents")
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
        worktree = (payload.get("entry") or {}).get("worktree") or {}
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
            client.wait_tools({"subagent_spawn"}, session="testbed-subagents")
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
        worktree = (payload.get("entry") or {}).get("worktree") or {}
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
            client.wait_tools({"subagent_spawn"}, session="testbed-subagents")
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
        worktree = (payload.get("entry") or {}).get("worktree") or {}
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

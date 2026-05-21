#!/usr/bin/env python3
from __future__ import annotations

import argparse
import time
import os
from pathlib import Path
import json
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

    def test_before_prompt_build_hooks_do_not_block_join_ack(self):
        hook = websocket.create_connection(self.url, timeout=5)
        client = websocket.create_connection(self.url, timeout=5)
        try:
            hook.send(json.dumps({
                "version": 2,
                "type": "connect",
                "name": "testbed-slow-before-prompt-build",
                "sends": ["hook_result"],
                "receives": ["hook"],
                "auth_token": kernel_auth_token(),
                "hooks": [{"event": "before_prompt_build", "priority": 100, "timeout_ms": 2000}],
            }))
            self.assertEqual(json.loads(hook.recv()).get("type"), "connected")

            client.send(json.dumps({
                "version": 2,
                "type": "connect",
                "name": "testbed-subagent-join-ack",
                "sends": ["message", "tool_use"],
                "receives": ["init", "message", "tool_result", "error"],
                "auth_token": kernel_auth_token(),
            }))
            self.assertEqual(json.loads(client.recv()).get("type"), "connected")

            started = time.monotonic()
            client.send(json.dumps({"version": 2, "type": "join", "session": "subagent-testbed-join-ack", "tenant_id": "default"}))
            joined = json.loads(client.recv())
            elapsed = time.monotonic() - started
            self.assertEqual(joined.get("type"), "joined")
            self.assertLess(elapsed, 1.0, f"joined was blocked by before_prompt_build for {elapsed:.3f}s")

            init = json.loads(client.recv())
            self.assertEqual(init.get("type"), "init")
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
            self.assertIn("blocked by hook", blocked)

    def test_subagents_writes_plugin_owned_state_and_logs(self):
        home = Path(self.tabula_home)
        with self.make_client("testbed-subagents-layout") as client:
            client.wait_tools({"subagent_spawn", "subagent_kill"}, session="testbed-subagents")
            spawned = client.call_tool("subagent_spawn", {
                "type": "general",
                "task": "layout smoke",
                "id": "sa-layout",
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
            msg = client.recv(type="message", timeout=5)
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
            client.call_tool("subagent_kill", {"id": "sa-acp-history"}, timeout=10)


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

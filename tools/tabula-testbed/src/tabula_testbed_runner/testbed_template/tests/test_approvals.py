#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import subprocess
import sys
import time
import unittest

from tabula_testbed import TestbedClient


FAKE_OPENAI = """import json
from types import SimpleNamespace


class _Stream:
    def __init__(self, chunks):
        self._chunks = chunks

    def __iter__(self):
        return iter(self._chunks)

    def close(self):
        return None


def _chunk(*, text="", tool_call=None):
    tool_calls = []
    if tool_call is not None:
        function = SimpleNamespace(name=tool_call["name"], arguments=tool_call["arguments"])
        tool_calls.append(SimpleNamespace(index=0, id=tool_call["id"], function=function))
    delta = SimpleNamespace(content=text, tool_calls=tool_calls)
    choice = SimpleNamespace(delta=delta)
    return SimpleNamespace(choices=[choice], usage=None)


class _Completions:
    def create(self, **kwargs):
        messages = kwargs.get("messages", []) or []
        if not kwargs.get("stream"):
            choice = SimpleNamespace(message=SimpleNamespace(content="<summary>unused</summary>"))
            return SimpleNamespace(choices=[choice])
        user_count = sum(1 for message in messages if message.get("role") == "user")
        tool_count = sum(1 for message in messages if message.get("role") == "tool")
        if tool_count < user_count:
            command = "printf approved" if user_count < 3 else "printf delayed"
            return _Stream([
                _chunk(tool_call={
                    "id": f"call-{user_count}",
                    "name": "exec_run",
                    "arguments": json.dumps({"command": command}),
                })
            ])
        return _Stream([_chunk(text=f"turn-{user_count}-done")])


class _Chat:
    def __init__(self):
        self.completions = _Completions()


class OpenAI:
    def __init__(self, api_key=None, base_url=None):
        self.api_key = api_key
        self.base_url = base_url
        self.chat = _Chat()
"""


class ApprovalFlowInstalled(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def test_hook_approvals_prompts_and_persists_allow_rule(self):
        home = Path(self.tabula_home)
        self.assertTrue((home / "plugins" / "hook-permissions" / "plugin.toml").is_file(), "hook-permissions plugin missing")
        self.assertTrue((home / "plugins" / "hook-approvals" / "plugin.toml").is_file(), "hook-approvals plugin missing")
        self.assertTrue((home / "plugins" / "exec" / "plugin.toml").is_file(), "exec plugin missing")

        workspace = home / "data" / "testbed" / "approvals-workspace"
        workspace.mkdir(parents=True, exist_ok=True)
        self.write_driver_config(home)
        self.write_exec_config(home, workspace)
        self.write_permissions_config(home)
        approvals_cfg = self.approvals_config_path(home)
        approvals_cfg.parent.mkdir(parents=True, exist_ok=True)
        approvals_cfg.unlink(missing_ok=True)
        (home / "config" / "plugins" / "hook-approvals" / "config.toml").unlink(missing_ok=True)

        stub_dir = home / "data" / "testbed" / "fake-openai-approvals"
        stub_dir.mkdir(parents=True, exist_ok=True)
        (stub_dir / "openai.py").write_text(FAKE_OPENAI, encoding="utf-8")

        session = "testbed-approvals"
        proc, log_handle, log_path = self.start_driver(session, stub_dir)
        try:
            with self.connect_client(session) as client:
                first = self.run_turn(client, "first run", approval_choice="allow always")
                self.assertEqual(first["text"], "turn-1-done")
                ask = first["ask"]
                self.assertIsNotNone(ask)
                self.assertEqual(ask["options"], ["allow once", "allow always", "deny once", "deny always"])

                saved = approvals_cfg.read_text(encoding="utf-8")
                self.assertIn('tool = "exec_run"', saved)
                self.assertIn('effect = "allow_always"', saved)

                second = self.run_turn(client, "second run", approval_choice=None)
                self.assertEqual(second["text"], "turn-2-done")
                self.assertIsNone(second["ask"])

                delayed = self.run_turn(client, "third run", approval_choice="allow once", approval_delay=6.0)
                self.assertEqual(delayed["text"], "turn-3-done")
                self.assertIsNotNone(delayed["ask"])

            history = home / "data" / "sessions" / session / "history.jsonl"
            text = history.read_text(encoding="utf-8")
            self.assertIn('"id": "call-1", "name": "exec_run"', text)
            self.assertIn('"id": "call-2", "name": "exec_run"', text)
            self.assertIn('"id": "call-3", "name": "exec_run"', text)
            self.assertIn('delayed', text)
            self.assertGreaterEqual(text.count('approved'), 2)
        finally:
            self.stop_driver(proc)
            log_handle.close()

    def connect_client(self, session: str, *, timeout: float = 20) -> TestbedClient:
        deadline = time.time() + timeout
        last_tools: set[str] = set()
        client = TestbedClient(self.url, name=f"testbed-approvals-{session}")
        while time.time() < deadline:
            client.close()
            client.connect(
                sends=["message.user", "tool.call", "exchange.approve"],
                receives=["session.init", "message.user", "tool.result", "error", "usage.update", "stream.start", "stream.delta", "stream.end", "turn.done", "tool.call", "exchange.approve"],
            )
            client.join(session)
            last_tools = {tool.get("name") for tool in client.tools() if tool.get("name")}
            if "exec_run" in last_tools:
                return client
            time.sleep(0.2)
        client.close()
        raise AssertionError(f"exec_run not advertised in time; tools={sorted(last_tools)}")

    def run_turn(self, client: TestbedClient, text: str, *, approval_choice: str | None, approval_delay: float = 0.0, timeout: float = 20) -> dict[str, object]:
        client.send_message(text)
        deadline = time.time() + timeout
        exchange_request = None
        chunks: list[str] = []
        while time.time() < deadline:
            msg = client.recv(timeout=max(0.1, deadline - time.time()))
            msg_type = msg.get("type")
            if msg_type == "request" and msg.get("topic") == "exchange.approve":
                candidate = {"id": msg.get("id", ""), **(msg.get("data") if isinstance(msg.get("data"), dict) else {})}
                if approval_choice is None:
                    raise AssertionError(f"unexpected approval request: {candidate}")
                exchange_request = candidate
                options = candidate.get("options") if isinstance(candidate.get("options"), list) else []
                self.assertIn(approval_choice, options)
                if approval_delay > 0:
                    time.sleep(approval_delay)
                client._send({
                    "type": "reply",
                    "topic": "exchange.approve",
                    "id": candidate.get("id", ""),
                    "data": {"choice": approval_choice, "index": options.index(approval_choice)},
                })
                continue
            if msg_type == "event" and msg.get("topic") == "stream.delta":
                data = msg.get("data") if isinstance(msg.get("data"), dict) else {}
                chunks.append(str(data.get("text") or ""))
                continue
            if msg_type == "error":
                raise AssertionError(f"kernel error during turn: {msg}")
            if msg_type == "event" and msg.get("topic") == "turn.done":
                return {"text": "".join(chunks), "ask": exchange_request}
        raise AssertionError(f"timed out waiting for turn completion after {text!r}")

    def start_driver(self, session: str, stub_dir: Path) -> tuple[subprocess.Popen[bytes], object, Path]:
        home = Path(self.tabula_home)
        python = home / ".venv" / "bin" / "python3"
        if not python.is_file():
            python = Path(sys.executable)
        driver = home / "clients" / "driver" / "run.py"
        log_path = home / "logs" / "testbed-approvals.log"
        log_path.parent.mkdir(parents=True, exist_ok=True)
        env = os.environ.copy()
        env.update({
            "TABULA_HOME": self.tabula_home,
            "TABULA_URL": self.url,
            "TABULA_VERBOSE": "1",
            "TABULA_APP_ID": "default",
            "TABULA_TENANT_ID": "default",
            "TABULA_CLIENT_DRIVER_OPENAI_API_KEY": "test-key",
            "TABULA_CLIENT_DRIVER_OPENAI_MODEL": "o3",
            "PYTHONPATH": f"{stub_dir}{os.pathsep}{env.get('PYTHONPATH', '')}" if env.get("PYTHONPATH") else str(stub_dir),
        })
        log_handle = log_path.open("wb")
        proc = subprocess.Popen(
            [str(python), str(driver), "--session", session, "--provider", "openai", "--app", "default"],
            env=env,
            stdout=log_handle,
            stderr=subprocess.STDOUT,
        )
        time.sleep(1)
        self.assertIsNone(proc.poll(), self.format_driver_log(log_path))
        return proc, log_handle, log_path

    def stop_driver(self, proc: subprocess.Popen[bytes]) -> None:
        if proc.poll() is not None:
            return
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=5)

    def write_exec_config(self, home: Path, workspace: Path) -> None:
        path = home / "config" / "plugins" / "exec" / "config.toml"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(
            f'cwd_default = {str(workspace)!r}\n'
            'timeout_default_seconds = 30\n'
            'timeout_max_seconds = 30\n',
            encoding="utf-8",
        )

    def write_driver_config(self, home: Path) -> None:
        path = home / "config" / "global.toml"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(
            '[clients.driver]\n'
            'provider = "openai"\n\n'
            '[clients.driver.providers.openai]\n'
            'type = "openai"\n'
            'api_key = "test-key"\n'
            'base_url = "https://example.invalid/v1"\n'
            'default_model = "o3"\n',
            encoding="utf-8",
        )

    def write_permissions_config(self, home: Path) -> None:
        path = home / "config" / "plugins" / "hook-permissions" / "config.toml"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(
            'default = "deny"\n'
            'deny_untyped = true\n\n'
            '[[rules]]\n'
            'tool = "exec_run"\n'
            'effect = "ask"\n',
            encoding="utf-8",
        )

    def approvals_config_path(self, home: Path) -> Path:
        return home / "tenants" / "default" / "config" / "plugins" / "hook-approvals" / "config.toml"

    def format_driver_log(self, log_path: Path) -> str:
        if not log_path.is_file():
            return "driver log missing"
        text = log_path.read_text(encoding="utf-8", errors="replace")
        return f"driver log:\n{text}"


def main() -> int:
    parser = argparse.ArgumentParser(description="Run hook-approvals testbed checks")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    ApprovalFlowInstalled.url = args.url
    ApprovalFlowInstalled.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(ApprovalFlowInstalled))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

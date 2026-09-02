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
        last_user = ""
        for message in reversed(messages):
            if message.get("role") == "user":
                last_user = str(message.get("content") or "")
                break
        if tool_count < user_count:
            if "persist" in last_user:
                command = "printf approved"
            elif "reconnect" in last_user:
                command = "printf reconnect"
            else:
                command = "printf delayed"
            return _Stream([
                _chunk(tool_call={
                    "id": f"call-{user_count}",
                    "name": "exec_run",
                    "arguments": json.dumps({"cmd": command}),
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
    tenant_id = "approvals"
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def test_hook_approvals_prompts_and_persists_allow_rule(self):
        home = Path(self.tabula_home)
        self.ensure_tenant(home)
        self.assertTrue((home / "plugins" / "hook-permissions" / "plugin.toml").is_file(), "hook-permissions plugin missing")
        self.assertTrue((home / "plugins" / "hook-approvals" / "plugin.toml").is_file(), "hook-approvals plugin missing")
        self.assertTrue((home / "plugins" / "exec" / "plugin.toml").is_file(), "exec plugin missing")

        permissions_cfg = self.permissions_config_path(home)
        original_permissions_cfg = permissions_cfg.read_text(encoding="utf-8") if permissions_cfg.is_file() else None
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
        (home / "plugins" / "driver" / "openai.py").write_text(FAKE_OPENAI, encoding="utf-8")

        session = "testbed-approvals"
        proc, log_handle, log_path = self.start_driver(session, stub_dir)
        try:
            with self.connect_client(session) as client:
                first = self.run_turn(client, "delayed run", approval_choice="allow once", approval_delay=6.0)
                self.assertRegex(str(first["text"]), r"^turn-\d+-done$")
                ask = first["ask"]
                self.assertIsNotNone(ask)
                self.assertEqual(ask["options"], ["allow once", "allow always", "deny once", "deny always"])

                reconnected = self.run_turn_with_approval_reconnect(client, session, "reconnect run")
                self.assertRegex(str(reconnected["text"]), r"^turn-\d+-done$")
                self.assertEqual(reconnected["first_request_id"], reconnected["resent_request_id"])

                persisted = self.run_turn(client, "persist first", approval_choice="allow always")
                self.assertRegex(str(persisted["text"]), r"^turn-\d+-done$")
                self.assertIsNotNone(persisted["ask"])

                saved = approvals_cfg.read_text(encoding="utf-8")
                self.assertIn('tool = "exec_run"', saved)
                self.assertIn('effect = "allow_always"', saved)

                second_persisted = self.run_turn(client, "persist second", approval_choice=None)
                self.assertRegex(str(second_persisted["text"]), r"^turn-\d+-done$")
                self.assertIsNone(second_persisted["ask"])

            tool_results = [
                result
                for turn in (first, reconnected, persisted, second_persisted)
                for result in turn["tool_results"]
            ]
            self.assertEqual([result.get("id") for result in tool_results], ["call-1", "call-2", "call-3", "call-4"])
            self.assertTrue(all(result.get("name") == "exec_run" for result in tool_results))
            outputs = [str(result.get("output") or "") for result in tool_results]
            self.assertIn("delayed", outputs[0])
            self.assertIn("reconnect", outputs[1])
            self.assertGreaterEqual(sum("approved" in output for output in outputs), 2)
            driver_log = log_path.read_text(encoding="utf-8", errors="replace")
            self.assertNotIn("runtime_unavailable", driver_log)
            self.assertNotIn("unknown tool", driver_log)
        finally:
            self.stop_driver(proc)
            log_handle.close()
            if original_permissions_cfg is None:
                permissions_cfg.unlink(missing_ok=True)
            else:
                permissions_cfg.write_text(original_permissions_cfg, encoding="utf-8")

    def connect_client(self, session: str, *, timeout: float = 20) -> TestbedClient:
        client = TestbedClient(self.url, name=f"testbed-approvals-{session}")
        self.connect_session(client, session, timeout=timeout)
        return client

    def connect_session(self, client: TestbedClient, session: str, *, timeout: float) -> None:
        client.connect(sends=["exchange.approve"], receives=["exchange.approve"])
        client.create_session(session, tenant_id=self.tenant_id)
        snapshot = client.get_session(session, tenant_id=self.tenant_id)
        client.subscribe(session, tenant_id=self.tenant_id, after_cursor=snapshot["data"]["cursor"])
        client.send_extension({"type": "join"})
        joined = client.recv(op="extension.event", timeout=timeout)
        self.assertEqual(joined.get("data", {}).get("type"), "joined")

    def run_turn(self, client: TestbedClient, text: str, *, approval_choice: str | None, approval_delay: float = 0.0, timeout: float = 20) -> dict[str, object]:
        client.submit_input(f"input-{time.time_ns()}", {"type": "text", "text": text})
        deadline = time.time() + timeout
        exchange_request = None
        chunks: list[str] = []
        tool_results: list[dict[str, object]] = []
        while time.time() < deadline:
            try:
                msg = client.recv(timeout=min(0.25, max(0.1, deadline - time.time())))
            except TimeoutError:
                continue
            if self.collect_turn_event(msg, chunks, tool_results):
                return {"text": "".join(chunks), "ask": exchange_request, "tool_results": tool_results}
            if msg.get("op") != "extension.event":
                continue
            extension = msg.get("data") if isinstance(msg.get("data"), dict) else {}
            if extension.get("type") != "request" or extension.get("topic") != "exchange.approve":
                continue
            candidate = {"id": extension.get("id") or msg.get("id", ""), **(extension.get("data") if isinstance(extension.get("data"), dict) else {})}
            if approval_choice is None:
                raise AssertionError(f"unexpected approval request: {candidate}")
            exchange_request = candidate
            options = candidate.get("options") if isinstance(candidate.get("options"), list) else []
            self.assertIn(approval_choice, options)
            if approval_delay > 0:
                time.sleep(approval_delay)
            client.send_extension({"type": "reply", "topic": "exchange.approve", "id": candidate["id"], "data": {"choice": approval_choice, "index": options.index(approval_choice), "approved": approval_choice.startswith("allow")}})
        raise AssertionError(f"timed out waiting for durable session execution after {text!r}")

    def run_turn_with_approval_reconnect(self, client: TestbedClient, session: str, text: str, *, timeout: float = 20) -> dict[str, object]:
        client.submit_input(f"input-{time.time_ns()}", {"type": "text", "text": text})
        deadline = time.time() + timeout
        chunks: list[str] = []
        tool_results: list[dict[str, object]] = []
        first_request = None
        while time.time() < deadline:
            msg = client.recv(timeout=max(0.1, deadline - time.time()))
            self.collect_turn_event(msg, chunks, tool_results)
            if msg.get("op") != "extension.event":
                continue
            extension = msg.get("data") if isinstance(msg.get("data"), dict) else {}
            if extension.get("type") == "request" and extension.get("topic") == "exchange.approve":
                first_request = {"id": extension.get("id") or msg.get("id", ""), **(extension.get("data") if isinstance(extension.get("data"), dict) else {})}
                break
        if first_request is None:
            raise AssertionError(f"timed out waiting for approval request before reconnect after {text!r}")

        client.close()
        self.connect_session(client, session, timeout=max(5, deadline - time.time()))
        resent = client.wait_for(
            lambda msg: msg.get("op") == "extension.event"
            and isinstance(msg.get("data"), dict)
            and msg["data"].get("type") == "request"
            and msg["data"].get("topic") == "exchange.approve",
            timeout=max(0.1, deadline - time.time()),
        )
        extension = resent["data"]
        resent_request = {"id": extension.get("id") or resent.get("id", ""), **(extension.get("data") if isinstance(extension.get("data"), dict) else {})}
        self.assertEqual(resent_request.get("question"), first_request.get("question"))
        options = resent_request.get("options") if isinstance(resent_request.get("options"), list) else []
        self.assertIn("allow once", options)
        client.send_extension({"type": "reply", "topic": "exchange.approve", "id": resent_request["id"], "data": {"choice": "allow once", "index": options.index("allow once"), "approved": True}})

        while time.time() < deadline:
            msg = client.recv(timeout=max(0.1, deadline - time.time()))
            if self.collect_turn_event(msg, chunks, tool_results):
                return {
                    "text": "".join(chunks),
                    "first_request_id": first_request.get("id"),
                    "resent_request_id": resent_request.get("id"),
                    "tool_results": tool_results,
                }
        raise AssertionError(f"timed out waiting for turn completion after approval reconnect for {text!r}")

    def collect_turn_event(self, msg: dict, chunks: list[str], tool_results: list[dict[str, object]]) -> bool:
        if msg.get("kind") != "event":
            return False
        committed = msg.get("data") if isinstance(msg.get("data"), dict) else {}
        event = committed.get("data") if isinstance(committed.get("data"), dict) else {}
        payload = event.get("payload") if isinstance(event.get("payload"), dict) else {}
        if msg.get("op") == "stream.delta":
            chunks.append(str(payload.get("text") or ""))
        elif msg.get("op") == "tool.result":
            tool_results.append(dict(payload))
        return msg.get("op") == "turn.state_changed" and event.get("state") in {
            "completed", "failed", "cancelled", "discarded", "recovery_required",
        }

    def start_driver(self, session: str, stub_dir: Path) -> tuple[subprocess.Popen[bytes], object, Path]:
        home = Path(self.tabula_home)
        python = home / ".venv" / "bin" / "python3"
        if not python.is_file():
            python = Path(sys.executable)
        driver = home / "plugins" / "driver" / "run.py"
        log_path = home / "logs" / "testbed-approvals.log"
        log_path.parent.mkdir(parents=True, exist_ok=True)
        env = os.environ.copy()
        env.update({
            "TABULA_HOME": self.tabula_home,
            "TABULA_URL": self.url,
            "TABULA_VERBOSE": "1",
            "TABULA_TENANT_ID": self.tenant_id,
            "TABULA_PLUGIN_DRIVER_OPENAI_API_KEY": "test-key",
            "TABULA_PLUGIN_DRIVER_OPENAI_MODEL": "o3",
            "PYTHONPATH": f"{stub_dir}{os.pathsep}{env.get('PYTHONPATH', '')}" if env.get("PYTHONPATH") else str(stub_dir),
        })
        log_handle = log_path.open("wb")
        proc = subprocess.Popen(
            [str(python), str(driver), "--session", session, "--provider", "openai", "--tenant", self.tenant_id],
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

    def ensure_tenant(self, home: Path) -> None:
        if (home / "tenants" / self.tenant_id / "install.lock.json").is_file():
            return
        env = os.environ.copy()
        env["TABULA_HOME"] = str(home)
        agent = home / ".venv" / ("Scripts/tabula-agent.exe" if os.name == "nt" else "bin/tabula-agent")
        subprocess.run(
            [
                str(agent),
                "--home", str(home),
                "install",
                "--distro", str(home / "generated-testbed"),
                "--tenant", self.tenant_id,
                "--no-start",
                "--non-interactive",
            ],
            env=env,
            check=True,
        )

    def write_driver_config(self, home: Path) -> None:
        path = home / "config" / "global.toml"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(
            '[plugins.driver]\n'
            'provider = "openai"\n'
            'session_autostart = false\n\n'
            '[plugins.driver.providers.openai]\n'
            'type = "openai"\n'
            'api_key = "test-key"\n'
            'base_url = "https://example.invalid/v1"\n'
            'default_model = "o3"\n',
            encoding="utf-8",
        )

    def write_permissions_config(self, home: Path) -> None:
        path = self.permissions_config_path(home)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(
            'default = "deny"\n'
            'deny_untyped = true\n\n'
            '[[rules]]\n'
            'tool = "exec_run"\n'
            'effect = "ask"\n',
            encoding="utf-8",
        )

    def permissions_config_path(self, home: Path) -> Path:
        return home / "config" / "plugins" / "hook-permissions" / "config.toml"

    def approvals_config_path(self, home: Path) -> Path:
        return home / "tenants" / self.tenant_id / "config" / "plugins" / "hook-approvals" / "config.toml"

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

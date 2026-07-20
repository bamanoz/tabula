#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import queue
import subprocess
import sys
import threading
import time
import unittest


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

        user_messages = [message for message in messages if message.get("role") == "user"]
        last_user = str(user_messages[-1].get("content") if user_messages else "")
        following_messages = messages[messages.index(user_messages[-1]) + 1:] if user_messages else []
        already_answered = any(message.get("role") in {"assistant", "tool"} for message in following_messages if isinstance(message, dict))
        if "__QUESTION__" in last_user and not already_answered:
            return _Stream([
                _chunk(tool_call={
                    "id": f"call-{len(user_messages)}",
                    "name": "question",
                    "arguments": json.dumps({"questions": [{"question": "Continue?", "options": [{"label": "yes"}, {"label": "no"}]}]}),
                })
            ])
        if "__USE_TOOL__" in last_user and not already_answered:
            return _Stream([
                _chunk(tool_call={
                    "id": f"call-{len(user_messages)}",
                    "name": "exec_run",
                    "arguments": json.dumps({"cmd": "printf acp-tool"}),
                })
            ])
        return _Stream([_chunk(text=f"reply:{last_user}")])


class _Chat:
    def __init__(self):
        self.completions = _Completions()


class OpenAI:
    def __init__(self, api_key=None, base_url=None):
        self.api_key = api_key
        self.base_url = base_url
        self.chat = _Chat()
"""


class ACPRequestError(RuntimeError):
    def __init__(self, error: dict[str, object], notifications: list[dict[str, object]]):
        super().__init__(str(error.get("message") or error))
        self.error = error
        self.notifications = notifications


class ACPProcessClient:
    def __init__(self, proc: subprocess.Popen[str]):
        self.proc = proc
        self._next_id = 0
        self.permission_requests: list[dict[str, object]] = []
        self._stdout: queue.Queue[str | None] = queue.Queue()
        self._reader = threading.Thread(target=self._read_stdout, daemon=True)
        self._reader.start()

    def _read_stdout(self) -> None:
        assert self.proc.stdout is not None
        try:
            for line in self.proc.stdout:
                self._stdout.put(line)
        finally:
            self._stdout.put(None)

    def request(self, method: str, params: dict[str, object] | None = None, *, timeout: float = 20.0) -> tuple[dict[str, object], list[dict[str, object]]]:
        self._next_id += 1
        message_id = self._next_id
        payload = {
            "jsonrpc": "2.0",
            "id": message_id,
            "method": method,
            "params": params or {},
        }
        assert self.proc.stdin is not None
        self.proc.stdin.write(json.dumps(payload) + "\n")
        self.proc.stdin.flush()
        notifications: list[dict[str, object]] = []
        deadline = time.monotonic() + timeout
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise AssertionError(f"timed out waiting for ACP response: method={method} notifications={notifications!r}")
            msg = self._read_message(timeout=remaining)
            if msg.get("id") == message_id:
                if "error" in msg:
                    raise ACPRequestError(msg["error"], notifications)
                result = msg.get("result")
                if not isinstance(result, dict):
                    raise AssertionError(f"unexpected ACP result shape for {method}: {msg!r}")
                return result, notifications
            if isinstance(msg.get("method"), str) and "id" in msg:
                self._handle_server_request(msg)
                continue
            if isinstance(msg.get("method"), str):
                notifications.append(msg)

    def _read_message(self, *, timeout: float) -> dict[str, object]:
        try:
            line = self._stdout.get(timeout=timeout)
        except queue.Empty:
            raise AssertionError(f"timed out waiting for ACP stdout; proc={self.proc.poll()}")
        if line is None:
            raise AssertionError(f"ACP process exited early with code {self.proc.poll()}")
        return json.loads(line)

    def _handle_server_request(self, msg: dict[str, object]) -> None:
        method = str(msg.get("method") or "")
        if method != "requestPermission":
            raise AssertionError(f"unexpected ACP server request: {msg!r}")
        params = msg.get("params") if isinstance(msg.get("params"), dict) else {}
        self.permission_requests.append(params)
        options = params.get("options") if isinstance(params.get("options"), list) else []
        option_id = "once"
        for option in options:
            if not isinstance(option, dict):
                continue
            candidate = str(option.get("optionId") or "")
            if candidate == "once":
                option_id = candidate
                break
        response = {
            "jsonrpc": "2.0",
            "id": msg.get("id"),
            "result": {"outcome": {"outcome": "selected", "optionId": option_id}},
        }
        assert self.proc.stdin is not None
        self.proc.stdin.write(json.dumps(response) + "\n")
        self.proc.stdin.flush()

    def close(self) -> None:
        if self.proc.stdin:
            self.proc.stdin.close()
        try:
            self.proc.terminate()
            self.proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            self.proc.wait(timeout=5)


class ACPGatewayInstalled(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def setUp(self) -> None:
        self.home = Path(self.tabula_home)
        self.workspace = self.home / "data" / "testbed" / "acp-workspace"
        self.workspace.mkdir(parents=True, exist_ok=True)
        self._mutated_files = (
            self.home / "config" / "plugins" / "exec" / "config.toml",
            self.home / "config" / "global.toml",
            self.home / "plugins" / "driver" / "openai.py",
            self.home / "agents" / "build.md",
            self.home / "agents" / "plan.md",
        )
        self._original_files = {
            path: path.read_bytes() if path.is_file() else None
            for path in self._mutated_files
        }
        self.write_exec_config()
        self.write_driver_config()
        self.write_fake_openai()
        self.reload_runtime_workers()
        self.write_agent_catalog()

    def tearDown(self) -> None:
        for path, content in self._original_files.items():
            if content is None:
                path.unlink(missing_ok=True)
                continue
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(content)
        reload_touch = self.home / "run" / "reload.touch"
        reload_touch.parent.mkdir(parents=True, exist_ok=True)
        reload_touch.touch()
        time.sleep(2.5)

    def test_gateway_acp_client_is_installed(self):
        self.assertTrue((self.home / "apps" / "gateway-acp" / "app.toml").is_file())
        self.assertTrue((self.home / "apps" / "gateway-acp" / "run.py").is_file())
        self.assertFalse((self.home / "plugins" / "gateway-acp").exists())

    def test_gateway_acp_full_protocol(self):
        client = self.start_gateway()
        driver_processes: list[tuple[subprocess.Popen[bytes], object]] = []
        try:
            init, _ = client.request("initialize", {"protocolVersion": 1})
            self.assertEqual(init["protocolVersion"], 1)
            self.assertIn("fork", init["agentCapabilities"]["sessionCapabilities"])
            self.assertIn("resume", init["agentCapabilities"]["sessionCapabilities"])

            with self.assertRaises(ACPRequestError) as auth:
                client.request("authenticate", {})
            self.assertIn("not required", str(auth.exception))

            created, _ = client.request("session/new", {"cwd": str(self.workspace), "title": "ACP Smoke"})
            session_id = str(created["sessionId"])
            driver_processes.append(self.start_driver(session_id))
            self.assertEqual(created.get("models", {}).get("currentModelId"), "openai/o3")
            self.assertTrue(any(item.get("modelId") == "openai/o3" for item in created.get("models", {}).get("availableModels", []) if isinstance(item, dict)))
            modes = created.get("modes") or {}
            available = {item.get("id") for item in modes.get("availableModes", []) if isinstance(item, dict)}
            self.assertIn("build", available)
            self.assertIn("plan", available)

            with self.assertRaises(ACPRequestError):
                client.request("setSessionMode", {"sessionId": session_id, "modeId": "unknown"})
            with self.assertRaises(ACPRequestError):
                client.request("unstable_setSessionModel", {"sessionId": session_id, "modelId": "bogus/model"})

            mode_result, _ = client.request("setSessionMode", {"sessionId": session_id, "modeId": "plan"})
            self.assertEqual(mode_result.get("_meta", {}).get("modeId"), "plan")

            model_result, _ = client.request("unstable_setSessionModel", {"sessionId": session_id, "modelId": "openai/o3-mini"})
            self.assertEqual(model_result.get("models", {}).get("currentModelId"), "openai/o3-mini")

            config_mode, _ = client.request("setSessionConfigOption", {"sessionId": session_id, "configId": "mode", "value": "plan"})
            self.assertEqual(config_mode.get("modes", {}).get("currentModeId"), "plan")

            config_model, _ = client.request("setSessionConfigOption", {"sessionId": session_id, "configId": "model", "value": "openai/o3-mini"})
            self.assertEqual(config_model.get("models", {}).get("currentModelId"), "openai/o3-mini")

            prompt_result, notifications = client.request(
                "session/prompt",
                {
                    "sessionId": session_id,
                    "prompt": [{"type": "text", "text": "__USE_TOOL__ first turn"}],
                },
            )
            self.assertEqual(prompt_result.get("stopReason"), "end_turn")
            updates = [note.get("params", {}).get("update", {}) for note in notifications if note.get("method") == "session/update"]
            kinds = {update.get("sessionUpdate") for update in updates if isinstance(update, dict)}
            self.assertIsInstance(kinds, set)
            ask_result, ask_notifications = client.request(
                "session/prompt",
                {
                    "sessionId": session_id,
                    "prompt": [{"type": "text", "text": "__QUESTION__ approval turn"}],
                },
            )
            self.assertEqual(ask_result.get("stopReason"), "end_turn")
            self.assertGreaterEqual(len(client.permission_requests), 1)

            listed, _ = client.request("session/list", {"cwd": str(self.workspace)})
            listed_ids = {item.get("sessionId") for item in listed.get("sessions", []) if isinstance(item, dict)}
            self.assertIn(session_id, listed_ids)

            self.assert_session_store_contains(session_id, agent="plan", model_ref="openai/o3-mini")
            self.assert_history_contains(session_id, '"agent": "plan"')
            self.assert_history_contains(session_id, '"provider": "openai"')
            self.assert_history_contains(session_id, '"model": "o3-mini"')
            self.assert_history_contains(session_id, "__USE_TOOL__ first turn")
            self.assert_history_contains(session_id, "__QUESTION__ approval turn")

            client.close()

            resumed = self.start_gateway()
            try:
                resumed.request("initialize", {"protocolVersion": 1})
                loaded, _ = resumed.request("session/load", {"sessionId": session_id, "cwd": str(self.workspace)})
                self.assertEqual(loaded.get("models", {}).get("currentModelId"), "openai/o3-mini")
                self.assertEqual(loaded.get("modes", {}).get("currentModeId"), "plan")

                resumed_result, _ = resumed.request("session/resume", {"sessionId": session_id, "cwd": str(self.workspace)})
                self.assertEqual(resumed_result.get("sessionId"), session_id)

                forked_id = f"fork-{int(time.time())}"
                forked, _ = resumed.request("session/fork", {"sessionId": session_id, "cwd": str(self.workspace), "newSessionId": forked_id})
                self.assertEqual(forked.get("sessionId"), forked_id)
                driver_processes.append(self.start_driver(forked_id))

                fork_prompt, fork_notifications = resumed.request(
                    "session/prompt",
                    {"sessionId": forked_id, "prompt": [{"type": "text", "text": "fork followup"}]},
                )
                self.assertEqual(fork_prompt.get("stopReason"), "end_turn")
                fork_updates = [note.get("params", {}).get("update", {}) for note in fork_notifications if note.get("method") == "session/update"]
                self.assertTrue(any(update.get("sessionUpdate") == "agent_message_chunk" for update in fork_updates if isinstance(update, dict)))
                self.assert_history_contains(forked_id, "__USE_TOOL__ first turn")
                self.assert_history_contains(forked_id, "fork followup")

                resumed.request("session/close", {"sessionId": forked_id})
                resumed.request("session/close", {"sessionId": session_id})
                self.assert_session_closed(session_id)
                self.assert_session_closed(forked_id)
            finally:
                resumed.close()
        finally:
            for proc, handle in reversed(driver_processes):
                self.stop_driver(proc)
                handle.close()
            if client.proc.poll() is None:
                client.close()

    def start_gateway(self) -> ACPProcessClient:
        python = self.home / ".venv" / "bin" / "python3"
        if not python.is_file():
            python = Path(sys.executable)
        gateway = self.home / "apps" / "gateway-acp" / "run.py"
        log_path = self.home / "logs" / "testbed-acp.log"
        log_path.parent.mkdir(parents=True, exist_ok=True)
        env = os.environ.copy()
        env.update({
            "TABULA_HOME": self.tabula_home,
            "TABULA_URL": self.url,
            "TABULA_APP_ID": "default",
            "TABULA_TENANT_ID": "default",
            "TABULA_PLUGIN_DRIVER_OPENAI_API_KEY": "test-key",
            "TABULA_PLUGIN_DRIVER_OPENAI_MODEL": "o3",
            "PYTHONPATH": f"{self.stub_dir}{os.pathsep}{env.get('PYTHONPATH', '')}" if env.get("PYTHONPATH") else str(self.stub_dir),
        })
        stderr_handle = log_path.open("w", encoding="utf-8")
        proc = subprocess.Popen(
            [str(python), str(gateway), "--app", "default", "--provider", "openai", "--cwd", str(self.workspace), "--timeout", "20"],
            env=env,
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=stderr_handle,
            text=True,
            bufsize=1,
        )
        self.addCleanup(stderr_handle.close)
        return ACPProcessClient(proc)

    def write_fake_openai(self) -> None:
        self.stub_dir = self.home / "data" / "testbed" / "fake-openai-acp"
        self.stub_dir.mkdir(parents=True, exist_ok=True)
        (self.stub_dir / "openai.py").write_text(FAKE_OPENAI, encoding="utf-8")
        driver_stub = self.home / "plugins" / "driver" / "openai.py"
        driver_stub.parent.mkdir(parents=True, exist_ok=True)
        driver_stub.write_text(FAKE_OPENAI, encoding="utf-8")

    def start_driver(self, session_id: str) -> tuple[subprocess.Popen[bytes], object]:
        python = self.home / ".venv" / "bin" / "python3"
        if not python.is_file():
            python = Path(sys.executable)
        driver = self.home / "plugins" / "driver" / "run.py"
        log_path = self.home / "logs" / f"testbed-acp-driver-{session_id}.log"
        log_path.parent.mkdir(parents=True, exist_ok=True)
        env = os.environ.copy()
        env.update({
            "TABULA_HOME": self.tabula_home,
            "TABULA_URL": self.url,
            "TABULA_VERBOSE": "1",
            "TABULA_APP_ID": "default",
            "TABULA_TENANT_ID": "default",
            "TABULA_PLUGIN_DRIVER_OPENAI_API_KEY": "test-key",
            "TABULA_PLUGIN_DRIVER_OPENAI_MODEL": "o3",
            "PYTHONPATH": f"{self.stub_dir}{os.pathsep}{env.get('PYTHONPATH', '')}" if env.get("PYTHONPATH") else str(self.stub_dir),
        })
        log_handle = log_path.open("wb")
        proc = subprocess.Popen(
            [str(python), str(driver), "--session", session_id, "--provider", "openai", "--app", "default"],
            env=env,
            stdout=log_handle,
            stderr=subprocess.STDOUT,
        )
        time.sleep(1)
        self.assertIsNone(proc.poll(), log_path.read_text(encoding="utf-8", errors="replace"))
        return proc, log_handle

    def stop_driver(self, proc: subprocess.Popen[bytes]) -> None:
        if proc.poll() is not None:
            return
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=5)

    def write_agent_catalog(self) -> None:
        agent_dir = self.home / "agents"
        agent_dir.mkdir(parents=True, exist_ok=True)
        (agent_dir / "plan.md").write_text(
            "---\nname: plan\ndescription: Planning agent\nmode: primary\nmodel: openai/o3-mini\n---\nPlan prompt.\n",
            encoding="utf-8",
        )
        (agent_dir / "build.md").write_text(
            "---\nname: build\ndescription: Build agent\nmode: primary\nmodel: openai/o3\n---\nBuild prompt.\n",
            encoding="utf-8",
        )

    def write_exec_config(self) -> None:
        path = self.home / "config" / "plugins" / "exec" / "config.toml"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(
            f'cwd_default = {str(self.workspace)!r}\n'
            'timeout_default_seconds = 30\n'
            'timeout_max_seconds = 30\n',
            encoding="utf-8",
        )

    def write_driver_config(self) -> None:
        path = self.home / "config" / "global.toml"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(
            """
[plugins.driver]
provider = "openai"
session_autostart = false

[plugins.driver.providers.openai]
api_key = "test-key"
base_url = "https://api.openai.com/v1"
default_model = "o3"
api = "chat_completions"

[plugins.driver.providers.openai.models."o3-mini"]
effort = "low"
""".strip()
            + "\n",
            encoding="utf-8",
        )

    def assert_session_store_contains(self, session_id: str, *, agent: str, model_ref: str) -> None:
        path = self.home / "state" / "apps" / "gateway-acp" / "sessions.json"
        payload = json.loads(path.read_text(encoding="utf-8"))
        sessions = payload.get("sessions", [])
        match = next((item for item in sessions if item.get("session_id") == session_id), None)
        self.assertIsNotNone(match)
        self.assertEqual(match.get("agent"), agent)
        self.assertEqual(match.get("model_ref"), model_ref)
        self.assertTrue(str(match.get("last_prompt_preview") or ""))
        self.assertTrue(str(match.get("last_response_preview") or ""))

    def assert_session_closed(self, session_id: str) -> None:
        path = self.home / "state" / "apps" / "gateway-acp" / "sessions.json"
        payload = json.loads(path.read_text(encoding="utf-8"))
        sessions = payload.get("sessions", [])
        match = next((item for item in sessions if item.get("session_id") == session_id), None)
        self.assertIsNotNone(match)
        self.assertIsNotNone(match.get("closed_at"))

    def assert_history_contains(self, session_id: str, needle: str) -> None:
        history = self.home / "data" / "sessions" / session_id / "history.jsonl"
        text = history.read_text(encoding="utf-8")
        self.assertIn(needle, text)


def main() -> int:
    parser = argparse.ArgumentParser(description="Run ACP gateway testbed checks")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    ACPGatewayInstalled.url = args.url
    ACPGatewayInstalled.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(ACPGatewayInstalled))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

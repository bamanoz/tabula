#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import threading
import time
import unittest
import urllib.parse
import urllib.request
import uuid
from typing import Callable

import websocket

from tabula_testbed import TestbedClient, restart_kernel, restart_runtime


FAKE_OPENAI = '''import os
from pathlib import Path
import time
from types import SimpleNamespace


class _Stream:
    def __init__(self, text):
        self.text = text

    def __iter__(self):
        if self.text.startswith("hold for "):
            marker = Path(os.environ["TABULA_HOME"]) / "data" / "testbed" / "gateway-v4-permitted"
            marker.parent.mkdir(parents=True, exist_ok=True)
            marker.write_text(self.text, encoding="utf-8")
            time.sleep(120)
        delta = SimpleNamespace(content=f"first-party:{self.text}", tool_calls=[])
        return iter([SimpleNamespace(choices=[SimpleNamespace(delta=delta)], usage=None)])

    def close(self):
        return None


class _Completions:
    def create(self, **kwargs):
        messages = kwargs.get("messages", []) or []
        text = ""
        for message in reversed(messages):
            if message.get("role") == "user":
                text = str(message.get("content") or "")
                break
        return _Stream(text)


class _Chat:
    def __init__(self):
        self.completions = _Completions()


class OpenAI:
    def __init__(self, api_key=None, base_url=None):
        self.chat = _Chat()
'''


class GatewayV4RecoveryInstalled(unittest.TestCase):
    kernel_url = "ws://localhost:8089/ws"
    tabula_home = ""

    @classmethod
    def setUpClass(cls) -> None:
        cls.home = Path(cls.tabula_home)
        driver_dir = cls.home / "plugins" / "driver"
        for component in ("driver", "gateway-api", "gateway-web"):
            manifest = cls.home / "plugins" / component / "plugin.toml"
            if not manifest.is_file():
                raise AssertionError(f"installed component missing: {manifest}")
        (driver_dir / "openai.py").write_text(FAKE_OPENAI, encoding="utf-8")
        config = cls.home / "config" / "plugins" / "driver" / "config.toml"
        config.parent.mkdir(parents=True, exist_ok=True)
        config.write_text(
            'provider = "openai"\n\n'
            '[providers.openai]\n'
            'type = "openai"\n'
            'api_key = "test-key"\n'
            'base_url = "https://api.openai.invalid/v1"\n'
            'default_model = "test-model"\n'
            'api = "chat_completions"\n',
            encoding="utf-8",
        )

    def test_api_runs_first_party_driver(self) -> None:
        status = self.gateway_status("gateway_api_status")
        session = f"testbed-gateway-api-{uuid.uuid4().hex}"
        response = post_json(
            str(status["url"]) + "/v1/responses",
            {"session": session, "tenant_id": "default", "input": "api success", "timeout": 45},
        )
        self.assertEqual(response.get("status"), "completed", response)
        self.assertIn("first-party:api success", str(response.get("output_text") or ""))
        self.assertEqual(response.get("session"), session)

    def test_api_reconnect_reports_recovery_required_after_kernel_restart(self) -> None:
        self._assert_api_recovery("hold for kernel restart", lambda: restart_kernel(timeout=30))

    def test_first_party_driver_takeover_after_runtime_restart(self) -> None:
        self._assert_api_recovery("hold for runtime restart", lambda: restart_runtime(timeout=30))

    def _assert_api_recovery(self, prompt: str, restart: Callable[[], object]) -> None:
        status = self.gateway_status("gateway_api_status")
        marker = self.home / "data" / "testbed" / "gateway-v4-permitted"
        marker.unlink(missing_ok=True)
        session = f"testbed-gateway-api-recovery-{uuid.uuid4().hex}"
        result: dict[str, object] = {}

        def request() -> None:
            try:
                result["response"] = post_json(
                    str(status["url"]) + "/v1/responses",
                    {"session": session, "tenant_id": "default", "input": prompt, "timeout": 90},
                    timeout=100,
                )
            except BaseException as exc:
                result["error"] = exc

        thread = threading.Thread(target=request, daemon=True)
        thread.start()
        self.wait_for_file(marker, timeout=45)
        restart()
        thread.join(timeout=70)
        self.assertFalse(thread.is_alive(), "gateway-api request did not terminate after recovery")
        self.assertNotIn("error", result, result)
        response = result.get("response")
        self.assertIsInstance(response, dict, result)
        self.assertEqual(response.get("status"), "recovery_required", response)
        self.assertEqual(response.get("error", {}).get("type"), "recovery_required", response)

    def test_websocket_gateway_creates_session_and_completes_turn(self) -> None:
        status = self.gateway_status("gateway_web_status")
        parsed = urllib.parse.urlparse(str(status["url"]))
        socket_url = urllib.parse.urlunparse(("ws", parsed.netloc, "/api/ws", "", parsed.query, ""))
        session = f"testbed-gateway-web-{uuid.uuid4().hex}"
        sock = websocket.create_connection(socket_url, timeout=10)
        try:
            sock.send(json.dumps({"type": "join", "tenant_id": "default", "session": session}))
            self.recv_until(sock, lambda event: event.get("type") == "session.joined", timeout=30)
            sock.send(json.dumps({"type": "message.user", "text": "web success"}))
            events = self.recv_until(sock, lambda event: event.get("type") == "turn.done", timeout=60, collect=True)
        finally:
            sock.close()
        text = "".join(str(event.get("text") or "") for event in events if event.get("type") == "stream.delta")
        self.assertIn("first-party:web success", text, events)
        terminal = next(event for event in reversed(events) if event.get("type") == "turn.done")
        self.assertEqual(terminal.get("state"), "completed", terminal)

    def gateway_status(self, tool: str) -> dict:
        deadline = time.monotonic() + 45
        last_error: object = "gateway status unavailable"
        with TestbedClient(self.kernel_url, name=f"testbed-{tool}") as client:
            session = f"testbed-{tool}-{uuid.uuid4().hex}"
            client.create_session(session, tenant_id="default")
            client.session = session
            client.tenant_id = "default"
            while time.monotonic() < deadline:
                try:
                    status = client.call_tool(tool, {}, timeout=20).json()
                except AssertionError as exc:
                    if f"unknown tool {tool}" not in str(exc) and "runtime unavailable" not in str(exc) and "runtime_unavailable" not in str(exc):
                        raise
                    last_error = exc
                else:
                    if status.get("health") == "ok" and status.get("url"):
                        return status
                    last_error = status
                time.sleep(0.1)
        self.fail(f"timed out waiting for {tool}: {last_error}")

    def wait_for_file(self, path: Path, *, timeout: float) -> None:
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if path.is_file():
                return
            time.sleep(0.05)
        self.fail(f"timed out waiting for {path}")

    def recv_until(self, sock, predicate, *, timeout: float, collect: bool = False):
        deadline = time.monotonic() + timeout
        events: list[dict] = []
        while time.monotonic() < deadline:
            sock.settimeout(max(0.1, deadline - time.monotonic()))
            event = json.loads(sock.recv())
            events.append(event)
            if predicate(event):
                return events if collect else event
        self.fail(f"timed out waiting for gateway event; events={events[-20:]!r}")


def post_json(url: str, payload: dict, *, timeout: float = 60) -> dict:
    request = urllib.request.Request(
        url,
        data=json.dumps(payload).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(request, timeout=timeout) as response:
        return json.loads(response.read().decode("utf-8"))


def main() -> int:
    parser = argparse.ArgumentParser(description="Run installed protocol-v4 gateway recovery tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    GatewayV4RecoveryInstalled.kernel_url = args.url
    GatewayV4RecoveryInstalled.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(
        unittest.defaultTestLoader.loadTestsFromTestCase(GatewayV4RecoveryInstalled)
    )
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

from __future__ import annotations

import json
import os
import socket
import threading
import time
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable

import websocket
from websocket import WebSocketTimeoutException


@dataclass(frozen=True)
class ToolResult:
    name: str
    id: str
    output: str

    def json(self) -> Any:
        try:
            return json.loads(self.output)
        except json.JSONDecodeError as exc:
            raise AssertionError(f"tool {self.name!r} returned non-JSON output: {self.output!r}") from exc

    @property
    def ok(self) -> bool:
        try:
            data = self.json()
        except Exception:
            return not self.output.startswith("ERROR:")
        if isinstance(data, dict) and "ok" in data:
            return bool(data["ok"])
        return not self.output.startswith("ERROR:")


class TestbedClient:
    def __init__(self, url: str = "ws://localhost:8089/ws", *, name: str | None = None, meta: dict[str, Any] | None = None):
        self.url = url
        self.name = name or f"testbed-{uuid.uuid4().hex[:8]}"
        self.meta = dict(meta or {})
        self.ws: websocket.WebSocket | None = None
        self.init: dict[str, Any] = {}
        self.recent_messages: list[dict[str, Any]] = []

    def __enter__(self) -> "TestbedClient":
        if self.ws is None:
            self.connect()
        return self

    def __exit__(self, *_exc: object) -> None:
        self.close()

    def connect(self, *, sends: list[str] | None = None, receives: list[str] | None = None) -> dict[str, Any]:
        sends = _normalize_capabilities(sends if sends is not None else ["message.user", "tool.call"])
        receives = _normalize_capabilities(receives if receives is not None else ["session.init", "message.user", "tool.result", "error"])
        self.ws = websocket.create_connection(self.url, timeout=30)
        msg = {
            "v": 3,
            "type": "hello",
            "data": {
                "name": self.name,
                "send_topics": sends,
                "receive_topics": receives,
                "auth_token": kernel_auth_token(),
                "meta": self.meta,
            },
        }
        self.ws.send(json.dumps(msg))
        return self.recv(type="hello_ack")

    def join(self, session: str, *, tenant_id: str = "default") -> dict[str, Any]:
        self._send({"type": "join", "session": session, "tenant_id": tenant_id})
        self.recv(type="joined")
        self.init = self.recv(type="session.init")
        return self.init

    def connect_join(self, session: str, *, tenant_id: str = "default", sends: list[str] | None = None, receives: list[str] | None = None) -> dict[str, Any]:
        self.connect(sends=sends, receives=receives)
        return self.join(session, tenant_id=tenant_id)

    def tools(self) -> list[dict[str, Any]]:
        tools = self.init.get("tools") or []
        return tools if isinstance(tools, list) else []

    def has_tool(self, name: str) -> bool:
        return any(tool.get("name") == name for tool in self.tools())

    def assert_tool(self, name: str) -> dict[str, Any]:
        for tool in self.tools():
            if tool.get("name") == name:
                return tool
        raise AssertionError(f"tool not advertised: {name}")

    def wait_tools(self, names: set[str] | list[str] | tuple[str, ...], *, timeout: float = 20, session: str = "testbed-tool-wait", tenant_id: str = "default") -> None:
        required = set(names)
        deadline = time.time() + timeout
        missing = required
        advertised: set[str] = set()
        while time.time() < deadline:
            self.refresh_init(session, tenant_id=tenant_id)
            advertised = {tool.get("name") for tool in self.tools()}
            missing = required - advertised
            if not missing:
                return
            time.sleep(0.5)
        raise AssertionError(f"missing required testbed tools: {sorted(missing)}; advertised: {sorted(str(name) for name in advertised if name)}")

    def refresh_init(self, session: str, *, tenant_id: str = "default") -> dict[str, Any]:
        self.close()
        self.connect()
        return self.join(session, tenant_id=tenant_id)

    def call_tool(self, name: str, input: dict[str, Any] | None = None, *, timeout: float = 10, meta: dict[str, Any] | None = None) -> ToolResult:
        call_id = f"tb-{uuid.uuid4().hex}"
        message = {"type": "request", "topic": "tool.call", "id": call_id, "name": name, "input": input or {}}
        if meta:
            message["meta"] = meta
        self._send(message)
        try:
            msg = self.wait_for(lambda m: m.get("type") == "reply" and m.get("topic") == "tool.result" and m.get("id") == call_id, timeout=timeout)
        except Exception as exc:
            raise TimeoutError(f"timed out waiting for tool_result: tool={name!r} id={call_id!r} client={self.name!r} recent={self.recent_messages[-10:]!r}") from exc
        return ToolResult(name=str(msg.get("name") or name), id=call_id, output=str(msg.get("output") or ""))

    def call_tool_async(self, name: str, input: dict[str, Any] | None = None, *, timeout: float = 10) -> "AsyncToolCall":
        result: dict[str, ToolResult | BaseException] = {}

        def run() -> None:
            try:
                result["value"] = self.call_tool(name, input, timeout=timeout)
            except BaseException as exc:
                result["value"] = exc

        thread = threading.Thread(target=run, daemon=True)
        thread.start()
        return AsyncToolCall(thread=thread, result=result, timeout=timeout)

    def reset_fixtures(self) -> None:
        for name in ("testbed_hook_mutator_reset", "testbed_hook_blocker_reset", "testbed_hook_recorder_clear"):
            if self.has_tool(name):
                self.call_tool(name, {})

    def send_message(self, text: str) -> None:
        self._send({"type": "event", "topic": "message.user", "data": {"text": text}})

    def wait_for(self, predicate: Callable[[dict[str, Any]], bool], *, timeout: float = 10) -> dict[str, Any]:
        deadline = time.time() + timeout
        while time.time() < deadline:
            msg = self.recv(timeout=max(0.1, deadline - time.time()))
            if predicate(msg):
                return msg
        raise TimeoutError("timed out waiting for testbed message")

    def recv(self, *, type: str | None = None, timeout: float = 10) -> dict[str, Any]:
        if self.ws is None:
            raise RuntimeError("client is not connected")
        old_timeout = self.ws.gettimeout()
        self.ws.settimeout(timeout)
        try:
            while True:
                raw = self.ws.recv()
                msg = json.loads(raw)
                self.recent_messages.append(msg)
                if len(self.recent_messages) > 50:
                    del self.recent_messages[: len(self.recent_messages) - 50]
                if type is None or msg.get("type") == type or _matches_expected_type(msg, type):
                    return msg
        except (TimeoutError, socket.timeout, WebSocketTimeoutException) as exc:
            wanted = f" type={type!r}" if type else ""
            raise TimeoutError(f"timed out waiting for message{wanted} client={self.name!r}") from exc
        finally:
            self.ws.settimeout(old_timeout)

    def close(self) -> None:
        if self.ws is not None:
            self.ws.close()
            self.ws = None

    def _send(self, msg: dict[str, Any]) -> None:
        if self.ws is None:
            raise RuntimeError("client is not connected")
        msg.setdefault("v", 3)
        self.ws.send(json.dumps(msg))


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


@dataclass
class AsyncToolCall:
    thread: threading.Thread
    result: dict[str, ToolResult | BaseException]
    timeout: float

    def wait(self) -> ToolResult:
        self.thread.join(self.timeout + 2)
        if self.thread.is_alive():
            raise TimeoutError("timed out waiting for async tool call")
        value = self.result.get("value")
        if isinstance(value, BaseException):
            raise value
        if value is None:
            raise RuntimeError("async tool call finished without a result")
        return value


def _matches_expected_type(msg: dict[str, Any], expected: str | None) -> bool:
    if not expected:
        return True
    if msg.get("type") == "event":
        return msg.get("topic") == expected
    return False


def _normalize_capabilities(items: list[str]) -> list[str]:
    return list(items)

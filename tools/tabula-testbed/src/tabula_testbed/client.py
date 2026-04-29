from __future__ import annotations

import json
import socket
import time
import uuid
from dataclasses import dataclass
from typing import Any, Callable

import websocket
from websocket import WebSocketTimeoutException


@dataclass(frozen=True)
class ToolResult:
    name: str
    id: str
    output: str

    def json(self) -> Any:
        return json.loads(self.output)

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
    def __init__(self, url: str = "ws://localhost:8089/ws", *, name: str | None = None):
        self.url = url
        self.name = name or f"testbed-{uuid.uuid4().hex[:8]}"
        self.ws: websocket.WebSocket | None = None
        self.init: dict[str, Any] = {}

    def __enter__(self) -> "TestbedClient":
        if self.ws is None:
            self.connect()
        return self

    def __exit__(self, *_exc: object) -> None:
        self.close()

    def connect(self, *, sends: list[str] | None = None, receives: list[str] | None = None) -> dict[str, Any]:
        self.ws = websocket.create_connection(self.url, timeout=30)
        msg = {
            "version": 1,
            "type": "connect",
            "name": self.name,
            "sends": sends if sends is not None else ["message", "tool_use"],
            "receives": receives if receives is not None else ["init", "message", "tool_result", "error"],
        }
        self.ws.send(json.dumps(msg))
        return self.recv(type="connected")

    def join(self, session: str) -> dict[str, Any]:
        self._send({"version": 1, "type": "join", "session": session})
        self.recv(type="joined")
        self.init = self.recv(type="init")
        return self.init

    def connect_join(self, session: str, *, sends: list[str] | None = None, receives: list[str] | None = None) -> dict[str, Any]:
        self.connect(sends=sends, receives=receives)
        return self.join(session)

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

    def wait_tools(self, names: set[str] | list[str] | tuple[str, ...], *, timeout: float = 20, session: str = "testbed-tool-wait") -> None:
        required = set(names)
        deadline = time.time() + timeout
        missing = required
        while time.time() < deadline:
            self.refresh_init(session)
            advertised = {tool.get("name") for tool in self.tools()}
            missing = required - advertised
            if not missing:
                return
            time.sleep(0.5)
        raise AssertionError(f"missing required testbed tools: {sorted(missing)}")

    def refresh_init(self, session: str) -> dict[str, Any]:
        self.close()
        self.connect()
        return self.join(session)

    def call_tool(self, name: str, input: dict[str, Any] | None = None, *, timeout: float = 10) -> ToolResult:
        call_id = f"tb-{uuid.uuid4().hex}"
        self._send({"version": 1, "type": "tool_use", "id": call_id, "name": name, "input": input or {}})
        try:
            msg = self.wait_for(lambda m: m.get("type") == "tool_result" and m.get("id") == call_id, timeout=timeout)
        except Exception as exc:
            raise TimeoutError(f"timed out waiting for tool_result: tool={name!r} id={call_id!r} client={self.name!r}") from exc
        return ToolResult(name=str(msg.get("name") or name), id=call_id, output=str(msg.get("output") or ""))

    def reset_fixtures(self) -> None:
        for name in ("testbed_hook_mutator_reset", "testbed_hook_blocker_reset", "testbed_hook_recorder_clear"):
            if self.has_tool(name):
                self.call_tool(name, {})

    def send_message(self, text: str) -> None:
        self._send({"version": 1, "type": "message", "text": text})

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
                if type is None or msg.get("type") == type:
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
        self.ws.send(json.dumps(msg))

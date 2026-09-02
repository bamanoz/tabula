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

PROTOCOL_VERSION = 4
DEFAULT_DRIVER_COMPONENT_ID = "driver"
DEFAULT_AGENT_SPEC_REVISION = "testbed:shared-client-v4"


@dataclass(frozen=True)
class ToolResult:
    name: str
    id: str
    output: str
    artifact: Any | None = None
    truncated: bool = False
    meta: dict[str, Any] | None = None

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


class ProtocolError(RuntimeError):
    def __init__(self, envelope: dict[str, Any]):
        data = envelope.get("data") if isinstance(envelope.get("data"), dict) else {}
        self.envelope = envelope
        self.code = str(data.get("code") or "unknown")
        super().__init__(str(data.get("message") or f"protocol-v4 request failed: {envelope!r}"))


class TestbedClient:
    def __init__(self, url: str = "ws://localhost:8089/ws", *, name: str | None = None, meta: dict[str, Any] | None = None):
        self.url = url
        self.name = name or f"testbed-{uuid.uuid4().hex[:8]}"
        self.meta = dict(meta or {})
        self.ws: websocket.WebSocket | None = None
        self.session: str = ""
        self.tenant_id: str = ""
        self.recent_messages: list[dict[str, Any]] = []
        self._pending_messages: list[dict[str, Any]] = []
        self._send_lock = threading.Lock()
        self._recv_lock = threading.Lock()

    def __enter__(self) -> "TestbedClient":
        if self.ws is None:
            self.connect()
        return self

    def __exit__(self, *_exc: object) -> None:
        self.close()

    def connect(self, *, sends: list[str] | None = None, receives: list[str] | None = None) -> dict[str, Any]:
        send_topics = _normalize_capabilities(sends or [])
        receive_topics = _normalize_capabilities(receives or [])
        self.ws = websocket.create_connection(self.url, timeout=30)
        self._pending_messages.clear()
        response = self._request(
            "command",
            "connection.open",
            {
                "name": self.name,
                "send_topics": send_topics,
                "receive_topics": receive_topics,
                "auth_token": kernel_auth_token(),
                "meta": self.meta,
            },
            request_id=f"open-{uuid.uuid4().hex}",
        )
        return response

    def create_session(
        self,
        session: str,
        *,
        tenant_id: str = "default",
        driver_component_id: str = DEFAULT_DRIVER_COMPONENT_ID,
        agent_spec_revision: str = DEFAULT_AGENT_SPEC_REVISION,
    ) -> dict[str, Any]:
        response = self._request(
            "command",
            "session.create",
            {
                "driver_component_id": driver_component_id,
                "agent_spec_revision": agent_spec_revision,
            },
            tenant_id=tenant_id,
            session_id=session,
        )
        self._bind_scope(tenant_id, session)
        return response

    def get_session(self, session: str, *, tenant_id: str = "default") -> dict[str, Any]:
        response = self._request("query", "session.get", {}, tenant_id=tenant_id, session_id=session)
        self._bind_scope(tenant_id, session)
        return response

    def subscribe(self, session: str, *, tenant_id: str = "default", after_cursor: str = "cur_0", limit: int = 256) -> dict[str, Any]:
        response = self._request(
            "query",
            "session.subscribe",
            {"after_cursor": after_cursor, "limit": limit},
            tenant_id=tenant_id,
            session_id=session,
        )
        self._bind_scope(tenant_id, session)
        return response

    def submit_input(
        self,
        input_id: str,
        content: dict[str, Any],
        *,
        expected_session_version: int | None = None,
        timeout: float = 10,
    ) -> dict[str, Any]:
        data: dict[str, Any] = {"input_id": input_id, "content": content}
        if expected_session_version is not None:
            data["expected_session_version"] = expected_session_version
        return self._request(
            "command",
            "input.submit",
            data,
            tenant_id=self._require_tenant(),
            session_id=self._require_session(),
            timeout=timeout,
            expected_op="input.accepted",
        )

    def call_tool(self, name: str, input: dict[str, Any] | None = None, *, timeout: float = 10, meta: dict[str, Any] | None = None) -> ToolResult:
        call_id = f"tb-{uuid.uuid4().hex}"
        data: dict[str, Any] = {"name": name, "input": input or {}}
        if meta:
            data["meta"] = meta
        try:
            msg = self._request(
                "command",
                "tool.call",
                data,
                tenant_id=self._require_tenant(),
                session_id=self._require_session(),
                request_id=call_id,
                timeout=timeout,
                expected_op="tool.result",
            )
        except TimeoutError as exc:
            raise TimeoutError(f"timed out waiting for tool.result: tool={name!r} id={call_id!r} client={self.name!r} recent={self.recent_messages[-10:]!r}") from exc
        result = msg.get("data") if isinstance(msg.get("data"), dict) else {}
        meta = result.get("meta") if isinstance(result.get("meta"), dict) else None
        return ToolResult(
            name=str(result.get("name") or name),
            id=call_id,
            output=str(result.get("output") or ""),
            artifact=result.get("artifact"),
            truncated=bool(result.get("truncated", False)),
            meta=meta,
        )

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

    def send_extension(
        self,
        message: dict[str, Any],
        *,
        tenant_id: str | None = None,
        session_id: str | None = None,
        timeout: float = 10,
    ) -> dict[str, Any]:
        return self._request(
            "command",
            "extension.send",
            dict(message),
            tenant_id=tenant_id or self._require_tenant(),
            session_id=session_id or self._require_session(),
            timeout=timeout,
        )

    def wait_for(self, predicate: Callable[[dict[str, Any]], bool], *, timeout: float = 10) -> dict[str, Any]:
        deadline = time.time() + timeout
        while time.time() < deadline:
            msg = self.recv(timeout=max(0.1, deadline - time.time()))
            if predicate(msg):
                return msg
        raise TimeoutError("timed out waiting for testbed message")

    def recv(self, *, op: str | None = None, timeout: float = 10) -> dict[str, Any]:
        if self.ws is None:
            raise RuntimeError("client is not connected")
        deadline = time.time() + timeout
        with self._recv_lock:
            while time.time() < deadline:
                if self._pending_messages:
                    msg = self._pending_messages.pop(0)
                else:
                    msg = self._recv_wire(timeout=max(0.1, deadline - time.time()))
                if op is None or msg.get("op") == op:
                    return msg
        wanted = f" op={op!r}" if op else ""
        raise TimeoutError(f"timed out waiting for message{wanted} client={self.name!r}")

    def close(self) -> None:
        if self.ws is not None:
            self.ws.close()
            self.ws = None
        self.session = ""
        self.tenant_id = ""
        self._pending_messages.clear()

    def _request(
        self,
        kind: str,
        op: str,
        data: dict[str, Any],
        *,
        tenant_id: str | None = None,
        session_id: str | None = None,
        request_id: str | None = None,
        timeout: float = 10,
        expected_op: str | None = None,
    ) -> dict[str, Any]:
        request_id = request_id or f"request-{uuid.uuid4().hex}"
        envelope: dict[str, Any] = {
            "v": PROTOCOL_VERSION,
            "kind": kind,
            "op": op,
            "id": request_id,
            "data": data,
        }
        if tenant_id:
            envelope["tenant_id"] = tenant_id
        if session_id:
            envelope["session_id"] = session_id
        self._send_wire(envelope)

        deadline = time.time() + timeout
        with self._recv_lock:
            while time.time() < deadline:
                pending = self._pop_pending_response(request_id, expected_op or op)
                if pending is not None:
                    return self._checked_response(pending)
                msg = self._recv_wire(timeout=max(0.1, deadline - time.time()))
                if _is_response(msg, request_id, expected_op or op):
                    return self._checked_response(msg)
                self._pending_messages.append(msg)
        raise TimeoutError(f"timed out waiting for protocol-v4 response op={expected_op or op!r} id={request_id!r} client={self.name!r}")

    def _checked_response(self, msg: dict[str, Any]) -> dict[str, Any]:
        if msg.get("kind") == "error":
            raise ProtocolError(msg)
        return msg

    def _pop_pending_response(self, request_id: str, op: str) -> dict[str, Any] | None:
        for index, msg in enumerate(self._pending_messages):
            if _is_response(msg, request_id, op):
                return self._pending_messages.pop(index)
        return None

    def _recv_wire(self, *, timeout: float) -> dict[str, Any]:
        if self.ws is None:
            raise RuntimeError("client is not connected")
        old_timeout = self.ws.gettimeout()
        self.ws.settimeout(timeout)
        try:
            raw = self.ws.recv()
            msg = _decode_wire_message(raw)
            self.recent_messages.append(msg)
            if len(self.recent_messages) > 50:
                del self.recent_messages[: len(self.recent_messages) - 50]
            return msg
        except (TimeoutError, socket.timeout, WebSocketTimeoutException) as exc:
            raise TimeoutError(f"timed out waiting for message client={self.name!r}") from exc
        finally:
            self.ws.settimeout(old_timeout)

    def _send_wire(self, msg: dict[str, Any]) -> None:
        if self.ws is None:
            raise RuntimeError("client is not connected")
        with self._send_lock:
            self.ws.send(json.dumps(msg))

    def _bind_scope(self, tenant_id: str, session: str) -> None:
        if self.tenant_id and self.tenant_id != tenant_id:
            raise RuntimeError(f"client already bound to tenant {self.tenant_id!r}")
        if self.session and self.session != session:
            raise RuntimeError(f"client already bound to session {self.session!r}")
        self.tenant_id = tenant_id
        self.session = session

    def _require_session(self) -> str:
        if not self.session:
            raise RuntimeError("client is not bound to a session")
        return self.session

    def _require_tenant(self) -> str:
        if not self.tenant_id:
            raise RuntimeError("client is not bound to a tenant")
        return self.tenant_id


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


def _decode_wire_message(raw: str | bytes) -> dict[str, Any]:
    return json.loads(raw)


def _is_response(msg: dict[str, Any], request_id: str, op: str) -> bool:
    if msg.get("id") != request_id:
        return False
    if msg.get("kind") == "error":
        return True
    return msg.get("op") == op and msg.get("kind") in {"result", "reply"}


def _normalize_capabilities(items: list[str]) -> list[str]:
    return list(dict.fromkeys(items))

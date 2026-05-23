"""Background approval responder for testbed scenarios.

Approval flow in Tabula is generic: any tool whose ``before_tool_call``
permissions evaluate to ``ask`` triggers ``hook-approvals`` to open an
``exchange.approve`` exchange. An interactive approval UI joins the
session, binds ``exchange.approve`` capability and replies with the user's
choice. In headless tests we stand in for that UI by running a small
background WebSocket client that auto-answers each request.

This module is intentionally tool-agnostic. Tests can target any tool that
goes through the same hook path (``exec_run``, ``exec_run_background``,
``fs_write``-with-ask, MCP tools, future tools), provided they bind the
generic ``exchange.approve`` topic.

Kernel policy requires a client to be joined to a session before it can
send replies on an exchange topic (see ``policy.go`` ``CanSend`` rule
``client not in a session``). The responder therefore joins the same
session whose tool calls are expected to trigger approvals; the real
approval UIs (ACP gateway, web gateway) follow the same model.
"""

from __future__ import annotations

import json
import socket
import threading
from typing import Any, Callable

import websocket
from websocket import WebSocketTimeoutException

from .client import kernel_auth_token

APPROVE_TOPIC = "exchange.approve"

ChoiceFn = Callable[[dict[str, Any]], str]


class ApprovalResponder:
    """Background WS client that answers ``exchange.approve`` requests.

    Use as a context manager around any block that is expected to trigger a
    ``before_tool_call`` approval. Each incoming ``exchange.approve``
    request is logged into :attr:`requests` and a reply is sent back using
    either the static ``choice`` argument or the ``choice_fn`` callback
    when finer-grained per-request decisions are needed.

    Parameters
    ----------
    url:
        Kernel WebSocket URL (e.g. ``ws://127.0.0.1:8089/ws``).
    session:
        Session id to join. Must match the session whose tool calls will
        trigger approval prompts.
    tenant_id:
        Tenant id to join with. Defaults to ``"default"``.
    choice / choice_fn:
        Static answer or per-request callback returning the choice label.
    name:
        Client name for logging/diagnostics on the kernel side.
    """

    def __init__(
        self,
        url: str,
        *,
        session: str,
        tenant_id: str = "default",
        choice: str = "allow once",
        choice_fn: ChoiceFn | None = None,
        name: str = "testbed-approver",
    ) -> None:
        self.url = url
        self.session = session
        self.tenant_id = tenant_id
        self.choice = choice
        self.choice_fn = choice_fn
        self.name = name
        self.requests: list[dict[str, Any]] = []
        self._stop = threading.Event()
        self._ready = threading.Event()
        self._thread: threading.Thread | None = None
        self._error: BaseException | None = None

    @property
    def handled(self) -> bool:
        """True if the responder has answered at least one approval."""
        return bool(self.requests)

    def __enter__(self) -> "ApprovalResponder":
        self._thread = threading.Thread(target=self._run, name=self.name, daemon=True)
        self._thread.start()
        if not self._ready.wait(timeout=5):
            self._stop.set()
            raise RuntimeError("approval responder failed to connect within 5s")
        if self._error is not None:
            raise self._error
        return self

    def __exit__(self, *_exc: object) -> None:
        self._stop.set()
        if self._thread is not None:
            self._thread.join(timeout=5)
        if self._error is not None:
            raise self._error

    def _decide(self, payload: dict[str, Any]) -> str:
        if self.choice_fn is not None:
            return self.choice_fn(payload)
        return self.choice

    def _run(self) -> None:
        try:
            ws = websocket.create_connection(self.url, timeout=10)
        except BaseException as exc:  # noqa: BLE001 - propagate to test thread
            self._error = exc
            self._ready.set()
            return
        try:
            ws.send(json.dumps({
                "v": 3,
                "type": "hello",
                "data": {
                    "name": self.name,
                    "send_topics": [APPROVE_TOPIC],
                    "receive_topics": [APPROVE_TOPIC],
                    "auth_token": kernel_auth_token(),
                },
            }))
            ack = json.loads(ws.recv())
            if ack.get("type") != "hello_ack":
                raise RuntimeError(f"approver hello_ack expected, got {ack!r}")
            ws.send(json.dumps({
                "v": 3,
                "type": "join",
                "session": self.session,
                "tenant_id": self.tenant_id,
            }))
            # Drain post-join messages until we see `joined`. Kernel may
            # also emit `session.init`, `session.member_joined`, etc. — we
            # only need confirmation that the join succeeded.
            ws.settimeout(5)
            while True:
                joined = json.loads(ws.recv())
                jtype = joined.get("type")
                if jtype == "joined":
                    break
                if jtype == "error":
                    raise RuntimeError(f"approver join failed: {joined!r}")
            ws.settimeout(0.25)
            self._ready.set()
            while not self._stop.is_set():
                try:
                    raw = ws.recv()
                except (TimeoutError, socket.timeout, WebSocketTimeoutException):
                    continue
                if not raw:
                    continue
                msg = json.loads(raw)
                if msg.get("type") != "request" or msg.get("topic") != APPROVE_TOPIC:
                    continue
                payload = msg.get("data") if isinstance(msg.get("data"), dict) else {}
                self.requests.append(payload)
                ws.send(json.dumps({
                    "v": 3,
                    "type": "reply",
                    "topic": APPROVE_TOPIC,
                    "id": msg.get("id"),
                    "session": msg.get("session") or self.session,
                    "data": {"choice": self._decide(payload)},
                }))
        except BaseException as exc:  # noqa: BLE001 - propagate to test thread
            self._error = exc
            self._ready.set()
        finally:
            try:
                ws.close()
            except Exception:
                pass


__all__ = ["ApprovalResponder", "APPROVE_TOPIC"]

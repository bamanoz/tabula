"""Background approval responder for testbed scenarios.

Approval flow in Tabula is generic: any tool whose ``before_tool_call``
permissions evaluate to ``ask`` can suspend the tool call in the kernel.
The responder uses protocol-v4 ``connection.open`` and durable session
operations through :class:`TestbedClient`. Approval prompts and replies remain
extension traffic because they are non-authoritative UI exchanges.
"""

from __future__ import annotations

import socket
import threading
from typing import Any, Callable

from websocket import WebSocketTimeoutException

from .client import ProtocolError, TestbedClient

APPROVE_TOPIC = "exchange.approve"

ApprovalFn = Callable[[dict[str, Any]], bool]


class ApprovalResponder:
    """Background client that answers ``exchange.approve`` requests."""

    def __init__(
        self,
        url: str,
        *,
        session: str,
        tenant_id: str = "default",
        approved: bool = True,
        approved_fn: ApprovalFn | None = None,
        name: str = "testbed-approver",
    ) -> None:
        self.url = url
        self.session = session
        self.tenant_id = tenant_id
        self.approved = approved
        self.approved_fn = approved_fn
        self.name = name
        self.requests: list[dict[str, Any]] = []
        self._stop = threading.Event()
        self._ready = threading.Event()
        self._thread: threading.Thread | None = None
        self._error: BaseException | None = None
        self._client: TestbedClient | None = None

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
        if self._client is not None:
            self._client.close()
        if self._thread is not None:
            self._thread.join(timeout=5)
        if self._error is not None:
            raise self._error

    def _decide(self, payload: dict[str, Any]) -> bool:
        if self.approved_fn is not None:
            return bool(self.approved_fn(payload))
        return bool(self.approved)

    def _run(self) -> None:
        client = TestbedClient(self.url, name=self.name)
        self._client = client
        try:
            client.connect(sends=[APPROVE_TOPIC], receives=[APPROVE_TOPIC])
            try:
                snapshot = client.get_session(self.session, tenant_id=self.tenant_id)
            except ProtocolError as exc:
                if exc.code != "not_found":
                    raise
                client.create_session(self.session, tenant_id=self.tenant_id)
                snapshot = client.get_session(self.session, tenant_id=self.tenant_id)
            cursor = snapshot.get("data", {}).get("cursor", "cur_0")
            client.subscribe(self.session, tenant_id=self.tenant_id, after_cursor=cursor)

            client.send_extension({"type": "join"})
            while True:
                joined = client.recv(op="extension.event")
                extension = joined.get("data") if isinstance(joined.get("data"), dict) else {}
                if extension.get("type") == "joined":
                    break

            self._ready.set()
            while not self._stop.is_set():
                try:
                    msg = client.recv(timeout=0.25)
                except (TimeoutError, socket.timeout, WebSocketTimeoutException):
                    continue
                if msg.get("kind") != "event" or msg.get("op") != "extension.event":
                    continue
                extension = msg.get("data") if isinstance(msg.get("data"), dict) else {}
                if extension.get("type") != "request" or extension.get("topic") != APPROVE_TOPIC:
                    continue
                payload = extension.get("data") if isinstance(extension.get("data"), dict) else {}
                self.requests.append(payload)
                client.send_extension(
                    {
                        "type": "reply",
                        "topic": APPROVE_TOPIC,
                        "id": extension.get("id") or msg.get("id"),
                        "data": {"approved": self._decide(payload)},
                    }
                )
        except BaseException as exc:  # noqa: BLE001 - propagate to test thread
            if not self._stop.is_set():
                self._error = exc
            self._ready.set()
        finally:
            client.close()


__all__ = ["ApprovalResponder", "APPROVE_TOPIC"]

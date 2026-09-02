from __future__ import annotations

import time
import unittest
from unittest import mock

from .approvals import APPROVE_TOPIC, ApprovalResponder
from .client import ProtocolError


class FakeClient:
    instances: list["FakeClient"] = []

    def __init__(self, url: str, *, name: str) -> None:
        self.url = url
        self.name = name
        self.calls: list[tuple[str, dict]] = []
        self.sent: list[dict] = []
        self.closed = False
        self._session_gets = 0
        self._messages = [
            {
                "v": 4,
                "kind": "event",
                "op": "extension.event",
                "id": "join-1",
                "tenant_id": "default",
                "session_id": "session-1",
                "data": {"type": "joined"},
            },
            {
                "v": 4,
                "kind": "event",
                "op": "extension.event",
                "id": "approval-1",
                "tenant_id": "default",
                "session_id": "session-1",
                "data": {"type": "request", "topic": APPROVE_TOPIC, "id": "approval-1", "data": {"question": "allow?"}},
            },
        ]
        self.instances.append(self)

    def connect(self, **kwargs: object) -> dict:
        self.calls.append(("connect", dict(kwargs)))
        return {"kind": "result", "op": "connection.open"}

    def get_session(self, session: str, **kwargs: object) -> dict:
        self.calls.append(("get_session", {"session": session, **kwargs}))
        self._session_gets += 1
        if self._session_gets == 1:
            raise ProtocolError({"kind": "error", "op": "session.get", "data": {"code": "not_found", "message": "not found"}})
        return {"kind": "reply", "op": "session.get", "data": {"cursor": "cur_1"}}

    def create_session(self, session: str, **kwargs: object) -> dict:
        self.calls.append(("create_session", {"session": session, **kwargs}))
        return {"kind": "result", "op": "session.create"}

    def subscribe(self, session: str, **kwargs: object) -> dict:
        self.calls.append(("subscribe", {"session": session, **kwargs}))
        return {"kind": "reply", "op": "session.subscribe"}

    def recv(self, *, op: str | None = None, timeout: float = 10) -> dict:
        if self._messages:
            message = self._messages.pop(0)
            if op is not None and message["op"] != op:
                raise AssertionError(f"unexpected op filter {op!r}: {message!r}")
            return message
        time.sleep(min(timeout, 0.01))
        raise TimeoutError("no more messages")

    def send_extension(self, message: dict) -> dict:
        self.sent.append(message)
        return {"kind": "result", "op": "extension.send", "data": {"accepted": True}}

    def close(self) -> None:
        self.closed = True


class ApprovalResponderV4Test(unittest.TestCase):
    def test_responder_uses_explicit_v4_session_flow_and_native_extension_event(self) -> None:
        FakeClient.instances.clear()
        with mock.patch("tabula_testbed.approvals.TestbedClient", FakeClient):
            with ApprovalResponder("ws://test/ws", session="session-1", approved_fn=lambda payload: payload["question"] == "allow?") as responder:
                deadline = time.time() + 1
                while not responder.handled and time.time() < deadline:
                    time.sleep(0.01)
                self.assertTrue(responder.handled)

        client = FakeClient.instances[0]
        self.assertEqual(client.calls, [
            ("connect", {"sends": [APPROVE_TOPIC], "receives": [APPROVE_TOPIC]}),
            ("get_session", {"session": "session-1", "tenant_id": "default"}),
            ("create_session", {"session": "session-1", "tenant_id": "default"}),
            ("get_session", {"session": "session-1", "tenant_id": "default"}),
            ("subscribe", {"session": "session-1", "tenant_id": "default", "after_cursor": "cur_1"}),
        ])
        self.assertEqual(client.sent, [
            {"type": "join"},
            {
                "type": "reply",
                "topic": APPROVE_TOPIC,
                "id": "approval-1",
                "data": {"approved": True},
            },
        ])
        self.assertTrue(client.closed)


if __name__ == "__main__":
    unittest.main()

from __future__ import annotations

import json
import unittest
from unittest import mock

from .client import ProtocolError, TestbedClient, _decode_wire_message


class FakeWebSocket:
    def __init__(self) -> None:
        self.sent: list[dict] = []
        self.responses: list[dict] = []
        self.timeout = 30
        self.session_gets = 0
        self.closed = False

    def send(self, raw: str) -> None:
        message = json.loads(raw)
        self.sent.append(message)
        op = message["op"]
        request_id = message["id"]
        base = {
            "v": 4,
            "id": request_id,
            "tenant_id": message.get("tenant_id"),
            "session_id": message.get("session_id"),
        }
        if op == "connection.open":
            self.responses.append({**base, "kind": "result", "op": op, "data": {"client_id": "c1", "server_protocol": 4}})
        elif op == "session.get":
            self.session_gets += 1
            if self.session_gets == 1:
                self.responses.append({**base, "kind": "error", "op": op, "data": {"code": "not_found", "message": "not found", "retryable": False}})
            else:
                self.responses.append({**base, "kind": "reply", "op": op, "data": {"projection": {"session_version": 1}, "cursor": "cur_1"}})
        elif op == "session.create":
            self.responses.append({**base, "kind": "result", "op": op, "data": {"session_version": 1, "cursor": "cur_1", "duplicate": False}})
        elif op == "session.subscribe":
            self.responses.append({**base, "kind": "reply", "op": op, "data": {"events": [], "cursor": "cur_1"}})
        elif op == "extension.send" and message["data"].get("type") == "join":
            self.responses.extend([
                _extension_event("joined", message),
                {**base, "kind": "result", "op": op, "data": {"accepted": True}},
            ])
        elif op == "extension.send":
            self.responses.append({**base, "kind": "result", "op": op, "data": {"accepted": True}})
        elif op == "input.submit":
            self.responses.append({**base, "kind": "result", "op": "input.accepted", "data": {"input_id": message["data"]["input_id"], "turn_id": "turn-1", "cursor": "cur_2"}})
        elif op == "tool.call":
            self.responses.extend([
                {**base, "kind": "event", "op": "tool.result.delta", "data": {"name": message["data"]["name"], "text": "chunk", "state": "streaming"}},
                {**base, "kind": "result", "op": "tool.result", "data": {"name": message["data"]["name"], "output": '{"ok":true}', "artifact": {"ref": "artifact-1"}, "truncated": True, "meta": {"source": "test"}}},
            ])
        else:
            raise AssertionError(f"unexpected request: {message!r}")

    def recv(self) -> str:
        if not self.responses:
            raise AssertionError("fake websocket has no queued response")
        return json.dumps(self.responses.pop(0))

    def gettimeout(self) -> float:
        return self.timeout

    def settimeout(self, value: float) -> None:
        self.timeout = value

    def close(self) -> None:
        self.closed = True


def _extension_event(message_type: str, request: dict, **fields: object) -> dict:
    return {
        "v": 4,
        "kind": "event",
        "op": "extension.event",
        "id": request["id"],
        "tenant_id": request["tenant_id"],
        "session_id": request["session_id"],
        "data": {"type": message_type, **fields},
    }


class TestbedClientV4Test(unittest.TestCase):
    def setUp(self) -> None:
        self.socket = FakeWebSocket()
        patcher = mock.patch("tabula_testbed.client.websocket.create_connection", return_value=self.socket)
        self.addCleanup(patcher.stop)
        patcher.start()

    def test_explicit_session_flow_binds_scope_and_extension_join(self) -> None:
        client = TestbedClient("ws://test/ws", name="test-client", meta={"tabula.client_role": "user"})
        client.connect()
        with self.assertRaises(ProtocolError) as raised:
            client.get_session("session-1")
        self.assertEqual(raised.exception.code, "not_found")

        client.create_session("session-1")
        snapshot = client.get_session("session-1")
        subscription = client.subscribe("session-1", after_cursor=snapshot["data"]["cursor"])
        client.send_extension({"type": "join"})
        joined = client.recv(op="extension.event")

        self.assertEqual([message["op"] for message in self.socket.sent], [
            "connection.open",
            "session.get",
            "session.create",
            "session.get",
            "session.subscribe",
            "extension.send",
        ])
        self.assertEqual(self.socket.sent[0]["data"]["send_topics"], [])
        self.assertEqual(self.socket.sent[0]["data"]["receive_topics"], [])
        self.assertEqual(self.socket.sent[2]["data"], {
            "driver_component_id": "driver",
            "agent_spec_revision": "testbed:shared-client-v4",
        })
        self.assertEqual(self.socket.sent[4]["data"], {"after_cursor": "cur_1", "limit": 256})
        self.assertEqual(self.socket.sent[5]["data"], {"type": "join"})
        self.assertEqual(client.tenant_id, "default")
        self.assertEqual(client.session, "session-1")
        self.assertEqual(subscription["op"], "session.subscribe")
        self.assertEqual(joined["op"], "extension.event")
        self.assertEqual(joined["data"]["type"], "joined")

    def test_submit_input_call_tool_and_send_extension_are_typed(self) -> None:
        client = TestbedClient("ws://test/ws", name="test-client")
        client.connect(sends=["exchange.choose"], receives=["exchange.choose"])
        client.create_session("session-1")
        self.socket.sent.clear()

        accepted = client.submit_input("input-1", {"type": "text", "text": "hello"}, expected_session_version=1)
        result = client.call_tool("echo", {"value": "hello"})
        delta = client.recv(op="tool.result.delta")
        client.send_extension({"type": "reply", "topic": "exchange.choose", "id": "question-1", "data": {"answers": [["yes"]]}})

        self.assertEqual(accepted["op"], "input.accepted")
        self.assertEqual(result.json(), {"ok": True})
        self.assertEqual(result.artifact, {"ref": "artifact-1"})
        self.assertTrue(result.truncated)
        self.assertEqual(result.meta, {"source": "test"})
        self.assertEqual(delta["kind"], "event")
        self.assertEqual(delta["op"], "tool.result.delta")
        self.assertEqual([message["op"] for message in self.socket.sent], ["input.submit", "tool.call", "extension.send"])
        self.assertEqual(self.socket.sent[0]["data"], {
            "input_id": "input-1",
            "content": {"type": "text", "text": "hello"},
            "expected_session_version": 1,
        })
        self.assertEqual(self.socket.sent[1]["data"], {"name": "echo", "input": {"value": "hello"}})
        self.assertEqual(self.socket.sent[2]["data"]["topic"], "exchange.choose")

    def test_decode_extension_event_preserves_native_v4_envelope(self) -> None:
        envelope = {
            "v": 4,
            "kind": "event",
            "op": "extension.event",
            "id": "approval-1",
            "tenant_id": "tenant",
            "session_id": "session",
            "data": {"type": "request", "topic": "exchange.approve", "data": {"question": "allow?"}},
        }
        self.assertEqual(_decode_wire_message(json.dumps(envelope)), envelope)


if __name__ == "__main__":
    unittest.main()

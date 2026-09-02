#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
import uuid
import unittest

import websocket


class ClientV4ReconnectInstalled(unittest.TestCase):
    url = "ws://localhost:8089/ws"

    def test_input_acceptance_survives_client_reconnect(self) -> None:
        session_id = f"testbed-client-v4-{uuid.uuid4().hex}"
        tenant_id = "default"
        input_id = f"input-{uuid.uuid4().hex}"
        first = connect_v4(self.url, "testbed-client-v4-first")
        try:
            created = request(
                first,
                kind="command",
                op="session.create",
                tenant_id=tenant_id,
                session_id=session_id,
                data={"driver_component_id": "driver", "agent_spec_revision": "testbed:v4-reconnect"},
            )
            self.assertEqual(created.get("kind"), "result", created)
            self.assertEqual(created.get("op"), "session.create", created)
            accepted = request(
                first,
                kind="command",
                op="input.submit",
                tenant_id=tenant_id,
                session_id=session_id,
                data={"input_id": input_id, "content": {"text": "durable input"}},
            )
        finally:
            first.close()

        self.assertEqual(accepted["kind"], "result")
        self.assertEqual(accepted["op"], "input.accepted")
        self.assertEqual(accepted["data"]["input_id"], input_id)
        cursor = accepted["data"]["cursor"]

        second = connect_v4(self.url, "testbed-client-v4-second")
        try:
            snapshot = request(second, kind="query", op="session.get", tenant_id=tenant_id, session_id=session_id, data={})
            replay = request(
                second,
                kind="query",
                op="session.subscribe",
                tenant_id=tenant_id,
                session_id=session_id,
                data={"after_cursor": "cur_0", "limit": 64},
            )
        finally:
            second.close()

        self.assertEqual(snapshot["kind"], "reply")
        self.assertEqual(snapshot["op"], "session.get")
        self.assertGreaterEqual(int(snapshot["data"]["projection"]["session_version"]), 1)
        self.assertEqual(snapshot["data"]["cursor"], cursor)
        self.assertTrue(
            any(event["type"] == "input.accepted" for event in replay["data"]["events"]),
            replay,
        )


def connect_v4(url: str, name: str):
    socket = websocket.create_connection(url, timeout=10)
    request_id = f"open-{uuid.uuid4().hex}"
    socket.send(json.dumps({
        "v": 4,
        "kind": "command",
        "op": "connection.open",
        "id": request_id,
        "data": {"name": name, "auth_token": kernel_auth_token(), "meta": {"tabula.client_role": "user"}},
    }))
    opened = json.loads(socket.recv())
    if opened.get("kind") != "result" or opened.get("op") != "connection.open" or opened.get("id") != request_id:
        socket.close()
        raise AssertionError(f"expected connection.open result, got {opened!r}")
    return socket


def request(socket, *, kind: str, op: str, tenant_id: str, session_id: str, data: dict) -> dict:
    request_id = f"request-{uuid.uuid4().hex}"
    socket.send(json.dumps({
        "v": 4,
        "kind": kind,
        "op": op,
        "id": request_id,
        "tenant_id": tenant_id,
        "session_id": session_id,
        "data": data,
    }))
    while True:
        response = json.loads(socket.recv())
        if response.get("id") != request_id:
            continue
        if response.get("kind") == "error":
            raise AssertionError(f"v4 {op} failed: {response!r}")
        return response


def kernel_auth_token() -> str:
    path = os.path.join(os.environ["TABULA_HOME"], "run", "kernel-client-token")
    with open(path, encoding="utf-8") as stream:
        return stream.read().strip()


def main() -> int:
    parser = argparse.ArgumentParser(description="Run installed client protocol v4 reconnect tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    ClientV4ReconnectInstalled.url = args.url
    result = unittest.TextTestRunner(verbosity=2).run(
        unittest.defaultTestLoader.loadTestsFromTestCase(ClientV4ReconnectInstalled)
    )
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

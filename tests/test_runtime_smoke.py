#!/usr/bin/env python3
"""Runtime smoke tests for boot -> connect -> init -> message -> done."""

from __future__ import annotations

import json
import shutil

from tests.runtime_harness import collect_until_done
from tests.runtime_harness import collect_turn_text
from tests.runtime_harness import connect_gateway
from tests.runtime_harness import create_test_home
from tests.runtime_harness import get_free_port
from tests.runtime_harness import start_kernel
from tests.runtime_harness import terminate_process
from tests.runtime_harness import wait_for_session_client


def test_kernel_boot_connect_init_and_done_smoke():
    tabula_port = get_free_port()
    home = create_test_home(
        tabula_port,
        system_prompt="runtime smoke prompt",
        spawn=[".venv/bin/python3 skills/driver-mock/run.py"],
    )
    proc = None
    conn = None
    try:
        proc = start_kernel(
            home,
            tabula_port,
            extra_env={
                "TABULA_MOCK_SUBAGENTS": "1",
                "TABULA_MOCK_TURNS": "2",
                "TABULA_MOCK_SLEEP_MS": "1",
                "TABULA_MOCK_WAIT_SEC": "5",
            },
        )
        wait_for_session_client(tabula_port, session="main", client_name="mock", timeout=5)
        conn, connected, joined, init_msg = connect_gateway(tabula_port)

        assert connected["type"] == "connected"
        assert joined["type"] == "joined"
        assert init_msg["type"] == "init"
        assert init_msg["prompt"] == "runtime smoke prompt"

        tools = init_msg["tools"]
        assert {tool["name"] for tool in tools} >= {"EXEC", "SPAWN", "KILL", "LIST"}

        conn.send(json.dumps({"type": "message", "text": "smoke test request"}))
        output = collect_turn_text(conn, timeout=10)
        assert "request: smoke test request" in output
        assert "aggregated subagent results" in output
    finally:
        if conn is not None:
            conn.close()
        terminate_process(proc)
        shutil.rmtree(home, ignore_errors=True)


def test_kernel_rejects_concurrent_root_message_until_turn_finishes():
    tabula_port = get_free_port()
    home = create_test_home(
        tabula_port,
        system_prompt="runtime busy smoke prompt",
        spawn=[".venv/bin/python3 skills/driver-mock/run.py"],
    )
    proc = None
    conn = None
    try:
        proc = start_kernel(
            home,
            tabula_port,
            extra_env={
                "TABULA_MOCK_SUBAGENTS": "2",
                "TABULA_MOCK_TURNS": "2",
                "TABULA_MOCK_SLEEP_MS": "5",
                "TABULA_MOCK_WAIT_SEC": "5",
            },
        )
        wait_for_session_client(tabula_port, session="main", client_name="mock", timeout=5)
        conn, _, _, _ = connect_gateway(tabula_port)

        conn.send(json.dumps({"type": "message", "text": "primary request"}))
        conn.send(json.dumps({"type": "message", "text": "secondary request"}))

        messages = collect_until_done(conn, timeout=10)
        output = "".join(msg.get("text", "") for msg in messages if msg.get("type") == "stream_delta")
        errors = [msg.get("text", "") for msg in messages if msg.get("type") == "error"]

        assert "request: primary request" in output
        assert "secondary request" not in output
        assert "busy, ignoring concurrent user message" not in output
        assert any("session busy" in text for text in errors), errors

        conn.send(json.dumps({"type": "message", "text": "after reset"}))
        second_output = collect_turn_text(conn, timeout=10)
        assert "request: after reset" in second_output
    finally:
        if conn is not None:
            conn.close()
        terminate_process(proc)
        shutil.rmtree(home, ignore_errors=True)

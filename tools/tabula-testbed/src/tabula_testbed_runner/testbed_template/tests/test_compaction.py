#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import subprocess
import sys
import time
import unittest

from tabula_testbed import TestbedClient


FAKE_OPENAI = """from types import SimpleNamespace


class _Stream:
    def __init__(self, text):
        delta = SimpleNamespace(content=text, tool_calls=[])
        choice = SimpleNamespace(delta=delta)
        self._chunks = [SimpleNamespace(choices=[choice], usage=None)]

    def __iter__(self):
        return iter(self._chunks)

    def close(self):
        return None


class _Completions:
    def create(self, **kwargs):
        messages = kwargs.get("messages", []) or []
        if kwargs.get("stream"):
            user_messages = [m for m in messages if m.get("role") == "user"]
            turn = len(user_messages)
            return _Stream(f"turn-{turn}-ok")
        summary = f"<summary>fake summary for {len(messages)} messages</summary>"
        message = SimpleNamespace(content=summary)
        choice = SimpleNamespace(message=message)
        return SimpleNamespace(choices=[choice])


class _Chat:
    def __init__(self):
        self.completions = _Completions()


class _Responses:
    def create(self, **kwargs):
        text = '<summary>fake summary for responses api</summary>'
        content = [SimpleNamespace(type='output_text', text=text)]
        message = SimpleNamespace(type='message', content=content)
        return SimpleNamespace(output=[message])


class OpenAI:
    def __init__(self, api_key=None, base_url=None):
        self.api_key = api_key
        self.base_url = base_url
        self.chat = _Chat()
        self.responses = _Responses()
"""


class CompactionSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def test_driver_history_compacts_and_restores_after_restart(self):
        home = Path(self.tabula_home)
        session = "testbed-compaction"
        stub_dir = home / "data" / "testbed" / "fake-openai"
        stub_dir.mkdir(parents=True, exist_ok=True)
        (stub_dir / "openai.py").write_text(FAKE_OPENAI, encoding="utf-8")
        (home / "plugins" / "driver" / "openai.py").write_text(FAKE_OPENAI, encoding="utf-8")
        driver_config = home / "config" / "plugins" / "driver" / "config.toml"
        driver_config.parent.mkdir(parents=True, exist_ok=True)
        driver_config.write_text(
            """
provider = "openai"
session_autostart = false
compaction_threshold = 0.0001

[providers.openai]
type = "openai"
api_key = "test-key"
base_url = "https://api.openai.invalid/v1"
default_model = "o3"
api = "chat_completions"
""".strip()
            + "\n",
            encoding="utf-8",
        )

        proc, log_handle, log_path = self.start_driver(session, stub_dir)
        try:
            with TestbedClient(self.url, name="testbed-compaction-client") as client:
                client.connect()
                client.create_session(session)
                events = self.session_events(client, session)
                payload = "x" * 4096
                for turn in range(1, 4):
                    client.submit_input(f"input-{time.time_ns()}", {"type": "text", "text": f"turn {turn} {payload}"})
                    self.wait_for_assistant_count(client, events, turn, proc, log_path)

                self.wait_for_compaction(client, events, proc, log_path)
                compactions = [event for event in events if event.get("type") == "compaction"]
                summary = self.event_payload(compactions[-1]).get("text") or self.event_payload(compactions[-1]).get("summary") or ""
                self.assertIn("fake summary", summary)

            self.stop_driver(proc)
            log_handle.close()

            proc, log_handle, log_path = self.start_driver(session, stub_dir, suffix="-restart")
            with TestbedClient(self.url, name="testbed-compaction-client-restart") as client:
                client.connect()
                client.get_session(session)
                events = self.session_events(client, session)
                client.submit_input(f"input-{time.time_ns()}", {"type": "text", "text": "after restart"})
                self.wait_for_assistant_count(client, events, 4, proc, log_path)

            self.assertEqual(self.assistant_turn_count(events), 4)
            self.assertTrue(any(event.get("type") == "compaction" for event in events))
        finally:
            self.stop_driver(proc)
            log_handle.close()

    def start_driver(self, session: str, stub_dir: Path, *, suffix: str = "") -> tuple[subprocess.Popen[bytes], object, Path]:
        home = Path(self.tabula_home)
        python = home / ".venv" / "bin" / "python3"
        if not python.is_file():
            python = Path(sys.executable)
        driver = home / "plugins" / "driver" / "run.py"
        log_path = home / "logs" / f"testbed-compaction{suffix}.log"
        log_path.parent.mkdir(parents=True, exist_ok=True)
        env = os.environ.copy()
        env.update({
            "TABULA_HOME": self.tabula_home,
            "TABULA_URL": self.url,
            "TABULA_VERBOSE": "1",
            "TABULA_PLUGIN_DRIVER_OPENAI_API_KEY": "test-key",
            "TABULA_PLUGIN_DRIVER_OPENAI_MODEL": "o3",
            "TABULA_COMPACT_THRESHOLD": "0.0001",
            "TABULA_COMPACT_TAIL_TURNS": "1",
            "PYTHONPATH": f"{stub_dir}{os.pathsep}{env.get('PYTHONPATH', '')}" if env.get("PYTHONPATH") else str(stub_dir),
        })
        log_handle = log_path.open("wb")
        proc = subprocess.Popen(
            [str(python), str(driver), "--session", session, "--provider", "openai", "--no-tenant-binding"],
            env=env,
            stdout=log_handle,
            stderr=subprocess.STDOUT,
        )
        time.sleep(1)
        self.assertIsNone(proc.poll(), self.format_driver_log(log_path))
        return proc, log_handle, log_path

    def stop_driver(self, proc: subprocess.Popen[bytes]) -> None:
        if proc.poll() is not None:
            return
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=5)

    def session_events(self, client: TestbedClient, session: str) -> list[dict]:
        reply = client.subscribe(session, after_cursor="cur_0", limit=256)
        return list(reply.get("data", {}).get("events", []))

    def event_payload(self, event: dict) -> dict:
        data = event.get("data") if isinstance(event.get("data"), dict) else {}
        payload = data.get("payload")
        return payload if isinstance(payload, dict) else {}

    def receive_event(self, client: TestbedClient, events: list[dict], *, timeout: float) -> None:
        try:
            msg = client.recv(timeout=timeout)
        except TimeoutError:
            return
        if msg.get("kind") != "event":
            return
        event = msg.get("data") if isinstance(msg.get("data"), dict) else {}
        if event.get("event_id") and not any(item.get("event_id") == event.get("event_id") for item in events):
            events.append(event)

    def wait_for_compaction(self, client: TestbedClient, events: list[dict], proc: subprocess.Popen[bytes], log_path: Path, *, timeout: float = 10) -> None:
        deadline = time.time() + timeout
        while time.time() < deadline:
            self.assertIsNone(proc.poll(), self.format_driver_log(log_path))
            if any(event.get("type") == "compaction" for event in events):
                return
            self.receive_event(client, events, timeout=max(0.1, deadline - time.time()))
        raise AssertionError(f"timed out waiting for compaction event\n{self.format_driver_log(log_path)}")

    def wait_for_assistant_count(self, client: TestbedClient, events: list[dict], expected: int, proc: subprocess.Popen[bytes], log_path: Path, *, timeout: float = 10) -> None:
        deadline = time.time() + timeout
        while time.time() < deadline:
            self.assertIsNone(proc.poll(), self.format_driver_log(log_path))
            if self.assistant_turn_count(events) >= expected:
                return
            self.receive_event(client, events, timeout=max(0.1, deadline - time.time()))
        raise AssertionError(
            f"timed out waiting for assistant event count >= {expected}\n{self.format_driver_log(log_path)}"
        )

    def assistant_turn_count(self, events: list[dict]) -> int:
        turns = set()
        for event in events:
            if event.get("type") != "stream.delta" or not self.event_payload(event).get("text"):
                continue
            data = event.get("data") if isinstance(event.get("data"), dict) else {}
            turns.add((data.get("turn_id"), data.get("attempt_id")))
        return len(turns)

    def format_driver_log(self, log_path: Path) -> str:
        if not log_path.is_file():
            return "driver log missing"
        text = log_path.read_text(encoding="utf-8", errors="replace")
        return f"driver log:\n{text}"


def main() -> int:
    parser = argparse.ArgumentParser(description="Run compaction testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    CompactionSmoke.url = args.url
    CompactionSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(CompactionSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

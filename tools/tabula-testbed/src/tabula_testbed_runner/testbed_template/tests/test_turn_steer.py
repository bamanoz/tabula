#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import sys
import threading
import time
import types
import unittest


class FakeConnection:
    def __init__(self, url: str):
        self.url = url
        self.sent: list[dict] = []

    def send(self, msg: dict):
        self.sent.append(msg)

    def recv(self, timeout=None):
        return None

    def close(self):
        return None


class BaseSteerProvider:
    def set_turn_context(self, context: str):
        self.turn_contexts.append(context)

    def clear_turn_context(self):
        self.turn_contexts.append("")

    def repair_history(self):
        return []


class BlockingSteerProvider(BaseSteerProvider):
    def __init__(self):
        self.system_prompt = "prompt"
        self.provider = "test"
        self.model = "test-model"
        self.messages: list[dict] = []
        self.context_window = 200000
        self.last_input_tokens = 0
        self.started = threading.Event()
        self.aborted = threading.Event()
        self.generate_calls = 0
        self.record_aborted_calls = 0
        self.turn_contexts: list[str] = []

    def add_user_text(self, text: str):
        self.messages.append({"role": "user", "text": text})

    def add_tool_results(self, results):
        self.messages.append({"role": "tool", "results": results})

    def stream_generate(self, emit, on_text_delta, on_reasoning_delta=None):
        from tabula_drivers.providers import TurnOutcome

        self.generate_calls += 1
        if self.generate_calls == 1:
            emit(types.SimpleNamespace(kind="text", text="partial", meta={}, tool_call=None, usage=None))
            self.started.set()
            if not self.aborted.wait(2):
                raise RuntimeError("provider was not aborted for steer")
            raise RuntimeError("steered")
        emit(types.SimpleNamespace(kind="text", text="after steer", meta={}, tool_call=None, usage=None))
        return TurnOutcome(final_text="after steer", tool_calls=[], usage=None)

    def abort(self):
        self.aborted.set()

    def record_aborted_turn(self):
        self.record_aborted_calls += 1

    def restore_history(self, entries: list[dict]):
        return None

    def needs_compact(self) -> bool:
        return False

    def compact(self, logger=None, previous_summary: str = "") -> str:
        return ""


class TimeoutProvider(BaseSteerProvider):
    def __init__(self):
        self.system_prompt = "prompt"
        self.provider = "test"
        self.model = "test-model"
        self.messages: list[dict] = []
        self.context_window = 200000
        self.last_input_tokens = 0
        self.timeout_seconds = 0.01
        self.abort_calls = 0
        self.turn_contexts: list[str] = []

    def add_user_text(self, text: str):
        self.messages.append({"role": "user", "text": text})

    def add_tool_results(self, results):
        self.messages.append({"role": "tool", "results": results})

    def stream_generate(self, emit, on_text_delta, on_reasoning_delta=None):
        from tabula_drivers.providers import TurnOutcome

        time.sleep(2)
        return TurnOutcome(final_text="too late", tool_calls=[], usage=None)

    def abort(self):
        self.abort_calls += 1

    def record_aborted_turn(self):
        return None

    def restore_history(self, entries: list[dict]):
        return None

    def needs_compact(self) -> bool:
        return False

    def compact(self, logger=None, previous_summary: str = "") -> str:
        return ""


class UninterruptibleSteerProvider(TimeoutProvider):
    def __init__(self):
        super().__init__()
        self.timeout_seconds = 30
        self.started = threading.Event()
        self.generate_calls = 0

    def stream_generate(self, emit, on_text_delta, on_reasoning_delta=None):
        from tabula_drivers.providers import TurnOutcome

        self.generate_calls += 1
        if self.generate_calls == 1:
            emit(types.SimpleNamespace(kind="text", text="partial", meta={}, tool_call=None, usage=None))
            self.started.set()
            time.sleep(2)
            return TurnOutcome(final_text="too late", tool_calls=[], usage=None)
        emit(types.SimpleNamespace(kind="text", text="after steer", meta={}, tool_call=None, usage=None))
        return TurnOutcome(final_text="after steer", tool_calls=[], usage=None)


class LateEventAfterAbortProvider(TimeoutProvider):
    def __init__(self):
        super().__init__()
        self.generation_timeout_seconds = 0.0
        self.started = threading.Event()
        self.abort_called = threading.Event()

    def stream_generate(self, emit, on_text_delta, on_reasoning_delta=None):
        from tabula_drivers.providers import ProviderEvent, TurnOutcome

        self.started.set()
        if not self.abort_called.wait(2):
            raise RuntimeError("provider was not aborted")
        time.sleep(0.2)
        emit(ProviderEvent(kind="usage", usage={"input_tokens": 1, "output_tokens": 1}))
        return TurnOutcome(final_text="late", tool_calls=[], usage={"input_tokens": 1, "output_tokens": 1})

    def abort(self):
        self.abort_calls += 1
        self.abort_called.set()


class NoOutputSteerProvider(TimeoutProvider):
    def __init__(self):
        super().__init__()
        self.timeout_seconds = 30
        self.started = threading.Event()
        self.generate_calls = 0

    def stream_generate(self, emit, on_text_delta, on_reasoning_delta=None):
        from tabula_drivers.providers import TurnOutcome

        self.generate_calls += 1
        if self.generate_calls == 1:
            self.started.set()
            time.sleep(0.2)
            return TurnOutcome(final_text="too late", tool_calls=[], usage=None)
        emit(types.SimpleNamespace(kind="text", text="after steer", meta={}, tool_call=None, usage=None))
        return TurnOutcome(final_text="after steer", tool_calls=[], usage=None)


class TurnSteerInstalled(unittest.TestCase):
    tabula_home = ""

    def setUp(self):
        home = Path(self.tabula_home)
        sys.path.insert(0, str(home / "packages" / "python" / "src"))
        sys.path.insert(0, str(home / "plugins" / "sessions" / "sdk" / "python" / "src"))
        websocket_stub = types.ModuleType("websocket")
        websocket_stub.WebSocketTimeoutException = TimeoutError
        websocket_stub.WebSocketConnectionClosedException = ConnectionError
        websocket_stub.create_connection = lambda url: FakeConnection(url)
        sys.modules.setdefault("websocket", websocket_stub)

    def test_driver_steer_interrupts_active_generate_without_closing_kernel_turn(self):
        from tabula_drivers import driver_runtime

        provider = BlockingSteerProvider()
        old_connection = driver_runtime.KernelConnection
        driver_runtime.KernelConnection = FakeConnection
        self.addCleanup(lambda: setattr(driver_runtime, "KernelConnection", old_connection))
        runtime = driver_runtime.DriverRuntime(
            driver_runtime.DriverConfig(name="test", url="ws://127.0.0.1:1/ws", session="main"),
            provider_factory=lambda prompt, tools, turn_provider=None, turn_model=None, turn_effort=None: provider,
            logger=lambda _msg: None,
        )
        runtime.handle_init({"tools": []})
        runtime.handle_message({"type": "event", "topic": "message.user", "data": {"text": "start"}}, async_turn=True)
        self.assertTrue(provider.started.wait(2))

        runtime.handle_steer({"type": "event", "topic": "turn.steer", "id": "steer-1", "data": {"text": "redirect"}, "meta": {"source": "testbed"}})

        deadline = time.time() + 3
        while time.time() < deadline and not any(msg.get("type") == "event" and msg.get("topic") == "turn.done" for msg in runtime.conn.sent):
            time.sleep(0.01)

        self.assertEqual([msg["text"] for msg in provider.messages if msg.get("role") == "user"], ["start", "redirect"])
        self.assertEqual(provider.generate_calls, 2)
        self.assertEqual(provider.record_aborted_calls, 0)
        self.assertEqual(len([msg for msg in runtime.conn.sent if msg.get("type") == "event" and msg.get("topic") == "turn.done"]), 1)
        runtime.close()

    def test_driver_defers_steer_context_until_pending_tool_result_arrives(self):
        from tabula_drivers import driver_runtime

        provider = BlockingSteerProvider()
        old_connection = driver_runtime.KernelConnection
        driver_runtime.KernelConnection = FakeConnection
        self.addCleanup(lambda: setattr(driver_runtime, "KernelConnection", old_connection))
        runtime = driver_runtime.DriverRuntime(
            driver_runtime.DriverConfig(name="test", url="ws://127.0.0.1:1/ws", session="main"),
            provider_factory=lambda prompt, tools, turn_provider=None, turn_model=None, turn_effort=None: provider,
            logger=lambda _msg: None,
        )
        runtime.handle_init({"tools": []})
        history_entries: list[dict] = []
        runtime._write_history = history_entries.append
        provider.add_user_text("start")
        runtime._turn_in_progress = True
        runtime._active_turn_id = "turn-1"
        runtime._expected_tool_ids = ["tool-1"]
        runtime._expected_tool_names = {"tool-1": "subagent_spawn"}

        runtime.handle_steer({"type": "event", "topic": "turn.steer", "id": "steer-1", "data": {"text": "redirect"}})

        self.assertEqual([msg.get("text") for msg in provider.messages if msg.get("role") == "user"], ["start"])
        self.assertEqual(history_entries, [])
        runtime.handle_tool_result({"id": "tool-1", "output": "child done"}, async_turn=True)
        self.assertEqual([msg.get("role") for msg in provider.messages], ["user", "tool", "user"])
        self.assertEqual(provider.messages[-1]["role"], "user")
        self.assertTrue(provider.messages[-1]["text"].endswith("redirect"), provider.messages[-1])
        self.assertEqual(history_entries[-1]["text"], "redirect")
        self.assertTrue(history_entries[-1]["steer"])
        runtime.close()

    def test_installed_transcript_replay_preserves_steer_event(self):
        from tabula_session_sdk import load_session_transcript

        home = Path(self.tabula_home)
        session = "testbed-turn-steer"
        session_dir = home / "data" / "sessions" / session
        session_dir.mkdir(parents=True, exist_ok=True)
        (session_dir / "history.jsonl").write_text(
            json.dumps({"role": "user", "text": "redirect", "steer": True, "message_id": "steer-1", "ts": 1}) + "\n",
            encoding="utf-8",
        )

        replay = load_session_transcript(session, "default", tabula_home_override=home)

        self.assertEqual(replay, [{"type": "turn.steer", "text": "redirect", "ts": 1, "id": "steer-1"}])

    def test_driver_provider_timeout_finishes_kernel_turn(self):
        from tabula_drivers import driver_runtime

        provider = TimeoutProvider()
        old_connection = driver_runtime.KernelConnection
        driver_runtime.KernelConnection = FakeConnection
        self.addCleanup(lambda: setattr(driver_runtime, "KernelConnection", old_connection))
        runtime = driver_runtime.DriverRuntime(
            driver_runtime.DriverConfig(name="test", url="ws://127.0.0.1:1/ws", session="main"),
            provider_factory=lambda prompt, tools, turn_provider=None, turn_model=None, turn_effort=None: provider,
            logger=lambda _msg: None,
        )
        runtime.handle_init({"tools": []})
        runtime.handle_message({"type": "event", "topic": "message.user", "data": {"text": "start"}})

        done = [msg for msg in runtime.conn.sent if msg.get("type") == "event" and msg.get("topic") == "turn.done"]
        self.assertTrue(done)
        runtime.close()

    def test_driver_cancel_pending_async_turn_finishes_kernel_turn(self):
        from tabula_drivers import driver_runtime

        old_connection = driver_runtime.KernelConnection
        driver_runtime.KernelConnection = FakeConnection
        self.addCleanup(lambda: setattr(driver_runtime, "KernelConnection", old_connection))
        runtime = driver_runtime.DriverRuntime(
            driver_runtime.DriverConfig(name="test", url="ws://127.0.0.1:1/ws", session="main"),
            provider_factory=lambda prompt, tools, turn_provider=None, turn_model=None, turn_effort=None: BlockingSteerProvider(),
            logger=lambda _msg: None,
        )
        runtime.handle_init({"tools": []})

        with runtime._state_lock:
            runtime._turn_start_pending = True
        runtime.abort()
        runtime.process_turn()

        done = [msg for msg in runtime.conn.sent if msg.get("type") == "event" and msg.get("topic") == "turn.done"]
        statuses = [msg.get("data", {}).get("state") for msg in runtime.conn.sent if msg.get("type") == "event" and msg.get("topic") == "session.status"]
        self.assertEqual(len(done), 1, runtime.conn.sent)
        self.assertIn("idle", statuses)
        runtime.close()

    def test_driver_cancel_active_provider_turn_suppresses_late_events(self):
        from tabula_drivers import driver_runtime

        provider = LateEventAfterAbortProvider()
        old_connection = driver_runtime.KernelConnection
        driver_runtime.KernelConnection = FakeConnection
        self.addCleanup(lambda: setattr(driver_runtime, "KernelConnection", old_connection))
        runtime = driver_runtime.DriverRuntime(
            driver_runtime.DriverConfig(name="test", url="ws://127.0.0.1:1/ws", session="main"),
            provider_factory=lambda prompt, tools, turn_provider=None, turn_model=None, turn_effort=None: provider,
            logger=lambda _msg: None,
        )
        runtime.handle_init({"tools": []})
        runtime.handle_message({"type": "event", "topic": "message.user", "data": {"text": "start"}}, async_turn=True)
        self.assertTrue(provider.started.wait(2))

        runtime.abort()
        time.sleep(0.5)

        done = [msg for msg in runtime.conn.sent if msg.get("type") == "event" and msg.get("topic") == "turn.done"]
        usage = [msg for msg in runtime.conn.sent if msg.get("type") == "event" and msg.get("topic") == "usage.update"]
        self.assertEqual(len(done), 1, runtime.conn.sent)
        self.assertEqual(usage, [], runtime.conn.sent)
        self.assertGreaterEqual(provider.abort_calls, 1)
        runtime.close()

    def test_driver_steer_interrupts_provider_wait_when_abort_does_not_unblock_socket(self):
        from tabula_drivers import driver_runtime

        provider = UninterruptibleSteerProvider()
        old_connection = driver_runtime.KernelConnection
        driver_runtime.KernelConnection = FakeConnection
        self.addCleanup(lambda: setattr(driver_runtime, "KernelConnection", old_connection))
        runtime = driver_runtime.DriverRuntime(
            driver_runtime.DriverConfig(name="test", url="ws://127.0.0.1:1/ws", session="main"),
            provider_factory=lambda prompt, tools, turn_provider=None, turn_model=None, turn_effort=None: provider,
            logger=lambda _msg: None,
        )
        runtime.handle_init({"tools": []})
        runtime.handle_message({"type": "event", "topic": "message.user", "data": {"text": "start"}}, async_turn=True)
        self.assertTrue(provider.started.wait(2))

        runtime.handle_steer({"type": "event", "topic": "turn.steer", "id": "steer-1", "data": {"text": "redirect"}, "meta": {"source": "testbed"}})
        deadline = time.time() + 1
        while time.time() < deadline and not any(msg.get("type") == "event" and msg.get("topic") == "turn.done" for msg in runtime.conn.sent):
            time.sleep(0.01)

        self.assertEqual(provider.generate_calls, 2)
        self.assertTrue(any(msg.get("type") == "event" and msg.get("topic") == "turn.done" for msg in runtime.conn.sent))
        runtime.close()

    def test_driver_early_steer_replaces_current_user_before_provider_output(self):
        from tabula_drivers import driver_runtime

        provider = NoOutputSteerProvider()
        old_connection = driver_runtime.KernelConnection
        driver_runtime.KernelConnection = FakeConnection
        self.addCleanup(lambda: setattr(driver_runtime, "KernelConnection", old_connection))
        runtime = driver_runtime.DriverRuntime(
            driver_runtime.DriverConfig(name="test", url="ws://127.0.0.1:1/ws", session="main"),
            provider_factory=lambda prompt, tools, turn_provider=None, turn_model=None, turn_effort=None: provider,
            logger=lambda _msg: None,
        )
        runtime.handle_init({"tools": []})
        runtime.handle_message({"type": "event", "topic": "message.user", "data": {"text": "original"}}, async_turn=True)
        runtime.handle_steer({"type": "event", "topic": "turn.steer", "id": "steer-1", "data": {"text": "replacement"}, "meta": {"source": "testbed"}})

        deadline = time.time() + 1
        while time.time() < deadline and not any(msg.get("type") == "event" and msg.get("topic") == "turn.done" for msg in runtime.conn.sent):
            time.sleep(0.01)

        self.assertEqual([msg.get("text") for msg in provider.messages if msg.get("role") == "user"], ["replacement"])
        runtime.close()


def main() -> int:
    parser = argparse.ArgumentParser(description="Run turn steer testbed tests")
    parser.add_argument("--url", default="")
    parser.add_argument("--observer-url", default="")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    TurnSteerInstalled.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(TurnSteerInstalled))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

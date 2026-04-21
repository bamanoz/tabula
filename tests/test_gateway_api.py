#!/usr/bin/env python3
"""Unit tests for the gateway-api skill."""

from __future__ import annotations

import json
import importlib.util
import os
import sys
import tempfile
import threading
import types
import unittest
from pathlib import Path
from unittest.mock import MagicMock, patch


ROOT = Path(__file__).resolve().parents[1]
GATEWAY_API_PATH = ROOT / "distrib" / "familiar" / "skills" / "gateway-api" / "run.py"

if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

if "websocket" not in sys.modules:
    fake_websocket = types.ModuleType("websocket")

    class _FakeTimeout(Exception):
        pass

    class _FakeClosed(Exception):
        pass

    def _unexpected_create_connection(_url: str):
        raise RuntimeError("websocket.create_connection should be patched in tests")

    fake_websocket.create_connection = _unexpected_create_connection
    fake_websocket.WebSocketTimeoutException = _FakeTimeout
    fake_websocket.WebSocketConnectionClosedException = _FakeClosed
    sys.modules["websocket"] = fake_websocket


def _load_gateway_api_module():
    spec = importlib.util.spec_from_file_location("gateway_api_run", GATEWAY_API_PATH)
    mod = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(mod)
    return mod


def _load_gateway_api_module_for_home(home: str, extra_env: dict[str, str] | None = None):
    env = {"TABULA_HOME": home}
    if extra_env:
        env.update(extra_env)
    with patch.dict(os.environ, env, clear=True):
        return _load_gateway_api_module()


def _write_global_config(home: Path, *, provider: str | None = None, extra: str = ""):
    cfg_dir = home / "config"
    cfg_dir.mkdir(parents=True, exist_ok=True)
    parts = []
    if provider:
        parts.append(f'provider = "{provider}"')
    if extra:
        parts.append(extra.strip())
    if parts:
        (cfg_dir / "global.toml").write_text("\n\n".join(parts) + "\n", encoding='utf-8')


def _write_fake_driver(home: Path, provider: str, *, ready: bool = True):
    driver_dir = home / "skills" / f"driver-{provider}"
    driver_dir.mkdir(parents=True, exist_ok=True)
    (driver_dir / "run.py").write_text("#!/usr/bin/env python3\n")

    if provider == "openai":
        alias = "OPENAI_API_KEY"
        default_base = "https://api.openai.com/v1"
        default_model = "gpt-5.4"
    else:
        alias = "ANTHROPIC_API_KEY"
        default_base = "https://api.anthropic.com"
        default_model = "claude-sonnet-4-6"

    (driver_dir / "SKILL.config.json").write_text(
        json.dumps(
            {
                "id": f"driver-{provider}",
                "config": {
                    "entries": [
                        {
                            "key": "api_key",
                            "type": "string",
                            "secret": True,
                            "required": True,
                            "env": f"TABULA_SKILL_DRIVER_{provider.upper()}_API_KEY",
                            "env_aliases": [alias],
                            "store_id": f"driver-{provider}.api_key",
                        },
                        {
                            "key": "base_url",
                            "type": "string",
                            "default": default_base,
                            "env": f"TABULA_SKILL_DRIVER_{provider.upper()}_BASE_URL",
                        },
                        {
                            "key": "model",
                            "type": "string",
                            "default": default_model,
                            "env": f"TABULA_SKILL_DRIVER_{provider.upper()}_MODEL",
                        },
                    ]
                },
            }
        ),
        encoding='utf-8',
    )

    if ready:
        (home / "secrets.json").write_text(json.dumps({f"driver-{provider}.api_key": f"sk-{provider}"}) + "\n", encoding='utf-8')


class DummyThread:
    def __init__(self, target=None, daemon=None):
        self.target = target
        self.daemon = daemon
        self.started = False

    def start(self):
        self.started = True


class FakeKernelConnection:
    def __init__(self, url: str):
        self.url = url
        self.sent: list[dict] = []
        self.recv_messages = [
            {"type": "connected"},
            {"type": "joined"},
            {"type": "tool_result", "id": "spawn-driver", "output": "PID 321"},
            {"type": "member_joined"},
        ]

    def send(self, msg: dict):
        self.sent.append(msg)

    def recv(self, timeout=None):
        if not self.recv_messages:
            return None
        return self.recv_messages.pop(0)

    def close(self):
        pass


class FakeThreadingHTTPServer:
    instances: list["FakeThreadingHTTPServer"] = []

    def __init__(self, server_address, handler_cls):
        self.server_address = server_address
        self.handler_cls = handler_cls
        self.daemon_threads = False
        self.closed = False
        FakeThreadingHTTPServer.instances.append(self)

    def serve_forever(self):
        return

    def server_close(self):
        self.closed = True


class TestGatewayAPIPaths(unittest.TestCase):
    def test_gateway_uses_absolute_driver_command(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_fake_driver(home, "openai")
            driver = home / "skills" / "driver-openai" / "run.py"
            _write_global_config(home, provider="openai")

            with patch.dict(os.environ, {"TABULA_HOME": str(home), "TABULA_PROVIDER": "openai"}, clear=False):
                mod = _load_gateway_api_module()
                gateway = mod.GatewayAPI()

            self.assertIn(str(driver), gateway.driver_cmd)
            self.assertNotIn("distrib/familiar/skills/driver-openai/run.py", gateway.driver_cmd.replace(str(driver), ""))

    def test_missing_driver_script_raises_clear_error(self):
        with tempfile.TemporaryDirectory() as tmp:
            with patch.dict(os.environ, {"TABULA_HOME": tmp, "TABULA_PROVIDER": "openai"}, clear=False):
                with self.assertRaisesRegex(Exception, "driver script not found"):
                    _load_gateway_api_module()


class TestGatewayAPIConfigImport(unittest.TestCase):
    def test_module_import_reads_skill_config_defaults(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_fake_driver(home, "openai")
            _write_global_config(home, provider="openai", extra=
                '\n'.join([
                    '[gateway.api]',
                    'auth_token = { source = "store", id = "gateway-api.auth_token" }',
                    '',
                    '[gateway.api.session]',
                    'idle_ttl = 111',
                    'max_age = 222',
                    'cleanup_interval = 7',
                    '',
                ]))
            (home / "secrets.json").write_text('{"gateway-api.auth_token":"secret-token"}\n', encoding='utf-8')

            mod = _load_gateway_api_module_for_home(str(home))

            self.assertEqual(mod.PROVIDER_OVERRIDE, None)
            self.assertEqual(mod.ACTIVE_PROVIDER, "openai")
            self.assertEqual(mod.AUTH_TOKEN, "secret-token")
            self.assertEqual(mod.SESSION_IDLE_TTL, 111.0)
            self.assertEqual(mod.SESSION_MAX_AGE, 222.0)
            self.assertEqual(mod.SESSION_CLEANUP_INTERVAL, 7.0)

    def test_module_import_env_overrides_skill_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_fake_driver(home, "openai")
            _write_global_config(home, provider="anthropic", extra=
                '\n'.join([
                    '[gateway.api]',
                    'provider_override = "openai"',
                    'auth_token = { source = "store", id = "gateway-api.auth_token" }',
                    '',
                    '[gateway.api.session]',
                    'idle_ttl = 111',
                    'max_age = 222',
                    'cleanup_interval = 7',
                    '',
                ]))
            (home / "secrets.json").write_text('{"gateway-api.auth_token":"secret-token"}\n', encoding='utf-8')

            mod = _load_gateway_api_module_for_home(
                str(home),
                {
                    "TABULA_SKILL_GATEWAY_API_AUTH_TOKEN": "env-token",
                    "TABULA_SKILL_GATEWAY_API_SESSION_IDLE_TTL": "15",
                },
            )

            self.assertEqual(mod.PROVIDER_OVERRIDE, "openai")
            self.assertEqual(mod.ACTIVE_PROVIDER, "openai")
            self.assertEqual(mod.AUTH_TOKEN, "env-token")
            self.assertEqual(mod.SESSION_IDLE_TTL, 15.0)


class TestSessionStateConnect(unittest.TestCase):
    def test_connect_declares_runtime_message_contract(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_fake_driver(home, "anthropic")
            driver = home / "skills" / "driver-anthropic" / "run.py"
            _write_global_config(home, provider="anthropic")

            with patch.dict(os.environ, {"TABULA_HOME": str(home), "TABULA_PROVIDER": "anthropic"}, clear=False):
                mod = _load_gateway_api_module()

            fake_conn = FakeKernelConnection("ws://example.test/ws")
            with patch.object(mod, "KernelConnection", return_value=fake_conn), patch.object(mod.threading, "Thread", DummyThread):
                state = mod.SessionState("sess-1234")
                state.connect(mod._driver_command("anthropic"))

            connect_msg = fake_conn.sent[0]
            self.assertEqual(connect_msg["type"], mod.MSG_CONNECT)
            self.assertIn(mod.MSG_CANCEL, connect_msg["sends"])
            self.assertIn(mod.MSG_STREAM_START, connect_msg["receives"])
            self.assertIn(mod.MSG_STREAM_DELTA, connect_msg["receives"])
            self.assertIn(mod.MSG_STREAM_END, connect_msg["receives"])
            self.assertIn(mod.MSG_DONE, connect_msg["receives"])
            self.assertIn(mod.MSG_ERROR, connect_msg["receives"])

            spawn_msg = fake_conn.sent[2]
            self.assertEqual(spawn_msg["name"], mod.TOOL_PROCESS_SPAWN)
            self.assertIn(str(driver), spawn_msg["input"]["command"])


class TestGatewayAPIConcurrency(unittest.TestCase):
    def _load_with_driver(self, provider: str = "anthropic"):
        tmp = tempfile.TemporaryDirectory()
        home = Path(tmp.name)
        _write_fake_driver(home, provider)
        _write_global_config(home, provider=provider)

        with patch.dict(os.environ, {"TABULA_HOME": str(home)}, clear=False):
            mod = _load_gateway_api_module()
        self.addCleanup(tmp.cleanup)
        return mod

    def test_main_uses_threading_http_server(self):
        mod = self._load_with_driver()
        FakeThreadingHTTPServer.instances = []

        with patch.object(mod, "ThreadingHTTPServer", FakeThreadingHTTPServer), patch.object(
            mod.sys, "argv", ["run.py", "--port", "0"]
        ):
            mod.main()

        self.assertEqual(len(FakeThreadingHTTPServer.instances), 1)
        server = FakeThreadingHTTPServer.instances[0]
        self.assertTrue(server.daemon_threads)
        self.assertTrue(server.closed)

    def test_different_sessions_connect_without_holding_global_lock(self):
        mod = self._load_with_driver()
        gateway = mod.GatewayAPI()
        self.addCleanup(gateway.shutdown)
        barrier = threading.Barrier(2)
        errors: list[BaseException] = []

        class SlowSession:
            def __init__(self, session_id: str):
                self.session_id = session_id
                self.alive = True

            def connect(self, driver_cmd: str):
                try:
                    barrier.wait(timeout=1)
                except BaseException as exc:
                    errors.append(exc)
                    raise

            def close(self, reason="closed"):
                self.alive = False

        results = []
        with patch.object(mod, "SessionState", SlowSession):
            threads = [
                threading.Thread(target=lambda sid=sid: results.append(gateway.get_or_create_session(sid)))
                for sid in ("sess-a", "sess-b")
            ]
            for thread in threads:
                thread.start()
            for thread in threads:
                thread.join(timeout=2)

        self.assertFalse(any(thread.is_alive() for thread in threads))
        self.assertEqual(errors, [])
        self.assertEqual({state.session_id for state in results}, {"sess-a", "sess-b"})

    def test_same_session_creation_is_single_flight(self):
        mod = self._load_with_driver()
        gateway = mod.GatewayAPI()
        self.addCleanup(gateway.shutdown)
        connect_started = threading.Event()
        release_connect = threading.Event()
        connect_calls = 0
        connect_lock = threading.Lock()

        class SlowSession:
            def __init__(self, session_id: str):
                self.session_id = session_id
                self.alive = True

            def connect(self, driver_cmd: str):
                nonlocal connect_calls
                with connect_lock:
                    connect_calls += 1
                connect_started.set()
                release_connect.wait(timeout=2)

            def touch(self, now=None):
                return None

            def expiry_reason(self, now=None):
                return None

            def is_busy(self):
                return False

            def close(self, reason="closed"):
                self.alive = False

        results = []
        with patch.object(mod, "SessionState", SlowSession):
            first = threading.Thread(target=lambda: results.append(gateway.get_or_create_session("same")))
            second = threading.Thread(target=lambda: results.append(gateway.get_or_create_session("same")))
            first.start()
            self.assertTrue(connect_started.wait(timeout=1))
            second.start()
            release_connect.set()
            first.join(timeout=2)
            second.join(timeout=2)

        self.assertFalse(first.is_alive())
        self.assertFalse(second.is_alive())
        self.assertEqual(connect_calls, 1)
        self.assertEqual(len(results), 2)
        self.assertIs(results[0], results[1])

    def test_session_turn_drains_stale_events_and_serializes_send(self):
        mod = self._load_with_driver()
        fake_conn = FakeKernelConnection("ws://example.test/ws")

        with patch.object(mod, "KernelConnection", return_value=fake_conn):
            state = mod.SessionState("sess-turn")

        state.events.put((mod.MSG_STREAM_DELTA, "stale"))
        with state.turn("hello"):
            self.assertTrue(state.turn_lock.locked())
            self.assertTrue(state.events.empty())
            self.assertEqual(fake_conn.sent[-1], {"type": mod.MSG_MESSAGE, "text": "hello"})


class TestGatewayAPISessionLifecycle(unittest.TestCase):
    def _load_with_driver(self, provider: str = "anthropic"):
        tmp = tempfile.TemporaryDirectory()
        home = Path(tmp.name)
        _write_fake_driver(home, provider)
        _write_global_config(home, provider=provider)

        with patch.dict(os.environ, {"TABULA_HOME": str(home)}, clear=False):
            mod = _load_gateway_api_module()
        self.addCleanup(tmp.cleanup)
        return mod

    def test_cancel_turn_requires_matching_inflight_turn(self):
        mod = self._load_with_driver()
        fake_conn = FakeKernelConnection("ws://example.test/ws")

        with patch.object(mod, "KernelConnection", return_value=fake_conn):
            state = mod.SessionState("sess-cancel")

        state.alive = True
        state.inflight_turn_id = "turn-123"

        self.assertFalse(state.cancel_turn("turn-mismatch"))
        self.assertEqual(fake_conn.sent, [])
        self.assertTrue(state.cancel_turn("turn-123"))
        self.assertTrue(state.cancel_requested)
        self.assertEqual(fake_conn.sent[-1], {"type": mod.MSG_CANCEL})

    def test_cleanup_evicts_idle_session_and_closes_driver(self):
        mod = self._load_with_driver()
        gateway = mod.GatewayAPI(cleanup_interval=0)

        class FakeSession:
            def __init__(self, session_id: str):
                self.session_id = session_id
                self.alive = True
                self.closed_reasons = []

            def expiry_reason(self, now=None):
                return "idle"

            def is_busy(self):
                return False

            def close(self, reason="closed"):
                self.closed_reasons.append(reason)
                self.alive = False

        session = FakeSession("sess-1")
        gateway.sessions["sess-1"] = session

        gateway.cleanup_sessions()

        self.assertNotIn("sess-1", gateway.sessions)
        self.assertEqual(session.closed_reasons, ["idle"])
        gateway.shutdown()

    def test_cleanup_skips_busy_expired_session(self):
        mod = self._load_with_driver()
        gateway = mod.GatewayAPI(cleanup_interval=0)

        class FakeSession:
            def __init__(self, session_id: str):
                self.session_id = session_id
                self.alive = True
                self.closed_reasons = []

            def expiry_reason(self, now=None):
                return "ttl"

            def is_busy(self):
                return True

            def close(self, reason="closed"):
                self.closed_reasons.append(reason)
                self.alive = False

        session = FakeSession("sess-busy")
        gateway.sessions["sess-busy"] = session

        gateway.cleanup_sessions()

        self.assertIs(gateway.sessions["sess-busy"], session)
        self.assertEqual(session.closed_reasons, [])
        gateway.shutdown()

    def test_get_or_create_session_replaces_expired_idle_session(self):
        mod = self._load_with_driver()
        gateway = mod.GatewayAPI(cleanup_interval=0)

        class ExpiredSession:
            def __init__(self, session_id: str):
                self.session_id = session_id
                self.alive = True
                self.closed_reasons = []

            def expiry_reason(self, now=None):
                return "ttl"

            def is_busy(self):
                return False

            def touch(self, now=None):
                return None

            def close(self, reason="closed"):
                self.closed_reasons.append(reason)
                self.alive = False

        replacement = MagicMock()
        replacement.session_id = "sess-9"
        replacement.alive = True
        replacement.expiry_reason.return_value = None
        replacement.is_busy.return_value = False

        existing = ExpiredSession("sess-9")
        gateway.sessions["sess-9"] = existing

        with patch.object(mod, "SessionState", return_value=replacement) as mock_state_cls:
            result = gateway.get_or_create_session("sess-9")

        self.assertIs(result, replacement)
        self.assertEqual(existing.closed_reasons, ["replaced"])
        mock_state_cls.assert_called_once_with("sess-9")
        replacement.connect.assert_called_once_with(gateway.driver_cmd)
        gateway.shutdown()

    def test_shutdown_closes_all_sessions_and_unblocks_creators(self):
        mod = self._load_with_driver()
        gateway = mod.GatewayAPI(cleanup_interval=0)

        class FakeSession:
            def __init__(self, session_id: str):
                self.session_id = session_id
                self.closed_reasons = []

            def close(self, reason="closed"):
                self.closed_reasons.append(reason)

        first = FakeSession("sess-1")
        second = FakeSession("sess-2")
        waiter = threading.Event()
        gateway.sessions = {"sess-1": first, "sess-2": second}
        gateway._creating = {"sess-3": waiter}

        gateway.shutdown()

        self.assertEqual(first.closed_reasons, ["shutdown"])
        self.assertEqual(second.closed_reasons, ["shutdown"])
        self.assertTrue(waiter.is_set())
        self.assertEqual(gateway.sessions, {})
        self.assertEqual(gateway._creating, {})
        self.assertTrue(gateway._stop_event.is_set())


if __name__ == "__main__":
    unittest.main()

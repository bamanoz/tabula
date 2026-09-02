#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import time
from types import SimpleNamespace
import unittest

from tabula_testbed import TestbedClient


FAKE_OPENAI = '''import json
import os
from pathlib import Path
from types import SimpleNamespace


class _Stream:
    def __iter__(self):
        delta = SimpleNamespace(content="managed-driver-ok", tool_calls=[])
        return iter([SimpleNamespace(choices=[SimpleNamespace(delta=delta)], usage=None)])

    def close(self):
        return None


class _Completions:
    def create(self, **kwargs):
        messages = kwargs.get("messages", []) or []
        prompt = next((str(message.get("content") or "") for message in messages if message.get("role") == "system"), "")
        marker = Path(os.environ["TABULA_HOME"]) / "data" / "testbed" / "managed-driver-prompt.json"
        marker.parent.mkdir(parents=True, exist_ok=True)
        marker.write_text(json.dumps({"prompt": prompt}), encoding="utf-8")
        return _Stream()


class _Chat:
    def __init__(self):
        self.completions = _Completions()


class OpenAI:
    def __init__(self, api_key=None, base_url=None):
        self.chat = _Chat()
'''


class ManagedDriverSessionInstalled(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def test_runtime_owned_driver_autostarts_and_receives_skills_prompt(self):
        home = Path(self.tabula_home)
        self.configure_installed_driver(home)

        session = "testbed-managed-driver-session"
        client = TestbedClient(
            self.url,
            name="testbed-managed-ui",
            meta={"tabula.client_role": "user", "tabula.managed": True},
        )
        try:
            client.connect()
            created = client.create_session(
                session,
                driver_component_id="driver",
                agent_spec_revision="testbed:managed-driver-v4",
            )
            self.assertEqual(created.get("kind"), "result", created)
            self.assertEqual(created.get("op"), "session.create", created)

            accepted = client.submit_input(
                f"input-{session}",
                {"text": "test managed driver startup"},
                timeout=20,
            )
            self.assertEqual(accepted.get("op"), "input.accepted", accepted)

            delta = client.recv(op="stream.delta", timeout=20)
            committed = delta.get("data") if isinstance(delta.get("data"), dict) else {}
            data = committed.get("data") if isinstance(committed.get("data"), dict) else {}
            payload = data.get("payload") if isinstance(data.get("payload"), dict) else {}
            self.assertTrue(committed.get("event_id"), committed)
            self.assertTrue(committed.get("cursor"), committed)
            self.assertEqual(payload.get("text"), "managed-driver-ok")

            prompt = self.wait_for_prompt(home)
            self.assertIn("## Agent Skills", prompt)
            self.assertIn("tabula-guide", prompt)
        finally:
            client.close()

    def configure_installed_driver(self, home: Path) -> None:
        driver_dir = home / "plugins" / "driver"
        self.assertTrue(driver_dir.is_dir(), "installed driver plugin missing")
        (driver_dir / "openai.py").write_text(FAKE_OPENAI, encoding="utf-8")

        config = home / "config" / "plugins" / "driver" / "config.toml"
        config.parent.mkdir(parents=True, exist_ok=True)
        config.write_text(
            """
provider = "openai"

[providers.openai]
type = "openai"
api_key = "test-key"
base_url = "https://api.openai.invalid/v1"
default_model = "test-model"
api = "chat_completions"
""".strip()
            + "\n",
            encoding="utf-8",
        )

    def wait_for_prompt(self, home: Path) -> str:
        marker = home / "data" / "testbed" / "managed-driver-prompt.json"
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            if marker.is_file():
                data = json.loads(marker.read_text(encoding="utf-8"))
                return str(data.get("prompt") or "")
            time.sleep(0.05)
        self.fail("timed out waiting for managed driver provider prompt")


def main() -> int:
    parser = argparse.ArgumentParser(description="Run managed driver session testbed tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    ManagedDriverSessionInstalled.url = args.url
    ManagedDriverSessionInstalled.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(
        unittest.defaultTestLoader.loadTestsFromTestCase(ManagedDriverSessionInstalled)
    )
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

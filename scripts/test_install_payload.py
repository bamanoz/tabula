#!/usr/bin/env python3
from __future__ import annotations

import importlib.util
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
MODULE_PATH = ROOT / "scripts" / "install_payload.py"
SPEC = importlib.util.spec_from_file_location("install_payload", MODULE_PATH)
assert SPEC and SPEC.loader
INSTALL_PAYLOAD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(INSTALL_PAYLOAD)


def _write(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def _snapshot(root: Path) -> dict[str, bytes]:
    return {
        path.relative_to(root).as_posix(): path.read_bytes()
        for path in root.rglob("*")
        if path.is_file()
    }


class InstallPayloadTests(unittest.TestCase):
    def test_existing_config_tree_is_preserved(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            payload = root / "payload"
            home = root / "home"
            for relative in (
                "global.toml",
                "kernel.toml",
                "runtime.toml",
                "plugins/gateway-web/config.toml",
            ):
                _write(payload / "config" / relative, f"payload:{relative}\n")
                _write(home / "config" / relative, f"user:{relative}\n")
            _write(home / "config" / "plugins" / "custom" / "config.toml", "keep = true\n")
            _write(payload / "bin" / "tabula-agent", "new launcher\n")
            before = _snapshot(home / "config")

            INSTALL_PAYLOAD.overlay_payload(payload, home)

            self.assertEqual(_snapshot(home / "config"), before)
            self.assertEqual((home / "bin" / "tabula-agent").read_text(encoding="utf-8"), "new launcher\n")

    def test_missing_config_tree_is_seeded(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            payload = root / "payload"
            home = root / "home"
            _write(payload / "config" / "global.toml.example", "[workspace]\n")

            INSTALL_PAYLOAD.overlay_payload(payload, home)

            self.assertEqual((home / "config" / "global.toml.example").read_text(encoding="utf-8"), "[workspace]\n")


class AgentGatewayConfigTests(unittest.TestCase):
    def test_make_target_does_not_replace_existing_gateway_config(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            gateway = home / "config" / "plugins" / "gateway-web" / "config.toml"
            _write(gateway, 'host = "100.64.0.1"\nallowed_origins = ["http://100.64.0.1:8865"]\n')
            _write(home / "config" / "kernel.toml", "[kernel]\ncustom = true\n")
            before = _snapshot(home / "config")

            subprocess.run(
                [
                    "make",
                    "agent-write-gateway-config",
                    f"AGENT_HOME={home.as_posix()}",
                    "AGENT_KERNEL_URL=ws://127.0.0.1:8189/ws",
                    "AGENT_GATEWAY_WEB_PORT=8865",
                ],
                cwd=ROOT,
                check=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )

            self.assertEqual(_snapshot(home / "config"), before)

    def test_make_target_seeds_missing_gateway_config(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp) / "home"
            subprocess.run(
                [
                    "make",
                    "agent-write-gateway-config",
                    f"AGENT_HOME={home.as_posix()}",
                    "AGENT_KERNEL_URL=ws://127.0.0.1:8189/ws",
                    "AGENT_GATEWAY_WEB_PORT=8865",
                ],
                cwd=ROOT,
                check=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )

            config = (home / "config" / "plugins" / "gateway-web" / "config.toml").read_text(encoding="utf-8")
            self.assertIn('host = "127.0.0.1"', config)
            self.assertIn("port = 8865", config)


if __name__ == "__main__":
    unittest.main()

#!/usr/bin/env python3
"""Tests for skill config loading without .env."""

from __future__ import annotations

import importlib.util
import importlib
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from skills.lib.config import SkillConfigError, load_skill_config


DRIVER_OPENAI_DIR = ROOT / "distrib" / "assistant" / "skills" / "driver-openai"
DRIVER_OPENAI_PATH = DRIVER_OPENAI_DIR / "run.py"
DRIVER_MOCK_DIR = ROOT / "testing" / "skills" / "driver-mock"
SUBAGENT_OPENAI_DIR = ROOT / "distrib" / "assistant" / "skills" / "subagent-openai"
SUBAGENT_OPENAI_PATH = SUBAGENT_OPENAI_DIR / "run.py"
DRIVER_ANTHROPIC_DIR = ROOT / "distrib" / "assistant" / "skills" / "driver-anthropic"
DRIVER_ANTHROPIC_PATH = DRIVER_ANTHROPIC_DIR / "run.py"
SUBAGENT_ANTHROPIC_DIR = ROOT / "distrib" / "assistant" / "skills" / "subagent-anthropic"
SUBAGENT_ANTHROPIC_PATH = SUBAGENT_ANTHROPIC_DIR / "run.py"
HOOK_LOGGER_DIR = ROOT / "distrib" / "assistant" / "skills" / "hook-logger"
HOOK_LOGGER_PATH = HOOK_LOGGER_DIR / "run.py"
MEMORY_DIR = ROOT / "distrib" / "assistant" / "skills" / "memory"
MCP_DIR = ROOT / "distrib" / "assistant" / "skills" / "mcp"
MCP_DAEMON_PATH = MCP_DIR / "daemon.py"


def _load_driver_openai_module():
    spec = importlib.util.spec_from_file_location("tabula_driver_openai_run", DRIVER_OPENAI_PATH)
    mod = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(mod)
    return mod


def _load_subagent_openai_module():
    spec = importlib.util.spec_from_file_location("tabula_subagent_openai_run", SUBAGENT_OPENAI_PATH)
    mod = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(mod)
    return mod


def _load_driver_anthropic_module():
    spec = importlib.util.spec_from_file_location("tabula_driver_anthropic_run", DRIVER_ANTHROPIC_PATH)
    mod = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(mod)
    return mod


def _load_subagent_anthropic_module():
    spec = importlib.util.spec_from_file_location("tabula_subagent_anthropic_run", SUBAGENT_ANTHROPIC_PATH)
    mod = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(mod)
    return mod


def _load_hook_logger_module():
    spec = importlib.util.spec_from_file_location("tabula_hook_logger_run", HOOK_LOGGER_PATH)
    mod = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(mod)
    return mod


def _load_mcp_daemon_module():
    spec = importlib.util.spec_from_file_location(
        "mcp.daemon",
        MCP_DAEMON_PATH,
        submodule_search_locations=[str(MCP_DIR)],
    )
    mod = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    sys.modules["mcp.daemon"] = mod
    spec.loader.exec_module(mod)
    return mod


def _write_global_toml(home: Path, text: str):
    cfg_dir = home / "config"
    cfg_dir.mkdir(parents=True, exist_ok=True)
    (cfg_dir / "global.toml").write_text(text, encoding="utf-8")


class TestSkillConfig(unittest.TestCase):
    def test_loads_driver_openai_from_skill_config_and_secret_store(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_global_toml(home, '\n'.join([
                '[openai]',
                'model = "gpt-5.4"',
                'base_url = "https://proxy.example/v1"',
                'api_key = { source = "store", id = "driver-openai.api_key" }',
                '',
            ]))
            (home / "secrets.json").write_text(
                '{"driver-openai.api_key":"sk-store-value"}\n',
                encoding="utf-8",
            )

            with patch.dict(os.environ, {"TABULA_HOME": str(home)}, clear=True):
                cfg = load_skill_config(DRIVER_OPENAI_DIR)

            self.assertEqual(cfg["api_key"], "sk-store-value")
            self.assertEqual(cfg["base_url"], "https://proxy.example/v1")
            self.assertEqual(cfg["model"], "gpt-5.4")

    def test_driver_openai_prefers_global_toml_for_core_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            (home / "config").mkdir(parents=True)
            (home / "config" / "global.toml").write_text(
                '\n'.join([
                    '[openai]',
                    'model = "global-model"',
                    'base_url = "https://global.example/v1"',
                    'api_key = { source = "store", id = "driver-openai.api_key" }',
                    '',
                ]),
                encoding="utf-8",
            )
            (home / "secrets.json").write_text(
                '{"driver-openai.api_key":"sk-global"}\n',
                encoding="utf-8",
            )

            with patch.dict(os.environ, {"TABULA_HOME": str(home)}, clear=True):
                cfg = load_skill_config(DRIVER_OPENAI_DIR)

            self.assertEqual(cfg["api_key"], "sk-global")
            self.assertEqual(cfg["base_url"], "https://global.example/v1")
            self.assertEqual(cfg["model"], "global-model")

    def test_driver_openai_ignores_legacy_skill_toml_when_global_key_exists(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            (home / "config").mkdir(parents=True)
            (home / "config" / "global.toml").write_text(
                '\n'.join([
                    '[openai]',
                    'model = "global-model"',
                    'base_url = "https://global.example/v1"',
                    'api_key = { source = "store", id = "driver-openai.api_key" }',
                    '',
                ]),
                encoding="utf-8",
            )
            (home / "config" / "skills").mkdir(parents=True)
            (home / "config" / "skills" / "driver-openai.toml").write_text(
                '\n'.join([
                    'model = "legacy-model"',
                    'base_url = "https://legacy.example/v1"',
                    'api_key = { source = "store", id = "driver-openai.legacy" }',
                    '',
                ]),
                encoding="utf-8",
            )
            (home / "secrets.json").write_text(
                '{"driver-openai.api_key":"sk-global","driver-openai.legacy":"sk-legacy"}\n',
                encoding="utf-8",
            )

            with patch.dict(os.environ, {"TABULA_HOME": str(home)}, clear=True):
                cfg = load_skill_config(DRIVER_OPENAI_DIR)

            self.assertEqual(cfg["api_key"], "sk-global")
            self.assertEqual(cfg["base_url"], "https://global.example/v1")
            self.assertEqual(cfg["model"], "global-model")

    def test_env_overrides_skill_config_and_store(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_global_toml(home, '\n'.join([
                '[openai]',
                'model = "file-model"',
                'base_url = "https://file.example/v1"',
                'api_key = { source = "store", id = "driver-openai.api_key" }',
                '',
            ]))
            (home / "secrets.json").write_text(
                '{"driver-openai.api_key":"sk-store-value"}\n',
                encoding="utf-8",
            )

            with patch.dict(
                os.environ,
                {
                    "TABULA_HOME": str(home),
                    "OPENAI_API_KEY": "sk-env-value",
                    "TABULA_SKILL_DRIVER_OPENAI_MODEL": "env-model",
                    "OPENAI_BASE_URL": "https://env.example/v1",
                },
                clear=True,
            ):
                cfg = load_skill_config(DRIVER_OPENAI_DIR)

            self.assertEqual(cfg["api_key"], "sk-env-value")
            self.assertEqual(cfg["base_url"], "https://env.example/v1")
            self.assertEqual(cfg["model"], "env-model")

    def test_missing_required_secret_raises(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            (home / "config" / "skills").mkdir(parents=True)

            with patch.dict(os.environ, {"TABULA_HOME": str(home)}, clear=True):
                with self.assertRaises(SkillConfigError) as ctx:
                    load_skill_config(DRIVER_OPENAI_DIR)

            self.assertIn("api_key", str(ctx.exception))

    def test_driver_openai_module_loads_settings_without_dotenv(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_global_toml(home, '\n'.join([
                '[openai]',
                'model = "gpt-5.4"',
                'base_url = "https://proxy.example/v1"',
                'api_key = { source = "store", id = "driver-openai.api_key" }',
                '',
            ]))
            (home / "secrets.json").write_text(
                '{"driver-openai.api_key":"sk-store-value"}\n',
                encoding="utf-8",
            )

            with patch.dict(os.environ, {"TABULA_HOME": str(home)}, clear=True):
                mod = _load_driver_openai_module()
                settings = mod.load_driver_settings()

            self.assertEqual(settings["api_key"], "sk-store-value")
            self.assertEqual(settings["base_url"], "https://proxy.example/v1")
            self.assertEqual(settings["model"], "gpt-5.4")

    def test_subagent_openai_loads_from_shared_driver_secret_store(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_global_toml(home, '\n'.join([
                '[openai]',
                'model = "gpt-5.4"',
                'base_url = "https://proxy.example/v1"',
                '',
            ]))
            (home / "secrets.json").write_text(
                '{"driver-openai.api_key":"sk-shared-openai"}\n',
                encoding="utf-8",
            )

            with patch.dict(os.environ, {"TABULA_HOME": str(home)}, clear=True):
                cfg = load_skill_config(SUBAGENT_OPENAI_DIR)

            self.assertEqual(cfg["api_key"], "sk-shared-openai")
            self.assertEqual(cfg["base_url"], "https://proxy.example/v1")
            self.assertEqual(cfg["model"], "gpt-5.4")

    def test_subagent_openai_env_overrides_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_global_toml(home, '\n'.join([
                '[openai]',
                'model = "file-model"',
                'base_url = "https://file.example/v1"',
                '',
            ]))
            (home / "secrets.json").write_text(
                '{"driver-openai.api_key":"sk-shared-openai"}\n',
                encoding="utf-8",
            )

            with patch.dict(
                os.environ,
                {
                    "TABULA_HOME": str(home),
                    "TABULA_SKILL_SUBAGENT_OPENAI_MODEL": "env-model",
                    "OPENAI_BASE_URL": "https://env.example/v1",
                    "OPENAI_API_KEY": "sk-env-openai",
                },
                clear=True,
            ):
                cfg = load_skill_config(SUBAGENT_OPENAI_DIR)

            self.assertEqual(cfg["api_key"], "sk-env-openai")
            self.assertEqual(cfg["base_url"], "https://env.example/v1")
            self.assertEqual(cfg["model"], "env-model")

    def test_subagent_openai_module_loads_settings_without_dotenv(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_global_toml(home, '\n'.join([
                '[openai]',
                'model = "gpt-5.4"',
                'base_url = "https://proxy.example/v1"',
                '',
            ]))
            (home / "secrets.json").write_text(
                '{"subagent-openai.api_key":"sk-subagent-openai"}\n',
                encoding="utf-8",
            )

            with patch.dict(os.environ, {"TABULA_HOME": str(home)}, clear=True):
                mod = _load_subagent_openai_module()
                settings = mod.load_subagent_settings()

            self.assertEqual(settings["api_key"], "sk-subagent-openai")
            self.assertEqual(settings["base_url"], "https://proxy.example/v1")
            self.assertEqual(settings["model"], "gpt-5.4")

    def test_loads_driver_anthropic_from_skill_config_and_secret_store(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_global_toml(home, '\n'.join([
                '[anthropic]',
                'model = "claude-sonnet-4-6"',
                'base_url = "https://anthropic-proxy.example"',
                'api_key = { source = "store", id = "driver-anthropic.api_key" }',
                '',
            ]))
            (home / "secrets.json").write_text(
                '{"driver-anthropic.api_key":"sk-ant-store"}\n',
                encoding="utf-8",
            )

            with patch.dict(os.environ, {"TABULA_HOME": str(home)}, clear=True):
                cfg = load_skill_config(DRIVER_ANTHROPIC_DIR)

            self.assertEqual(cfg["api_key"], "sk-ant-store")
            self.assertEqual(cfg["base_url"], "https://anthropic-proxy.example")
            self.assertEqual(cfg["model"], "claude-sonnet-4-6")

    def test_driver_anthropic_env_overrides_skill_config_and_store(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_global_toml(home, '\n'.join([
                '[anthropic]',
                'model = "file-model"',
                'base_url = "https://file.example"',
                'api_key = { source = "store", id = "driver-anthropic.api_key" }',
                '',
            ]))
            (home / "secrets.json").write_text(
                '{"driver-anthropic.api_key":"sk-ant-store"}\n',
                encoding="utf-8",
            )

            with patch.dict(
                os.environ,
                {
                    "TABULA_HOME": str(home),
                    "ANTHROPIC_API_KEY": "sk-ant-env",
                    "TABULA_SKILL_DRIVER_ANTHROPIC_MODEL": "env-model",
                    "ANTHROPIC_BASE_URL": "https://env.example",
                },
                clear=True,
            ):
                cfg = load_skill_config(DRIVER_ANTHROPIC_DIR)

            self.assertEqual(cfg["api_key"], "sk-ant-env")
            self.assertEqual(cfg["base_url"], "https://env.example")
            self.assertEqual(cfg["model"], "env-model")

    def test_driver_anthropic_module_loads_settings_without_dotenv(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_global_toml(home, '\n'.join([
                '[anthropic]',
                'model = "claude-sonnet-4-6"',
                'base_url = "https://anthropic-proxy.example"',
                'api_key = { source = "store", id = "driver-anthropic.api_key" }',
                '',
            ]))
            (home / "secrets.json").write_text(
                '{"driver-anthropic.api_key":"sk-ant-store"}\n',
                encoding="utf-8",
            )

            with patch.dict(os.environ, {"TABULA_HOME": str(home)}, clear=True):
                mod = _load_driver_anthropic_module()
                settings = mod.load_driver_settings()

            self.assertEqual(settings["api_key"], "sk-ant-store")
            self.assertEqual(settings["base_url"], "https://anthropic-proxy.example")
            self.assertEqual(settings["model"], "claude-sonnet-4-6")

    def test_subagent_anthropic_loads_from_shared_driver_secret_store(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_global_toml(home, '\n'.join([
                '[anthropic]',
                'model = "claude-sonnet-4-6"',
                'base_url = "https://anthropic-proxy.example"',
                '',
            ]))
            (home / "secrets.json").write_text(
                '{"driver-anthropic.api_key":"sk-ant-shared"}\n',
                encoding="utf-8",
            )

            with patch.dict(os.environ, {"TABULA_HOME": str(home)}, clear=True):
                cfg = load_skill_config(SUBAGENT_ANTHROPIC_DIR)

            self.assertEqual(cfg["api_key"], "sk-ant-shared")
            self.assertEqual(cfg["base_url"], "https://anthropic-proxy.example")
            self.assertEqual(cfg["model"], "claude-sonnet-4-6")

    def test_subagent_anthropic_env_overrides_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_global_toml(home, '\n'.join([
                '[anthropic]',
                'model = "file-model"',
                'base_url = "https://file.example"',
                '',
            ]))
            (home / "secrets.json").write_text(
                '{"driver-anthropic.api_key":"sk-ant-shared"}\n',
                encoding="utf-8",
            )

            with patch.dict(
                os.environ,
                {
                    "TABULA_HOME": str(home),
                    "TABULA_SKILL_SUBAGENT_ANTHROPIC_MODEL": "env-model",
                    "ANTHROPIC_BASE_URL": "https://env.example",
                    "ANTHROPIC_API_KEY": "sk-ant-env",
                },
                clear=True,
            ):
                cfg = load_skill_config(SUBAGENT_ANTHROPIC_DIR)

            self.assertEqual(cfg["api_key"], "sk-ant-env")
            self.assertEqual(cfg["base_url"], "https://env.example")
            self.assertEqual(cfg["model"], "env-model")

    def test_subagent_anthropic_module_loads_settings_without_dotenv(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_global_toml(home, '\n'.join([
                '[anthropic]',
                'model = "claude-sonnet-4-6"',
                'base_url = "https://anthropic-proxy.example"',
                '',
            ]))
            (home / "secrets.json").write_text(
                '{"subagent-anthropic.api_key":"sk-ant-subagent"}\n',
                encoding="utf-8",
            )

            with patch.dict(os.environ, {"TABULA_HOME": str(home)}, clear=True):
                mod = _load_subagent_anthropic_module()
                settings = mod.load_subagent_settings()

            self.assertEqual(settings["api_key"], "sk-ant-subagent")
            self.assertEqual(settings["base_url"], "https://anthropic-proxy.example")
            self.assertEqual(settings["model"], "claude-sonnet-4-6")

    def test_hook_logger_reads_log_file_from_skill_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_global_toml(home, '[hook.logger]\nlog_file = "/tmp/hooks.jsonl"\n')

            with patch.dict(os.environ, {"TABULA_HOME": str(home)}, clear=True):
                mod = _load_hook_logger_module()

            self.assertEqual(mod.DEFAULT_LOG, "/tmp/hooks.jsonl")

    def test_hook_logger_env_overrides_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_global_toml(home, '[hook.logger]\nlog_file = "/tmp/file-hooks.jsonl"\n')

            with patch.dict(
                os.environ,
                {
                    "TABULA_HOME": str(home),
                    "TABULA_SKILL_HOOK_LOGGER_LOG_FILE": "/tmp/env-hooks.jsonl",
                },
                clear=True,
            ):
                mod = _load_hook_logger_module()

            self.assertEqual(mod.DEFAULT_LOG, "/tmp/env-hooks.jsonl")

    def test_mcp_daemon_reads_pool_settings_from_skill_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_global_toml(home, '\n'.join([
                '[mcp.pool]',
                'url = "http://remote-pool:9000"',
                'host = "127.0.0.1"',
                'port = 8123',
                '',
            ]))

            with patch.dict(os.environ, {"TABULA_HOME": str(home)}, clear=True):
                mod = _load_mcp_daemon_module()

            self.assertEqual(mod.SETTINGS["pool.url"], "http://remote-pool:9000")
            self.assertEqual(mod.SETTINGS["pool.host"], "127.0.0.1")
            self.assertEqual(mod.SETTINGS["pool.port"], 8123)
            self.assertEqual(mod._get_pool_url(), "http://remote-pool:9000")

    def test_mcp_daemon_env_overrides_skill_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_global_toml(home, '\n'.join([
                '[mcp.pool]',
                'url = "http://file-pool:9000"',
                'host = "127.0.0.1"',
                'port = 8123',
                '',
            ]))

            with patch.dict(
                os.environ,
                {
                    "TABULA_HOME": str(home),
                    "TABULA_SKILL_MCP_POOL_URL": "http://env-pool:9100",
                    "TABULA_MCP_POOL_PORT": "9001",
                },
                clear=True,
            ):
                mod = _load_mcp_daemon_module()

            self.assertEqual(mod.SETTINGS["pool.url"], "http://env-pool:9100")
            self.assertEqual(mod.SETTINGS["pool.port"], 9001)

    def test_loads_driver_mock_from_skill_config_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            _write_global_toml(home, '\n'.join([
                '[driver.mock]',
                'subagent_count = 2',
                'max_turns = 7',
                'sleep_ms = 1',
                'default_waves = 2',
                'default_fanouts = [2, 1]',
                '',
            ]))

            with patch.dict(os.environ, {"TABULA_HOME": str(home)}, clear=True):
                cfg = load_skill_config(DRIVER_MOCK_DIR)

            self.assertEqual(cfg["subagent_count"], 2)
            self.assertEqual(cfg["max_turns"], 7)
            self.assertEqual(cfg["sleep_ms"], 1)
            self.assertEqual(cfg["default_waves"], 2)
            self.assertEqual(cfg["default_fanouts"], [2, 1])

    def test_driver_mock_env_overrides_file_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            (home / "config" / "skills").mkdir(parents=True)
            (home / "config" / "skills" / "driver-mock.toml").write_text(
                '\n'.join([
                    'subagent_count = 2',
                    'max_turns = 7',
                    'sleep_ms = 1',
                    'default_waves = 2',
                    '',
                ]),
                encoding="utf-8",
            )

            with patch.dict(
                os.environ,
                {
                    "TABULA_HOME": str(home),
                    "TABULA_SKILL_DRIVER_MOCK_SUBAGENT_COUNT": "4",
                    "TABULA_SKILL_DRIVER_MOCK_MAX_TURNS": "3",
                    "TABULA_SKILL_DRIVER_MOCK_DEFAULT_WAVES": "1",
                },
                clear=True,
            ):
                cfg = load_skill_config(DRIVER_MOCK_DIR)

            self.assertEqual(cfg["subagent_count"], 4)
            self.assertEqual(cfg["max_turns"], 3)
            self.assertEqual(cfg["default_waves"], 1)


if __name__ == "__main__":
    unittest.main()

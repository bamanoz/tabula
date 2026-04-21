#!/usr/bin/env python3
"""Tests for skill-owned path conventions under TABULA_HOME."""

from __future__ import annotations

import importlib.util
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[1]


def _load_module(path: Path, name: str):
    spec = importlib.util.spec_from_file_location(name, path)
    mod = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(mod)
    return mod


def _load_mcp_module(path: Path, module_name: str):
    import types

    package = types.ModuleType("mcp")
    package.__path__ = [str(path.parent)]
    sys_modules = __import__("sys").modules
    sys_modules["mcp"] = package
    spec = importlib.util.spec_from_file_location(
        module_name,
        path,
        submodule_search_locations=[str(path.parent)],
    )
    mod = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    sys_modules[module_name] = mod
    spec.loader.exec_module(mod)
    return mod


def _load_boot_module():
    old = dict(os.environ)
    os.environ["TABULA_HOME"] = str(ROOT)
    os.environ["TABULA_PROVIDER"] = os.environ.get("TABULA_PROVIDER", "openai")
    spec = importlib.util.spec_from_file_location("tabula_main_boot", ROOT / "distrib" / "familiar" / "boot.py")
    mod = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    try:
        spec.loader.exec_module(mod)
    finally:
        os.environ.clear()
        os.environ.update(old)
    return mod


class TestSkillPathLayout(unittest.TestCase):
    def test_boot_managed_paths_follow_new_layout(self):
        boot = _load_boot_module()

        with tempfile.TemporaryDirectory() as tmp:
            orig_home = boot.TABULA_HOME
            orig_perm = boot.PERMISSIONS_FILE
            orig_mcp = boot.MCP_CONFIG
            orig_sub = boot.SUBAGENT_PROMPT_FILE
            try:
                boot.TABULA_HOME = tmp
                boot.PERMISSIONS_FILE = os.path.join(tmp, "config", "skills", "hook-permissions", "permissions.json")
                boot.MCP_CONFIG = os.path.join(tmp, "config", "skills", "mcp", "servers.json")
                boot.SUBAGENT_PROMPT_FILE = os.path.join(tmp, "state", "subagent", "prompt.txt")

                self.assertTrue(boot.PERMISSIONS_FILE.endswith("config/skills/hook-permissions/permissions.json"))
                self.assertTrue(boot.MCP_CONFIG.endswith("config/skills/mcp/servers.json"))
                self.assertTrue(boot.SUBAGENT_PROMPT_FILE.endswith("state/subagent/prompt.txt"))
            finally:
                boot.TABULA_HOME = orig_home
                boot.PERMISSIONS_FILE = orig_perm
                boot.MCP_CONFIG = orig_mcp
                boot.SUBAGENT_PROMPT_FILE = orig_sub

    def test_pair_auth_file_uses_data_dir(self):
        pair_path = ROOT / "distrib" / "familiar" / "skills" / "pair" / "run.py"
        with tempfile.TemporaryDirectory() as tmp:
            old = dict(os.environ)
            try:
                os.environ.clear()
                os.environ["TABULA_HOME"] = tmp
                mod = _load_module(pair_path, "tabula_pair_run")
                self.assertEqual(
                    mod.auth_file("telegram"),
                    Path(tmp) / "data" / "pair" / "telegram.json",
                )
            finally:
                os.environ.clear()
                os.environ.update(old)

    def test_driver_runtime_history_uses_data_sessions_dir(self):
        from skills.lib.driver_runtime import DriverConfig, DriverRuntime

        with tempfile.TemporaryDirectory() as tmp:
            old = dict(os.environ)
            try:
                os.environ.clear()
                os.environ["TABULA_HOME"] = tmp
                with patch("skills.lib.driver_runtime.KernelConnection"):
                    runtime = DriverRuntime(
                        DriverConfig(name="test", url="ws://localhost:1/ws", session="sess-1"),
                        provider_factory=lambda prompt, tools: None,
                        logger=lambda _msg: None,
                    )
                self.assertIsNotNone(runtime._history_file)
                assert runtime._history_file is not None
                self.assertTrue(runtime._history_file.name.endswith("data/sessions/sess-1/history.jsonl"))
                runtime._history_file.close()
            finally:
                os.environ.clear()
                os.environ.update(old)

    def test_cron_jobs_use_data_dir(self):
        cron_path = ROOT / "distrib" / "familiar" / "skills" / "cron" / "run.py"
        with tempfile.TemporaryDirectory() as tmp:
            old = dict(os.environ)
            try:
                os.environ.clear()
                os.environ["TABULA_HOME"] = tmp
                mod = _load_module(cron_path, "tabula_cron_run")
                self.assertEqual(mod.JOBS_PATH, str(Path(tmp) / "data" / "cron" / "jobs.json"))
            finally:
                os.environ.clear()
                os.environ.update(old)

    def test_hook_permissions_uses_config_skills_dir(self):
        hook_path = ROOT / "distrib" / "familiar" / "skills" / "hook-permissions" / "run.py"
        with tempfile.TemporaryDirectory() as tmp:
            old = dict(os.environ)
            try:
                os.environ.clear()
                os.environ["TABULA_HOME"] = tmp
                mod = _load_module(hook_path, "tabula_hook_permissions_run")
                self.assertEqual(
                    mod.PERMISSIONS_FILE,
                    str(Path(tmp) / "config" / "skills" / "hook-permissions" / "permissions.json"),
                )
            finally:
                os.environ.clear()
                os.environ.update(old)

    def test_mcp_pool_uses_config_skills_servers_json(self):
        with tempfile.TemporaryDirectory() as tmp:
            old = dict(os.environ)
            try:
                os.environ.clear()
                os.environ["TABULA_HOME"] = tmp
                mod = _load_mcp_module(ROOT / "distrib" / "familiar" / "skills" / "mcp" / "pool.py", "mcp.pool")
                self.assertEqual(mod.CONFIG_FILE, os.path.join(tmp, "config", "skills", "mcp", "servers.json"))
            finally:
                os.environ.clear()
                os.environ.update(old)

    def test_memory_skill_uses_palace_under_tabula_home(self):
        lib_path = ROOT / "../tabula-bundles" / "memory" / "_memory" / "lib.py"
        with tempfile.TemporaryDirectory() as tmp:
            old = dict(os.environ)
            try:
                os.environ.clear()
                os.environ["TABULA_HOME"] = tmp
                mod = _load_module(lib_path, "tabula_memory_lib")
                self.assertEqual(mod.PALACE_PATH, os.path.join(tmp, "data", "memory", "palace"))
                self.assertEqual(os.environ.get("MEMPALACE_PALACE_PATH"), mod.PALACE_PATH)
            finally:
                os.environ.clear()
                os.environ.update(old)


if __name__ == "__main__":
    unittest.main()

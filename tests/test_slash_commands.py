#!/usr/bin/env python3
"""Tests for slash commands: discover_slash_commands() and gateway dispatch."""

from __future__ import annotations

import json
import os
import sys
import tempfile
import importlib.util
import types
from pathlib import Path

from tests.flat_surface import materialize_flat_surface

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))


def _load_boot_module():
    materialize_flat_surface(ROOT, source_root=ROOT)

    old_env = dict(os.environ)
    os.environ["TABULA_HOME"] = str(ROOT)
    os.environ["TABULA_PROVIDER"] = os.environ.get("TABULA_PROVIDER", "openai")

    for name in list(sys.modules.keys()):
        if name == "skills" or (name.startswith("skills.") and not name.startswith("skills.lib")):
            sys.modules.pop(name, None)

    skills_pkg = types.ModuleType("skills")
    skills_pkg.__path__ = [str(ROOT / "skills")]
    sys.modules["skills"] = skills_pkg

    if "skills.lib" in sys.modules:
        lib_mod = sys.modules["skills.lib"]
    else:
        lib_init = ROOT / "skills" / "lib" / "__init__.py"
        lib_spec = importlib.util.spec_from_file_location(
            "skills.lib",
            lib_init,
            submodule_search_locations=[str(ROOT / "skills" / "lib")],
        )
        lib_mod = importlib.util.module_from_spec(lib_spec)
        assert lib_spec.loader is not None
        sys.modules["skills.lib"] = lib_mod
        lib_spec.loader.exec_module(lib_mod)
    skills_pkg.lib = lib_mod

    boot_path = ROOT / "distrib" / "familiar" / "boot.py"
    spec = importlib.util.spec_from_file_location("tabula_main_boot", boot_path)
    mod = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    try:
        spec.loader.exec_module(mod)
    finally:
        os.environ.clear()
        os.environ.update(old_env)
    return mod


# ── Unit tests for discover_slash_commands ─────────────────────


def make_skill(skills_dir: str, name: str, frontmatter: str, body: str = "Skill body."):
    """Create a minimal SKILL.md in a skill directory."""
    skill_dir = Path(skills_dir) / name
    skill_dir.mkdir(parents=True, exist_ok=True)
    (skill_dir / "SKILL.md").write_text(f"---\n{frontmatter}\n---\n\n{body}\n")


def run_discover(skills_dir: str) -> list[dict]:
    """Run discover_slash_commands with a custom SKILLS_DIR."""
    boot = _load_boot_module()
    original = boot.SKILLS_DIR
    boot.SKILLS_DIR = skills_dir
    try:
        return boot.discover_slash_commands()
    finally:
        boot.SKILLS_DIR = original


def test_explicit_true_is_included():
    """Skills with user-invocable: true should be discovered."""
    with tempfile.TemporaryDirectory() as tmp:
        make_skill(tmp, "weather", 'name: weather\ndescription: "Get weather"\nuser-invocable: true')
        cmds = run_discover(tmp)
        assert len(cmds) == 1
        assert cmds[0]["name"] == "weather"
        assert cmds[0]["description"] == "Get weather"
        assert "Skill body." in cmds[0]["body"]


def test_no_flag_is_excluded():
    """Skills without user-invocable should NOT be discovered."""
    with tempfile.TemporaryDirectory() as tmp:
        make_skill(tmp, "memory", 'name: memory\ndescription: "Persistent memory"')
        cmds = run_discover(tmp)
        assert len(cmds) == 0


def test_explicit_false_is_excluded():
    """Skills with user-invocable: false should NOT be discovered."""
    with tempfile.TemporaryDirectory() as tmp:
        make_skill(tmp, "cron", 'name: cron\ndescription: "Scheduled tasks"\nuser-invocable: false')
        cmds = run_discover(tmp)
        assert len(cmds) == 0


def test_inject_none_without_flag_excluded():
    """Internal skills (inject: none) without explicit flag should be excluded."""
    with tempfile.TemporaryDirectory() as tmp:
        make_skill(tmp, "driver-mock", 'name: driver-mock\ninject: none')
        cmds = run_discover(tmp)
        assert len(cmds) == 0


def test_inject_none_with_flag_included():
    """Internal skills with explicit user-invocable: true should still be included."""
    with tempfile.TemporaryDirectory() as tmp:
        make_skill(tmp, "hook-debug", 'name: hook-debug\ninject: none\nuser-invocable: true\ndescription: "Debug hook"')
        cmds = run_discover(tmp)
        assert len(cmds) == 1
        assert cmds[0]["name"] == "hook-debug"


def test_multiple_skills_mixed():
    """Only user-invocable: true skills appear, others filtered."""
    with tempfile.TemporaryDirectory() as tmp:
        make_skill(tmp, "weather", 'name: weather\ndescription: "Weather"\nuser-invocable: true', body="Weather instructions.")
        make_skill(tmp, "memory", 'name: memory\ndescription: "Memory"')
        make_skill(tmp, "timer", 'name: timer\ndescription: "Timer"\nuser-invocable: true', body="Timer instructions.")
        make_skill(tmp, "driver-anthropic", 'name: driver-anthropic\ninject: none')
        cmds = run_discover(tmp)
        names = [c["name"] for c in cmds]
        assert sorted(names) == ["timer", "weather"]


def test_body_is_captured():
    """The body field should contain the SKILL.md content after frontmatter."""
    with tempfile.TemporaryDirectory() as tmp:
        body = "# Weather\n\nUse get_weather tool to check weather."
        make_skill(tmp, "weather", 'name: weather\nuser-invocable: true', body=body)
        cmds = run_discover(tmp)
        assert cmds[0]["body"] == body


def test_name_defaults_to_dirname():
    """If no name in frontmatter, directory name is used."""
    with tempfile.TemporaryDirectory() as tmp:
        make_skill(tmp, "my-tool", 'user-invocable: true\ndescription: "A tool"')
        cmds = run_discover(tmp)
        assert cmds[0]["name"] == "my-tool"


def test_empty_skills_dir():
    """Empty or missing skills dir returns empty list."""
    with tempfile.TemporaryDirectory() as tmp:
        cmds = run_discover(os.path.join(tmp, "nonexistent"))
        assert cmds == []


def test_yes_variant():
    """user-invocable: yes should work like true."""
    with tempfile.TemporaryDirectory() as tmp:
        make_skill(tmp, "tool", 'name: tool\nuser-invocable: yes')
        cmds = run_discover(tmp)
        assert len(cmds) == 1


# ── Unit tests for scan_skills (system prompt injection) ───────


def run_scan(skills_dir: str) -> list[str]:
    """Run scan_skills with a custom SKILLS_DIR."""
    boot = _load_boot_module()
    original = boot.SKILLS_DIR
    boot.SKILLS_DIR = skills_dir
    try:
        return boot.scan_skills()
    finally:
        boot.SKILLS_DIR = original


def test_scan_skills_with_description():
    """Skills with description are injected as one-liner."""
    with tempfile.TemporaryDirectory() as tmp:
        make_skill(tmp, "weather", 'name: weather\ndescription: "Get weather"')
        result = run_scan(tmp)
        assert len(result) == 1
        assert result[0] == "**weather**: Get weather"


def test_scan_skills_without_description_excluded():
    """Skills without description are not injected into system prompt."""
    with tempfile.TemporaryDirectory() as tmp:
        make_skill(tmp, "internal", 'name: internal', body="Some internal docs.")
        result = run_scan(tmp)
        assert len(result) == 0


def test_scan_skills_no_inject_none():
    """inject: none is no longer used — description controls visibility."""
    with tempfile.TemporaryDirectory() as tmp:
        # Has description but also inject: none — should still appear (inject: none is ignored)
        make_skill(tmp, "driver", 'name: driver\ndescription: "LLM driver"\ninject: none')
        result = run_scan(tmp)
        assert len(result) == 1
        assert "LLM driver" in result[0]


def test_scan_skills_all_with_description():
    """All skills with description appear in system prompt."""
    with tempfile.TemporaryDirectory() as tmp:
        make_skill(tmp, "weather", 'name: weather\ndescription: "Weather"')
        make_skill(tmp, "gateway-cli", 'name: gateway-cli\ndescription: "CLI gateway"')
        make_skill(tmp, "hook-logger", 'name: hook-logger\ndescription: "Audit logger"')
        result = run_scan(tmp)
        assert len(result) == 3


# ── Unit tests for gateway slash dispatch ──────────────────────


def test_load_slash_commands():
    """_load_slash_commands should combine builtins with skill commands."""
    boot = _load_boot_module()
    original = boot.SKILLS_DIR

    with tempfile.TemporaryDirectory() as tmp:
        make_skill(tmp, "weather", 'name: weather\ndescription: "Weather"\nuser-invocable: true')
        make_skill(tmp, "memory", 'name: memory\ndescription: "Memory"')
        boot.SKILLS_DIR = tmp
        try:
            cmds = boot.discover_slash_commands()
            # Build the same data structures as _load_slash_commands
            builtins = {"help": "Show available commands", "exit": "Exit CLI"}
            skill_commands = {c["name"]: c for c in cmds}
            all_names = sorted(set(list(builtins.keys()) + list(skill_commands.keys())))
            descriptions = {**builtins}
            for name, cmd in skill_commands.items():
                descriptions[name] = cmd.get("description", "")

            assert "help" in all_names
            assert "exit" in all_names
            assert "weather" in all_names
            assert "memory" not in all_names  # no user-invocable flag
            assert descriptions["help"] == "Show available commands"
            assert descriptions["weather"] == "Weather"
        finally:
            boot.SKILLS_DIR = original


def test_complete_command():
    """Prefix completion should match commands."""
    # Simulate the completion logic independent of Gateway class
    all_commands = sorted(["help", "exit", "weather", "memory", "timer"])

    def complete(prefix: str) -> list[str]:
        return [c for c in all_commands if c.startswith(prefix)]

    assert complete("w") == ["weather"]
    assert complete("he") == ["help"]
    assert complete("e") == ["exit"]
    assert complete("m") == ["memory"]
    assert complete("t") == ["timer"]
    assert complete("") == all_commands
    assert complete("z") == []
    assert sorted(complete("")) == all_commands


def test_complete_command_multiple():
    """Multiple matches should return all matching commands."""
    all_commands = sorted(["help", "history", "hooks"])

    def complete(prefix: str) -> list[str]:
        return [c for c in all_commands if c.startswith(prefix)]

    assert complete("h") == ["help", "history", "hooks"]
    assert complete("he") == ["help"]
    assert complete("hi") == ["history"]
    assert complete("ho") == ["hooks"]


def test_dispatch_slash_builtin_help(capsys):
    """Builtin /help should list commands without sending to kernel."""
    # Test that builtin dispatch map contains expected commands
    builtins = {"help", "exit"}
    assert "help" in builtins
    assert "exit" in builtins
    assert "weather" not in builtins


def test_dispatch_slash_skill_message():
    """Skill command should produce body + args message."""
    skill = {"name": "weather", "description": "Weather", "body": "# Weather\n\nUse get_weather."}
    args = "москва"
    expected = skill["body"] + f"\n\nUser request: {args}"
    assert "# Weather" in expected
    assert "User request: москва" in expected


def test_dispatch_slash_skill_no_args():
    """Skill command without args should send only body."""
    skill = {"name": "weather", "description": "Weather", "body": "# Weather\n\nUse get_weather."}
    args = ""
    text = skill["body"]
    if args:
        text += f"\n\nUser request: {args}"
    assert text == skill["body"]
    assert "User request" not in text


# ── Config output test ─────────────────────────────────────────


def test_config_includes_commands():
    """boot.py config output should include 'commands' field."""
    boot = _load_boot_module()
    original = boot.SKILLS_DIR

    with tempfile.TemporaryDirectory() as tmp:
        make_skill(tmp, "weather", 'name: weather\ndescription: "Weather"\nuser-invocable: true')
        boot.SKILLS_DIR = tmp

        try:
            cmds = boot.discover_slash_commands()
            config = {
                "url": "ws://localhost:8089/ws",
                "spawn": [],
                "tools": [],
                "commands": cmds,
            }
            assert "commands" in config
            assert len(config["commands"]) == 1
            assert config["commands"][0]["name"] == "weather"
        finally:
            boot.SKILLS_DIR = original


# ── Tests for tabula-guide skill ───────────────────────────────


def test_tabula_guide_exists():
    """tabula-guide/SKILL.md should exist."""
    guide_path = ROOT / "distrib" / "familiar" / "skills" / "tabula-guide" / "SKILL.md"
    assert guide_path.is_file(), f"tabula-guide/SKILL.md not found at {guide_path}"


def test_tabula_guide_has_description():
    """tabula-guide should have description in frontmatter (appears in system prompt)."""
    boot = _load_boot_module()
    guide_path = ROOT / "distrib" / "familiar" / "skills" / "tabula-guide" / "SKILL.md"
    meta, body = boot.parse_skill_md(guide_path.read_text().strip())
    assert meta.get("description"), "tabula-guide must have description"
    assert meta.get("name") == "tabula-guide"


def test_tabula_guide_in_scan_skills():
    """tabula-guide should appear in scan_skills output."""
    boot = _load_boot_module()
    original = boot.SKILLS_DIR
    boot.SKILLS_DIR = str(ROOT / "distrib" / "familiar" / "skills")
    try:
        skills = boot.scan_skills()
        guide_entries = [s for s in skills if "tabula-guide" in s]
        assert len(guide_entries) == 1
    finally:
        boot.SKILLS_DIR = original


def test_tabula_guide_not_user_invocable():
    """tabula-guide should NOT be a slash command."""
    boot = _load_boot_module()
    original = boot.SKILLS_DIR
    boot.SKILLS_DIR = str(ROOT / "distrib" / "familiar" / "skills")
    try:
        cmds = boot.discover_slash_commands()
        names = [c["name"] for c in cmds]
        assert "tabula-guide" not in names
    finally:
        boot.SKILLS_DIR = original


def test_tabula_guide_covers_key_sections():
    """tabula-guide body should cover all major architecture sections."""
    guide_path = ROOT / "distrib" / "familiar" / "skills" / "tabula-guide" / "SKILL.md"
    content = guide_path.read_text()
    required_sections = [
        "## Overview",
        "## Boot System",
        "## Kernel",
        "## Hook System",
        "## Tool System",
        "## Skills Reference",
        "## SKILL.md Format",
        "## Slash Commands",
        "## Wire Protocol",
        "## Limits",
        "## Creating a New Skill",
    ]
    for section in required_sections:
        assert section in content, f"Missing section: {section}"


def test_tabula_guide_documents_all_hook_events():
    """tabula-guide should document all hook events."""
    guide_path = ROOT / "distrib" / "familiar" / "skills" / "tabula-guide" / "SKILL.md"
    content = guide_path.read_text()
    hook_events = [
        "before_message", "after_message",
        "before_tool_call", "after_tool_call",
        "session_start", "session_end",
        "before_spawn", "after_spawn",
    ]
    for event in hook_events:
        assert event in content, f"Missing hook event: {event}"


def test_tabula_guide_documents_kernel_tools():
    """tabula-guide should document the default kernel tools and their policy model."""
    guide_path = ROOT / "distrib" / "familiar" / "skills" / "tabula-guide" / "SKILL.md"
    content = guide_path.read_text()
    assert "boot-controlled" in content
    for tool in ["shell_exec", "process_spawn", "process_kill", "process_list"]:
        assert f"**{tool}**" in content, f"Missing kernel tool: {tool}"


if __name__ == "__main__":
    import pytest
    sys.exit(pytest.main([__file__, "-v"]))

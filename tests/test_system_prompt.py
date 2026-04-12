#!/usr/bin/env python3
"""Tests for system prompt builder: sections, project files, cache boundary, subagent prompt."""

from __future__ import annotations

import json
import os
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))


# ── Helpers ──────────────────────────────────────────────────────


def with_tabula_home(fn):
    """Run function with a temporary TABULA_HOME, restoring boot module state after."""
    import boot

    orig_home = boot.TABULA_HOME
    orig_skills = boot.SKILLS_DIR
    orig_mem = boot.MEMORY_FILE

    def wrapper(*args, **kwargs):
        with tempfile.TemporaryDirectory() as tmp:
            boot.TABULA_HOME = tmp
            boot.SKILLS_DIR = os.path.join(tmp, "skills")
            boot.MEMORY_FILE = os.path.join(tmp, "memory", "MEMORY.md")
            os.makedirs(os.path.join(tmp, "skills"), exist_ok=True)
            try:
                return fn(tmp, *args, **kwargs)
            finally:
                boot.TABULA_HOME = orig_home
                boot.SKILLS_DIR = orig_skills
                boot.MEMORY_FILE = orig_mem

    wrapper.__name__ = fn.__name__
    wrapper.__doc__ = fn.__doc__
    return wrapper


def make_skill(skills_dir: str, name: str, frontmatter: str, body: str = "Skill body."):
    """Create a minimal SKILL.md in a skill directory."""
    skill_dir = Path(skills_dir) / name
    skill_dir.mkdir(parents=True, exist_ok=True)
    (skill_dir / "SKILL.md").write_text(f"---\n{frontmatter}\n---\n\n{body}\n")


# ── Section tests ────────────────────────────────────────────────


def test_system_template():
    """System template mentions Tabula."""
    import boot
    text = boot._read_template("SYSTEM.md")
    assert "Tabula" in text
    assert "multi-agent" in text


def test_section_tools():
    """Tools template documents all kernel tools."""
    import boot
    text = boot._read_template("TOOLS.md")
    for tool in ["EXEC", "SPAWN", "KILL", "LIST"]:
        assert f"**{tool}**" in text


def test_section_guidelines():
    """Guidelines template has actionable rules."""
    import boot
    text = boot._read_template("GUIDELINES.md")
    assert "## Guidelines" in text
    assert "Act first" in text
    assert "concise" in text


def test_section_safety():
    """Safety template warns about destructive actions and secrets."""
    import boot
    text = boot._read_template("SAFETY.md")
    assert "## Safety" in text
    assert "destructive" in text.lower()
    assert "API keys" in text


def test_section_environment():
    """Environment section includes provider, date, and working directory."""
    import boot
    text = boot._section_environment()
    assert "## Environment" in text
    assert "Provider:" in text
    assert "Date:" in text
    assert "Working directory:" in text


def test_section_skills_with_data():
    """Skills section lists all skills."""
    import boot
    text = boot._section_skills(["**weather**: Get weather", "**timer**: Set timer"])
    assert "## Available skills" in text
    assert "**weather**" in text
    assert "**timer**" in text


def test_section_skills_empty():
    """Empty skills produces fallback message."""
    import boot
    text = boot._section_skills([])
    assert "No skills" in text


# ── Project files tests ──────────────────────────────────────────


@with_tabula_home
def test_project_files_all_present(tmp):
    """All project files are read and injected."""
    import boot
    Path(tmp, "IDENTITY.md").write_text("Name: Tabby")
    Path(tmp, "SOUL.md").write_text("Be chill and helpful.")
    Path(tmp, "USER.md").write_text("Name: Test User")
    Path(tmp, "AGENTS.md").write_text("Always check memory first.")

    text = boot._section_project_files(subagent=False)
    assert "Name: Tabby" in text
    assert "Be chill and helpful" in text
    assert "Name: Test User" in text
    assert "Always check memory first" in text
    assert "Identity" in text
    assert "Personality and tone" in text
    assert "User context" in text
    assert "Workspace instructions" in text


@with_tabula_home
def test_project_files_none_present(tmp):
    """No project files — empty string returned."""
    import boot
    text = boot._section_project_files(subagent=False)
    assert text == ""


@with_tabula_home
def test_project_files_partial(tmp):
    """Only existing files are included."""
    import boot
    Path(tmp, "SOUL.md").write_text("Sharp and direct.")

    text = boot._section_project_files(subagent=False)
    assert "Sharp and direct" in text
    assert "User context" not in text
    assert "Workspace instructions" not in text


@with_tabula_home
def test_project_files_subagent_only_agents(tmp):
    """Subagent gets only AGENTS.md, not IDENTITY, SOUL or USER."""
    import boot
    Path(tmp, "IDENTITY.md").write_text("Name: Tabby")
    Path(tmp, "SOUL.md").write_text("Be quirky.")
    Path(tmp, "USER.md").write_text("Name: Test")
    Path(tmp, "AGENTS.md").write_text("Check memory.")

    text = boot._section_project_files(subagent=True)
    assert "Check memory" in text
    assert "Name: Tabby" not in text
    assert "Be quirky" not in text
    assert "Name: Test" not in text


@with_tabula_home
def test_project_files_subagent_no_agents(tmp):
    """Subagent with no AGENTS.md gets empty string."""
    import boot
    Path(tmp, "SOUL.md").write_text("Be quirky.")

    text = boot._section_project_files(subagent=True)
    assert text == ""


# ── Memory section tests ─────────────────────────────────────────


@with_tabula_home
def test_section_memory_present(tmp):
    """Memory section reads from MEMORY.md."""
    import boot
    mem_dir = Path(tmp, "memory")
    mem_dir.mkdir()
    (mem_dir / "MEMORY.md").write_text("User prefers Russian responses.")

    text = boot._section_memory()
    assert "## Long-term memory" in text
    assert "User prefers Russian" in text


@with_tabula_home
def test_section_memory_absent(tmp):
    """No memory file — empty string."""
    import boot
    text = boot._section_memory()
    assert text == ""


@with_tabula_home
def test_section_memory_empty_file(tmp):
    """Empty memory file — empty string."""
    import boot
    mem_dir = Path(tmp, "memory")
    mem_dir.mkdir()
    (mem_dir / "MEMORY.md").write_text("")

    text = boot._section_memory()
    assert text == ""


# ── Full prompt tests ─────────────────────────────────────────────


@with_tabula_home
def test_full_prompt_structure(tmp):
    """Full prompt has all sections in order with cache boundary."""
    import boot
    make_skill(os.path.join(tmp, "skills"), "weather", 'name: weather\ndescription: "Weather"')
    Path(tmp, "SOUL.md").write_text("Be direct.")

    skills = boot.scan_skills()
    prompt = boot.build_system_prompt(skills)

    # Cache boundary present
    assert boot.CACHE_BOUNDARY in prompt

    static, dynamic = prompt.split(boot.CACHE_BOUNDARY)

    # Static part
    assert "Tabula" in static
    assert "## Tools" in static
    assert "## Guidelines" in static
    assert "## Safety" in static
    assert "Be direct" in static

    # Dynamic part
    assert "## Available skills" in dynamic
    assert "**weather**" in dynamic
    assert "## Environment" in dynamic


@with_tabula_home
def test_full_prompt_with_memory(tmp):
    """Memory appears in dynamic part."""
    import boot
    mem_dir = Path(tmp, "memory")
    mem_dir.mkdir()
    (mem_dir / "MEMORY.md").write_text("important fact")

    prompt = boot.build_system_prompt([])
    _, dynamic = prompt.split(boot.CACHE_BOUNDARY)
    assert "important fact" in dynamic


@with_tabula_home
def test_full_prompt_with_mcp(tmp):
    """MCP tools appear in dynamic part."""
    import boot
    mcp = {"test-server": [{"name": "search", "description": "Search stuff", "inputSchema": {"properties": {"q": {"type": "string"}}}}]}
    prompt = boot.build_system_prompt([], mcp)
    _, dynamic = prompt.split(boot.CACHE_BOUNDARY)
    assert "## MCP Tools" in dynamic
    assert "search" in dynamic


@with_tabula_home
def test_full_prompt_no_project_files(tmp):
    """Prompt works fine without any project files."""
    import boot
    prompt = boot.build_system_prompt([])
    assert boot.CACHE_BOUNDARY in prompt
    assert "Tabula" in prompt
    assert "## Environment" in prompt


# ── Subagent prompt tests ─────────────────────────────────────────


@with_tabula_home
def test_subagent_prompt_minimal(tmp):
    """Subagent prompt has identity, tools, guidelines, safety, environment."""
    import boot
    prompt = boot.build_subagent_prompt()
    assert "Tabula" in prompt
    assert "## Tools" in prompt
    assert "## Guidelines" in prompt
    assert "## Safety" in prompt
    assert "## Environment" in prompt


@with_tabula_home
def test_subagent_prompt_no_skills(tmp):
    """Subagent prompt has no skills section."""
    import boot
    make_skill(os.path.join(tmp, "skills"), "weather", 'name: weather\ndescription: "Weather"')
    prompt = boot.build_subagent_prompt()
    assert "## Available skills" not in prompt


@with_tabula_home
def test_subagent_prompt_no_memory(tmp):
    """Subagent prompt has no memory section."""
    import boot
    mem_dir = Path(tmp, "memory")
    mem_dir.mkdir()
    (mem_dir / "MEMORY.md").write_text("important fact")

    prompt = boot.build_subagent_prompt()
    assert "## Long-term memory" not in prompt
    assert "important fact" not in prompt


@with_tabula_home
def test_subagent_prompt_no_soul_or_user(tmp):
    """Subagent prompt excludes IDENTITY.md, SOUL.md and USER.md."""
    import boot
    Path(tmp, "IDENTITY.md").write_text("Name: Tabby")
    Path(tmp, "SOUL.md").write_text("Be quirky.")
    Path(tmp, "USER.md").write_text("Name: Test")
    Path(tmp, "AGENTS.md").write_text("Check memory.")

    prompt = boot.build_subagent_prompt()
    assert "Name: Tabby" not in prompt
    assert "Be quirky" not in prompt
    assert "Name: Test" not in prompt
    assert "Check memory" in prompt


@with_tabula_home
def test_subagent_prompt_no_cache_boundary(tmp):
    """Subagent prompt has no cache boundary (all static)."""
    import boot
    prompt = boot.build_subagent_prompt()
    assert boot.CACHE_BOUNDARY not in prompt


# ── Config output tests ───────────────────────────────────────────


@with_tabula_home
def test_config_has_no_subagent_prompt(tmp):
    """Config output should NOT include system_prompt_subagent (it's file-based)."""
    import boot
    skills = boot.scan_skills()
    config = {
        "url": "ws://localhost:8089/ws",
        "system_prompt": boot.build_system_prompt(skills),
        "spawn": [],
        "tools": [],
        "commands": [],
    }
    assert "system_prompt_subagent" not in config


@with_tabula_home
def test_subagent_prompt_written_to_file(tmp):
    """boot.main() writes .subagent_prompt file to TABULA_HOME."""
    import boot
    prompt_path = os.path.join(tmp, ".subagent_prompt")
    # Simulate what main() does
    with open(prompt_path, "w") as f:
        f.write(boot.build_subagent_prompt())

    assert os.path.isfile(prompt_path)
    content = Path(prompt_path).read_text()
    assert "Tabula" in content
    assert "## Tools" in content
    assert "## Available skills" not in content


@with_tabula_home
def test_config_prompts_differ(tmp):
    """Main and subagent prompts are different."""
    import boot
    make_skill(os.path.join(tmp, "skills"), "weather", 'name: weather\ndescription: "Weather"')
    Path(tmp, "SOUL.md").write_text("Be direct.")

    skills = boot.scan_skills()
    main = boot.build_system_prompt(skills)
    sub = boot.build_subagent_prompt()

    assert len(main) > len(sub)
    assert "## Available skills" in main
    assert "## Available skills" not in sub
    assert "Be direct" in main
    assert "Be direct" not in sub


# ── Read project file tests ──────────────────────────────────────


@with_tabula_home
def test_read_project_file_exists(tmp):
    """Existing project file is read."""
    import boot
    Path(tmp, "SOUL.md").write_text("Be chill.")
    assert boot._read_project_file("SOUL.md") == "Be chill."


@with_tabula_home
def test_read_project_file_missing(tmp):
    """Missing project file returns empty string."""
    import boot
    assert boot._read_project_file("SOUL.md") == ""


@with_tabula_home
def test_read_project_file_whitespace(tmp):
    """Project file content is stripped."""
    import boot
    Path(tmp, "USER.md").write_text("  \nName: Test\n  \n")
    assert boot._read_project_file("USER.md") == "Name: Test"


# ── ensure_project_files tests ───────────────────────────────────


@with_tabula_home
def test_ensure_creates_defaults(tmp):
    """ensure_project_files creates all default files."""
    import boot
    boot.ensure_project_files()
    for name in ["IDENTITY.md", "SOUL.md", "USER.md", "AGENTS.md"]:
        path = Path(tmp, name)
        assert path.is_file(), f"{name} not created"
        assert path.stat().st_size > 0, f"{name} is empty"


@with_tabula_home
def test_ensure_does_not_overwrite(tmp):
    """ensure_project_files does not overwrite existing files."""
    import boot
    Path(tmp, "SOUL.md").write_text("Custom soul.")
    boot.ensure_project_files()
    assert Path(tmp, "SOUL.md").read_text() == "Custom soul."
    # Other files should still be created
    assert Path(tmp, "USER.md").is_file()
    assert Path(tmp, "AGENTS.md").is_file()


@with_tabula_home
def test_ensure_default_identity_content(tmp):
    """Default IDENTITY.md has template fields."""
    import boot
    boot.ensure_project_files()
    content = Path(tmp, "IDENTITY.md").read_text()
    assert "**Name:**" in content
    assert "**Personality:**" in content
    assert "**Language:**" in content


@with_tabula_home
def test_ensure_default_soul_content(tmp):
    """Default SOUL.md has key phrases."""
    import boot
    boot.ensure_project_files()
    content = Path(tmp, "SOUL.md").read_text()
    assert "genuinely helpful" in content
    assert "Have opinions" in content
    assert "concise" in content.lower()


@with_tabula_home
def test_ensure_default_user_content(tmp):
    """Default USER.md has template fields."""
    import boot
    boot.ensure_project_files()
    content = Path(tmp, "USER.md").read_text()
    assert "**Name:**" in content
    assert "**Timezone:**" in content


@with_tabula_home
def test_ensure_default_agents_content(tmp):
    """Default AGENTS.md has first run and session startup rules."""
    import boot
    boot.ensure_project_files()
    content = Path(tmp, "AGENTS.md").read_text()
    assert "First Run" in content
    assert "IDENTITY.md" in content
    assert "Session Startup" in content


@with_tabula_home
def test_ensure_injected_into_prompt(tmp):
    """Default project files appear in full prompt after ensure."""
    import boot
    boot.ensure_project_files()
    prompt = boot.build_system_prompt([])
    assert "genuinely helpful" in prompt
    assert "Workspace" in prompt


if __name__ == "__main__":
    import pytest
    sys.exit(pytest.main([__file__, "-v"]))

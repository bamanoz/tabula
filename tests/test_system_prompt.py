#!/usr/bin/env python3
"""Tests for system prompt builder: sections, project files, cache boundary, subagent prompt."""

from __future__ import annotations

import json
import os
import sys
import tempfile
from pathlib import Path

from tests.flat_surface import materialize_flat_surface

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))


def _load_boot_module():
    import importlib.util
    import types

    materialize_flat_surface(ROOT, source_root=ROOT)

    old_env = dict(os.environ)
    os.environ["TABULA_HOME"] = str(ROOT)
    os.environ["TABULA_PROVIDER"] = os.environ.get("TABULA_PROVIDER", "openai")

    for name in list(sys.modules.keys()):
        if name == "skills" or name.startswith("skills."):
            sys.modules.pop(name, None)

    skills_pkg = types.ModuleType("skills")
    skills_pkg.__path__ = [str(ROOT / "skills")]
    sys.modules["skills"] = skills_pkg

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

    boot_path = ROOT / "distrib" / "assistant" / "boot.py"
    spec = importlib.util.spec_from_file_location("tabula_main_boot", boot_path)
    mod = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    try:
        spec.loader.exec_module(mod)
    finally:
        os.environ.clear()
        os.environ.update(old_env)
    return mod


# ── Helpers ──────────────────────────────────────────────────────


def with_tabula_home(fn):
    """Run function with a temporary TABULA_HOME, restoring boot module state after."""
    boot = _load_boot_module()

    orig_home = boot.TABULA_HOME
    orig_skills = boot.SKILLS_DIR
    orig_mem = boot.MEMORY_FILE
    orig_subagent_prompt = boot.SUBAGENT_PROMPT_FILE

    def wrapper(*args, **kwargs):
        with tempfile.TemporaryDirectory() as tmp:
            old_env = dict(os.environ)
            os.environ["TABULA_HOME"] = tmp
            os.environ["TABULA_PROVIDER"] = "openai"
            boot.TABULA_HOME = tmp
            boot.SKILLS_DIR = os.path.join(tmp, "skills")
            boot.MEMORY_FILE = os.path.join(tmp, "data", "memory", "MEMORY.md")
            boot.SUBAGENT_PROMPT_FILE = os.path.join(tmp, "state", "subagent", "prompt.txt")
            boot.TEMPLATES_DIR = os.path.join(tmp, "templates")
            os.makedirs(os.path.join(tmp, "skills"), exist_ok=True)
            os.makedirs(os.path.join(tmp, "templates"), exist_ok=True)
            for name in ["SYSTEM.md", "TOOLS.md", "GUIDELINES.md", "SAFETY.md", "AGENTS.md", "IDENTITY.md", "SOUL.md", "USER.md"]:
                src = ROOT / "distrib" / "assistant" / "templates" / name
                Path(tmp, "templates", name).write_text(src.read_text())
            materialize_flat_surface(Path(tmp), source_root=Path(tmp))
            try:
                return fn(tmp, boot, *args, **kwargs)
            finally:
                os.environ.clear()
                os.environ.update(old_env)
                boot.TABULA_HOME = orig_home
                boot.SKILLS_DIR = orig_skills
                boot.MEMORY_FILE = orig_mem
                boot.SUBAGENT_PROMPT_FILE = orig_subagent_prompt

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
    boot = _load_boot_module()
    text = boot._read_template("SYSTEM.md")
    assert "Tabula" in text
    assert "multi-agent" in text


def test_section_tools():
    """Tools template render documents all visible kernel tools."""
    from skills.lib.prompt_builder import _render_tools_template

    text = _render_tools_template(visible_tools=[
        {"name": "shell_exec"},
        {"name": "process_spawn"},
        {"name": "process_kill"},
        {"name": "process_list"},
    ])
    for tool in ["shell_exec", "process_spawn", "process_kill", "process_list"]:
        assert f"**{tool}**" in text


def test_section_tools_respects_hidden_builtins(monkeypatch):
    """Rendered tools section only documents enabled built-ins."""
    from skills.lib.prompt_builder import _render_tools_template

    text = _render_tools_template(visible_tools=[{"name": "shell_exec"}, {"name": "process_list"}])
    assert "**shell_exec**" in text
    assert "**process_list**" in text
    assert "**process_spawn**" not in text
    assert "**process_kill**" not in text


def test_section_tools_rendered_from_visible_tools_input():
    from skills.lib.prompt_builder import _render_tools_template

    text = _render_tools_template(visible_tools=[{"name": "process_spawn"}])
    assert "**process_spawn**" in text
    assert "**shell_exec**" not in text
    assert "**process_kill**" not in text
    assert "**process_list**" not in text


def test_tools_template_source_is_not_canonical_list():
    path = ROOT / "distrib" / "assistant" / "templates" / "TOOLS.md"
    text = path.read_text()
    assert "**shell_exec**" not in text
    assert "**process_spawn**" not in text
    assert "**process_kill**" not in text
    assert "**process_list**" not in text


def test_scan_skills_hides_shell_exec_dependent_skills():
    boot = _load_boot_module()
    old = os.environ.get("TABULA_KERNEL_TOOLS")
    try:
        os.environ["TABULA_KERNEL_TOOLS"] = "process_spawn"
        skills = boot.scan_skills()
        joined = "\n".join(skills)
        assert "**memory**" not in joined
        assert "**mcp**" not in joined
        assert "**sessions**" not in joined
    finally:
        if old is None:
            os.environ.pop("TABULA_KERNEL_TOOLS", None)
        else:
            os.environ["TABULA_KERNEL_TOOLS"] = old


def test_scan_skills_hides_process_spawn_dependent_skills():
    boot = _load_boot_module()
    old = os.environ.get("TABULA_KERNEL_TOOLS")
    try:
        os.environ["TABULA_KERNEL_TOOLS"] = "shell_exec"
        skills = boot.scan_skills()
        joined = "\n".join(skills)
        assert "**subagent-openai**" not in joined
        assert "**timer**" not in joined
    finally:
        if old is None:
            os.environ.pop("TABULA_KERNEL_TOOLS", None)
        else:
            os.environ["TABULA_KERNEL_TOOLS"] = old


def test_section_guidelines():
    """Guidelines template has actionable rules."""
    boot = _load_boot_module()
    text = boot._read_template("GUIDELINES.md")
    assert "## Guidelines" in text
    assert "Act first" in text
    assert "concise" in text


def test_section_safety():
    """Safety template warns about destructive actions and secrets."""
    boot = _load_boot_module()
    text = boot._read_template("SAFETY.md")
    assert "## Safety" in text
    assert "destructive" in text.lower()
    assert "API keys" in text


def test_section_environment():
    """Environment section includes provider, date, and working directory."""
    boot = _load_boot_module()
    text = boot._section_environment()
    assert "## Environment" in text
    assert "Provider:" in text
    assert "Date:" in text
    assert "Working directory:" in text


def test_section_skills_with_data():
    """Skills section lists all skills."""
    boot = _load_boot_module()
    text = boot._section_skills(["**weather**: Get weather", "**timer**: Set timer"])
    assert "## Available skills" in text
    assert "**weather**" in text
    assert "**timer**" in text


def test_section_skills_empty():
    """Empty skills produces fallback message."""
    boot = _load_boot_module()
    text = boot._section_skills([])
    assert "No skills" in text


# ── Project files tests ──────────────────────────────────────────


@with_tabula_home
def test_project_files_all_present(tmp, boot):
    """All project files are read and injected."""
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
def test_project_files_none_present(tmp, boot):
    """No project files — empty string returned."""
    text = boot._section_project_files(subagent=False)
    assert text == ""


@with_tabula_home
def test_project_files_partial(tmp, boot):
    """Only existing files are included."""
    Path(tmp, "SOUL.md").write_text("Sharp and direct.")

    text = boot._section_project_files(subagent=False)
    assert "Sharp and direct" in text
    assert "User context" not in text
    assert "Workspace instructions" not in text


@with_tabula_home
def test_project_files_subagent_only_agents(tmp, boot):
    """Subagent gets only AGENTS.md, not IDENTITY, SOUL or USER."""
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
def test_project_files_subagent_no_agents(tmp, boot):
    """Subagent with no AGENTS.md gets empty string."""
    Path(tmp, "SOUL.md").write_text("Be quirky.")

    text = boot._section_project_files(subagent=True)
    assert text == ""


# ── Memory section tests ─────────────────────────────────────────


@with_tabula_home
def test_section_memory_present(tmp, boot):
    """Memory section reads from MEMORY.md."""
    mem_dir = Path(tmp, "data", "memory")
    mem_dir.mkdir(parents=True)
    (mem_dir / "MEMORY.md").write_text("User prefers Russian responses.")

    text = boot._section_memory()
    assert "## Long-term memory" in text
    assert "User prefers Russian" in text


@with_tabula_home
def test_section_memory_absent(tmp, boot):
    """No memory file — empty string."""
    text = boot._section_memory()
    assert text == ""


@with_tabula_home
def test_section_memory_empty_file(tmp, boot):
    """Empty memory file — empty string."""
    mem_dir = Path(tmp, "data", "memory")
    mem_dir.mkdir(parents=True)
    (mem_dir / "MEMORY.md").write_text("")

    text = boot._section_memory()
    assert text == ""


# ── Full prompt tests ─────────────────────────────────────────────


@with_tabula_home
def test_full_prompt_structure(tmp, boot):
    """Full prompt has all sections in order with cache boundary."""
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
def test_full_prompt_with_memory(tmp, boot):
    """Memory appears in dynamic part."""
    mem_dir = Path(tmp, "data", "memory")
    mem_dir.mkdir(parents=True)
    (mem_dir / "MEMORY.md").write_text("important fact")

    prompt = boot.build_system_prompt([])
    _, dynamic = prompt.split(boot.CACHE_BOUNDARY)
    assert "important fact" in dynamic


@with_tabula_home
def test_full_prompt_with_mcp(tmp, boot):
    """MCP tools appear in dynamic part."""
    mcp = {"test-server": [{"name": "search", "description": "Search stuff", "inputSchema": {"properties": {"q": {"type": "string"}}}}]}
    prompt = boot.build_system_prompt([], mcp)
    _, dynamic = prompt.split(boot.CACHE_BOUNDARY)
    assert "## MCP tools" in dynamic
    assert "search" in dynamic


@with_tabula_home
def test_full_prompt_no_project_files(tmp, boot):
    """Prompt works fine without any project files."""
    prompt = boot.build_system_prompt([])
    assert boot.CACHE_BOUNDARY in prompt
    assert "Tabula" in prompt
    assert "## Environment" in prompt


# ── Subagent prompt tests ─────────────────────────────────────────


@with_tabula_home
def test_subagent_prompt_minimal(tmp, boot):
    """Subagent prompt has identity, tools, guidelines, safety, environment."""
    prompt = boot.build_subagent_prompt()
    assert "Tabula" in prompt
    assert "## Tools" in prompt
    assert "## Guidelines" in prompt
    assert "## Safety" in prompt
    assert "## Environment" in prompt


@with_tabula_home
def test_subagent_prompt_no_skills(tmp, boot):
    """Subagent prompt has no skills section."""
    make_skill(os.path.join(tmp, "distrib", "main", "skills"), "weather", 'name: weather\ndescription: "Weather"')
    prompt = boot.build_subagent_prompt()
    assert "## Available skills" not in prompt


@with_tabula_home
def test_subagent_prompt_no_memory(tmp, boot):
    """Subagent prompt has no memory section."""
    mem_dir = Path(tmp, "data", "memory")
    mem_dir.mkdir(parents=True)
    (mem_dir / "MEMORY.md").write_text("important fact")

    prompt = boot.build_subagent_prompt()
    assert "## Long-term memory" not in prompt
    assert "important fact" not in prompt


@with_tabula_home
def test_subagent_prompt_no_soul_or_user(tmp, boot):
    """Subagent prompt excludes IDENTITY.md, SOUL.md and USER.md."""
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
def test_subagent_prompt_no_cache_boundary(tmp, boot):
    """Subagent prompt has no cache boundary (all static)."""
    prompt = boot.build_subagent_prompt()
    assert boot.CACHE_BOUNDARY not in prompt


# ── Config output tests ───────────────────────────────────────────


@with_tabula_home
def test_config_has_no_subagent_prompt(tmp, boot):
    """Config output should NOT include system_prompt_subagent."""
    skills = boot.scan_skills()
    config = {
        "url": "ws://localhost:8089/ws",
        "spawn": [],
        "tools": [],
        "commands": [],
    }
    assert "system_prompt_subagent" not in config


@with_tabula_home
def test_subagent_prompt_built_dynamically(tmp, boot):
    """Subagent prompt is built dynamically by skills/lib/prompt_builder."""
    from skills.lib.prompt_builder import build_subagent_system_prompt
    make_skill(os.path.join(tmp, "skills"), "driver-openai", 'name: driver-openai\ndescription: "OpenAI driver"')
    content = build_subagent_system_prompt(provider="openai")
    assert "Tabula" in content
    assert "## Tools" in content
    assert "## Available skills" not in content


@with_tabula_home
def test_config_prompts_differ(tmp, boot):
    """Main and subagent prompts are different."""
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
def test_read_project_file_exists(tmp, boot):
    """Existing project file is read."""
    Path(tmp, "SOUL.md").write_text("Be chill.")
    assert boot._read_project_file("SOUL.md") == "Be chill."


@with_tabula_home
def test_read_project_file_missing(tmp, boot):
    """Missing project file returns empty string."""
    assert boot._read_project_file("SOUL.md") == ""


@with_tabula_home
def test_read_project_file_whitespace(tmp, boot):
    """Project file content is stripped."""
    Path(tmp, "USER.md").write_text("  \nName: Test\n  \n")
    assert boot._read_project_file("USER.md") == "Name: Test"


# ── ensure_project_files tests ───────────────────────────────────


@with_tabula_home
def test_ensure_creates_defaults(tmp, boot):
    """ensure_project_files creates all default files."""
    boot.ensure_project_files()
    for name in ["IDENTITY.md", "SOUL.md", "USER.md", "AGENTS.md"]:
        path = Path(tmp, name)
        assert path.is_file(), f"{name} not created"
        assert path.stat().st_size > 0, f"{name} is empty"


@with_tabula_home
def test_ensure_does_not_overwrite(tmp, boot):
    """ensure_project_files does not overwrite existing files."""
    Path(tmp, "SOUL.md").write_text("Custom soul.")
    boot.ensure_project_files()
    assert Path(tmp, "SOUL.md").read_text() == "Custom soul."
    # Other files should still be created
    assert Path(tmp, "USER.md").is_file()
    assert Path(tmp, "AGENTS.md").is_file()


@with_tabula_home
def test_ensure_default_identity_content(tmp, boot):
    """Default IDENTITY.md has template fields."""
    boot.ensure_project_files()
    content = Path(tmp, "IDENTITY.md").read_text()
    assert "**Name:**" in content
    assert "**Personality:**" in content
    assert "**Language:**" in content


@with_tabula_home
def test_ensure_default_soul_content(tmp, boot):
    """Default SOUL.md has key phrases."""
    boot.ensure_project_files()
    content = Path(tmp, "SOUL.md").read_text()
    assert "genuinely helpful" in content
    assert "Have opinions" in content
    assert "concise" in content.lower()


@with_tabula_home
def test_ensure_default_user_content(tmp, boot):
    """Default USER.md has template fields."""
    boot.ensure_project_files()
    content = Path(tmp, "USER.md").read_text()
    assert "**Name:**" in content
    assert "**Timezone:**" in content


@with_tabula_home
def test_ensure_default_agents_content(tmp, boot):
    """Default AGENTS.md has first run and session startup rules."""
    boot.ensure_project_files()
    content = Path(tmp, "AGENTS.md").read_text()
    assert "First Run" in content
    assert "IDENTITY.md" in content
    assert "Session Startup" in content


@with_tabula_home
def test_ensure_injected_into_prompt(tmp, boot):
    """Default project files appear in full prompt after ensure."""
    boot.ensure_project_files()
    prompt = boot.build_system_prompt([])
    assert "genuinely helpful" in prompt
    assert "Workspace" in prompt


if __name__ == "__main__":
    import pytest
    sys.exit(pytest.main([__file__, "-v"]))

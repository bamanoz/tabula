#!/usr/bin/env python3
"""Shared system prompt assembly for Tabula drivers and subagents."""

from __future__ import annotations

import json
import os
import subprocess
import sys
from datetime import date

from skills.lib.paths import skills_dir as skills_dir_path, templates_dir as templates_dir_path, tabula_home as tabula_home_path
from skills.lib.protocol import (
    DEFAULT_KERNEL_TOOLS,
    TOOL_SHELL_EXEC,
    TOOL_PROCESS_SPAWN,
    TOOL_PROCESS_KILL,
    TOOL_PROCESS_LIST,
)
from skills.lib.provider_selection import resolve_provider


PROJECT_FILES = ["IDENTITY.md", "SOUL.md", "USER.md", "AGENTS.md"]
CACHE_BOUNDARY = "\n<!-- CACHE_BOUNDARY -->\n"
KERNEL_TOOL_LINES = {
    TOOL_SHELL_EXEC: "**shell_exec** — run a shell command. Output capped at 16KB.",
    TOOL_PROCESS_SPAWN: "**process_spawn** — start a background process. Returns PID.",
    TOOL_PROCESS_KILL: "**process_kill** — terminate a spawned process by PID.",
    TOOL_PROCESS_LIST: "**process_list** — list spawned processes in the current session, including their `alive=` status.",
}


def current_provider() -> str:
    return resolve_provider(os.environ.get("TABULA_PROVIDER"), tabula_home=tabula_home(), require_ready=False)


def tabula_home() -> str:
    return str(tabula_home_path())


def skills_dir() -> str:
    return str(skills_dir_path())


def templates_dir() -> str:
    return str(templates_dir_path())


def kernel_tools() -> list[str]:
    raw = os.environ.get("TABULA_KERNEL_TOOLS", "")
    if not raw.strip():
        return list(DEFAULT_KERNEL_TOOLS)
    tools = []
    for item in raw.split(","):
        name = item.strip()
        if name in KERNEL_TOOL_LINES and name not in tools:
            tools.append(name)
    return tools


def memory_file() -> str:
    return os.path.join(tabula_home(), "data", "memory", "MEMORY.md")


def mcp_config_file() -> str:
    return os.path.join(tabula_home(), "config", "skills", "mcp", "servers.json")

if sys.platform == "win32":
    def venv_python() -> str:
        return os.path.join(tabula_home(), ".venv", "Scripts", "python.exe")
else:
    def venv_python() -> str:
        return os.path.join(tabula_home(), ".venv", "bin", "python3")


def include_skill(name: str, provider: str | None = None) -> bool:
    provider = provider or current_provider()
    if name.startswith("driver-"):
        return name == f"driver-{provider}"
    if name.startswith("subagent-"):
        return name == f"subagent-{provider}"
    return True


def parse_skill_md(text: str) -> tuple[dict, str]:
    if not text.startswith("---"):
        return {}, text
    end = text.find("\n---", 3)
    if end == -1:
        return {}, text
    front = text[3:end].strip()
    body = text[end + 4 :].strip()
    entries: list[tuple[str, str]] = []
    for line in front.split("\n"):
        stripped = line.strip()
        if not stripped:
            continue
        if line[0] in (" ", "\t") or ":" not in stripped:
            if entries:
                k, v = entries[-1]
                entries[-1] = (k, v + "\n" + stripped)
            continue
        key, _, value = stripped.partition(":")
        entries.append((key.strip(), value.strip()))
    meta = {}
    for key, raw in entries:
        raw = raw.strip()
        if raw and raw[0] in ("[", "{"):
            try:
                meta[key] = json.loads(raw)
            except json.JSONDecodeError:
                meta[key] = raw
        else:
            val = raw.strip('"').strip("'")
            if val.startswith((">\n", "|\n")):
                val = val[2:]
            elif val in (">", "|"):
                val = ""
            meta[key] = val.strip()
    return meta, body


def walk_skills(provider: str | None = None) -> list[tuple[str, str]]:
    results = []
    root_dir = skills_dir()
    if not os.path.isdir(root_dir):
        return results
    for root, dirs, files in os.walk(root_dir, followlinks=True):
        dirs[:] = [d for d in sorted(dirs) if not d.startswith((".", "__"))]
        if "SKILL.md" in files:
            rel = os.path.relpath(root, root_dir)
            leaf = os.path.basename(root)
            if not include_skill(leaf, provider=provider):
                continue
            results.append((rel, os.path.join(root, "SKILL.md")))
    return results


def scan_skills(provider: str | None = None) -> list[str]:
    skills = []
    for rel_path, skill_md in walk_skills(provider=provider):
        with open(skill_md) as f:
            raw = f.read().strip()
        meta, _ = parse_skill_md(raw)
        description = meta.get("description", "")
        if not description:
            continue
        skill_name = meta.get("name", os.path.basename(rel_path))
        skills.append(f"**{skill_name}**: {description}")
    return skills


def _read_template(name: str) -> str:
    if name == "TOOLS.md":
        return _render_tools_template()
    path = os.path.join(templates_dir(), name)
    with open(path) as f:
        return f.read().strip()


def _render_tools_template() -> str:
    lines = ["## Tools", ""]
    enabled = kernel_tools()
    for tool in enabled:
        line = KERNEL_TOOL_LINES.get(tool)
        if line:
            lines.append(line)
    if enabled:
        lines.append("")
    if TOOL_SHELL_EXEC in enabled:
        lines.append("Use shell_exec for quick commands (CLI scripts, cat, ls, python3 skills/...). process_spawn is only for long-running daemons (gateways, servers, watchers).")
        if TOOL_PROCESS_SPAWN in enabled:
            lines.append("NEVER use process_spawn for a command that exits immediately — that's what shell_exec is for.")
        lines.append("If shell_exec is blocked by a hook, do NOT silently fall back to process_spawn — tell the user the command was denied.")
        lines.append("To learn about a skill: shell_exec cat skills/<name>/SKILL.md")
        lines.append("To discover skills: shell_exec ls skills/")
    elif TOOL_PROCESS_SPAWN in enabled:
        lines.append("process_spawn is available only for long-running daemons or background workers.")
    return "\n".join(lines).strip()


def _read_project_file(name: str) -> str:
    path = os.path.join(tabula_home(), name)
    if not os.path.isfile(path):
        return ""
    with open(path) as f:
        return f.read().strip()


def ensure_project_files():
    for name in PROJECT_FILES:
        dest = os.path.join(tabula_home(), name)
        if os.path.exists(dest):
            continue
        src = os.path.join(templates_dir(), name)
        if not os.path.isfile(src):
            continue
        with open(src) as f:
            content = f.read()
        with open(dest, "x") as f:
            f.write(content)


def _section_project_files(subagent: bool = False) -> str:
    files = [("AGENTS.md", "Workspace instructions")] if subagent else [
        ("IDENTITY.md", "Identity"),
        ("SOUL.md", "Personality and tone"),
        ("USER.md", "User context"),
        ("AGENTS.md", "Workspace instructions"),
    ]
    parts = []
    for filename, label in files:
        content = _read_project_file(filename)
        if content:
            parts.append(f"## {label} ({filename})\n\n{content}")
    return "\n\n".join(parts)


def _section_skills(skills: list[str]) -> str:
    if not skills:
        return "No skills are currently available."
    lines = ["## Available skills", ""]
    for doc in skills:
        lines.append(doc)
        lines.append("")
    return "\n".join(lines)


def _section_memory() -> str:
    mem_path = memory_file()
    if not os.path.isfile(mem_path):
        return ""
    with open(mem_path) as f:
        memory = f.read().strip()
    if not memory:
        return ""
    return "\n".join([
        "## Long-term memory",
        "",
        "The following is your persistent memory. Use it to inform your responses.",
        "To save new memories, use the memory skill commands above.",
        "",
        memory,
    ])


def _section_environment(provider: str) -> str:
    return "\n".join([
        "## Environment",
        "",
        f"- Provider: {provider}",
        f"- Date: {date.today().isoformat()}",
        f"- Working directory: {tabula_home()}",
    ])


def _is_first_run() -> bool:
    identity = _read_project_file("IDENTITY.md")
    user = _read_project_file("USER.md")
    if not identity:
        return True
    placeholders = [
        "_(pick something",
        "_(sharp? warm? chaotic? calm? snarky? helpful?)_",
        "_(what language to respond in by default)_",
        "_(What do they care about? What projects are they working on?",
    ]
    return any(token in identity for token in placeholders[:3]) or placeholders[3] in user


FIRST_RUN_INSTRUCTION = (
    "IMPORTANT: This is your first conversation. Your identity is not configured yet. "
    "Before doing anything else, greet the user and start a conversation to set up together:\n"
    "1. Ask what they'd like to call you and what vibe they want\n"
    "2. Ask about them — name, timezone, what they're working on\n"
    "3. Update IDENTITY.md, USER.md, and SOUL.md using the available command-execution tool based on what you agree on\n"
    "Do NOT fill these files silently. Do NOT skip this. Start with a greeting and questions."
)


def discover_mcp_tools() -> dict[str, list[dict]]:
    if os.environ.get("TABULA_SKIP_MCP"):
        return {}
    mcp_cfg = mcp_config_file()
    if not os.path.isfile(mcp_cfg):
        return {}
    mcp_script = os.path.join(skills_dir(), "mcp", "run.py")
    if not os.path.isfile(mcp_script):
        return {}
    try:
        result = subprocess.run([venv_python(), mcp_script, "discover"], capture_output=True, text=True, timeout=30)
        if result.returncode != 0:
            print(f"warning: MCP discovery failed: {result.stderr.strip()}", file=sys.stderr)
            return {}
        return json.loads(result.stdout)
    except Exception as e:
        print(f"warning: MCP discovery failed: {e}", file=sys.stderr)
        return {}


def format_mcp_tools(tools_by_server: dict[str, list[dict]]) -> str:
    if not tools_by_server:
        return ""
    parts = ["## MCP tools", "", "The following MCP tools are available:", ""]
    for server_name in sorted(tools_by_server.keys()):
        parts.append(f"### {server_name}")
        parts.append("")
        for tool in tools_by_server[server_name]:
            parts.append(f"- {tool['name']}: {tool.get('description', '')}")
        parts.append("")
    return "\n".join(parts).rstrip()


def build_main_system_prompt(
    provider: str | None = None,
    *,
    skills: list[str] | None = None,
    mcp_tools: dict[str, list[dict]] | None = None,
) -> str:
    ensure_project_files()
    provider = provider or current_provider()
    skills = scan_skills(provider=provider) if skills is None else skills
    mcp_tools = discover_mcp_tools() if mcp_tools is None else mcp_tools
    static = [_read_template("SYSTEM.md")]
    if _is_first_run():
        static.append(FIRST_RUN_INSTRUCTION)
    static.extend([
        _read_template("TOOLS.md"),
        _read_template("GUIDELINES.md"),
        _read_template("SAFETY.md"),
    ])
    project = _section_project_files(subagent=False)
    if project:
        static.append(project)
    dynamic = [_section_skills(skills)]
    memory = _section_memory()
    if memory:
        dynamic.append(memory)
    if mcp_tools:
        dynamic.append(format_mcp_tools(mcp_tools))
    dynamic.append(_section_environment(provider))
    return "\n\n".join(static) + CACHE_BOUNDARY + "\n\n".join(dynamic)


def build_subagent_system_prompt(provider: str | None = None) -> str:
    ensure_project_files()
    provider = provider or current_provider()
    sections = [
        _read_template("SYSTEM.md"),
        _read_template("TOOLS.md"),
        _read_template("GUIDELINES.md"),
        _read_template("SAFETY.md"),
    ]
    project = _section_project_files(subagent=True)
    if project:
        sections.append(project)
    sections.append(_section_environment(provider))
    return "\n\n".join(sections)

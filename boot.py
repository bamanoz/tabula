#!/usr/bin/env python3
"""
Tabula boot script.

Scans skills, assembles system prompt, injects long-term memory,
and outputs full kernel config as JSON to stdout.

Contract: stdout must be a single JSON object with:
  - system_prompt: string
  - spawn: list of commands to start
  - tools: list of skill tool definitions
  - commands: list of slash commands
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
from datetime import date

TABULA_HOME = os.environ.get("TABULA_HOME", os.path.join(os.path.expanduser("~"), ".tabula"))
SKILLS_DIR = os.path.join(TABULA_HOME, "skills")
MEMORY_FILE = os.path.join(TABULA_HOME, "memory", "MEMORY.md")
TEMPLATES_DIR = os.path.join(os.path.dirname(__file__), "templates")
PROJECT_FILES = ["IDENTITY.md", "SOUL.md", "USER.md", "AGENTS.md"]
CACHE_BOUNDARY = "\n<!-- CACHE_BOUNDARY -->\n"
if sys.platform == "win32":
    VENV_PYTHON = os.path.join(TABULA_HOME, ".venv", "Scripts", "python.exe")
else:
    VENV_PYTHON = os.path.join(TABULA_HOME, ".venv", "bin", "python3")
TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
PERMISSIONS_FILE = os.path.join(TABULA_HOME, "permissions.json")
TABULA_PROVIDER = os.environ.get("TABULA_PROVIDER", "anthropic").strip().lower() or "anthropic"
PROVIDER_ALIASES = {
    "anthropic": "anthropic",
    "claude": "anthropic",
    "openai": "openai",
    "gpt": "openai",
    "openclaw": "openai",
    "mock": "mock",
}


def available_providers() -> list[str]:
    providers = []
    if not os.path.isdir(SKILLS_DIR):
        return providers
    for root, dirs, files in os.walk(SKILLS_DIR):
        dirs[:] = [d for d in sorted(dirs) if not d.startswith((".", "__"))]
        name = os.path.basename(root)
        if not name.startswith("driver-"):
            continue
        provider = name[len("driver-"):]
        if "run.py" in files and provider not in providers:
            providers.append(provider)
    return providers


def resolve_provider() -> str:
    requested = PROVIDER_ALIASES.get(TABULA_PROVIDER)
    if not requested:
        raise SystemExit(f"Unknown TABULA_PROVIDER={TABULA_PROVIDER!r}. Use one of: anthropic, openai.")

    providers = available_providers()
    if requested in providers:
        return requested

    fallback_order = ["anthropic", "openai"]
    for provider in fallback_order:
        if provider in providers:
            print(
                f"warning: provider {requested!r} is unavailable, falling back to {provider!r}",
                file=sys.stderr,
            )
            return provider

    raise SystemExit("No LLM provider skills found. Expected skills/driver-anthropic or skills/driver-openai.")


ACTIVE_PROVIDER = resolve_provider()


def include_skill(name: str) -> bool:
    if name.startswith("driver-"):
        return name == f"driver-{ACTIVE_PROVIDER}"
    if name.startswith("subagent-"):
        return name == f"subagent-{ACTIVE_PROVIDER}"
    return True


def parse_skill_md(text: str) -> tuple[dict, str]:
    """Parse optional YAML frontmatter from SKILL.md.
    Returns (metadata dict, body text).

    Supports multi-line values: lines that start with whitespace or don't
    contain a top-level ':' are appended to the previous key's value.
    Values that look like JSON (start with '[' or '{') are parsed as JSON.
    Other values have surrounding quotes stripped.
    """
    if not text.startswith("---"):
        return {}, text
    end = text.find("\n---", 3)
    if end == -1:
        return {}, text
    front = text[3:end].strip()
    body = text[end + 4:].strip()

    # Collect key-value pairs, joining continuation lines.
    entries: list[tuple[str, str]] = []
    for line in front.split("\n"):
        stripped = line.strip()
        if not stripped:
            continue
        # Continuation line: starts with whitespace or has no bare ':'
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
            # Handle YAML block scalar indicators (> and |)
            if val.startswith((">\n", "|\n")):
                val = val[2:]
            elif val in (">", "|"):
                val = ""
            meta[key] = val.strip()
    return meta, body


def walk_skills() -> list[tuple[str, str]]:
    """Walk SKILLS_DIR recursively, yield (rel_path, SKILL.md abs path) for each skill.

    A skill is any directory containing SKILL.md. rel_path is relative to SKILLS_DIR
    (e.g. "weather", "caveman/caveman-compress"). Follows symlinks (used for bundles).
    """
    results = []
    if not os.path.isdir(SKILLS_DIR):
        return results
    for root, dirs, files in os.walk(SKILLS_DIR, followlinks=True):
        dirs[:] = [d for d in sorted(dirs) if not d.startswith((".", "__"))]
        if "SKILL.md" in files:
            rel = os.path.relpath(root, SKILLS_DIR)
            # Filter by provider (use the last path component as skill name)
            leaf = os.path.basename(root)
            if not include_skill(leaf):
                continue
            results.append((rel, os.path.join(root, "SKILL.md")))
    return results


def scan_skills() -> list[str]:
    """Read SKILL.md from each skill directory (recursive).

    Hidden skills (not injected into system prompt) are those without
    a description field.
    """
    skills = []
    for rel_path, skill_md in walk_skills():
        with open(skill_md) as f:
            raw = f.read().strip()
        meta, body = parse_skill_md(raw)

        description = meta.get("description", "")
        if not description:
            continue

        skill_name = meta.get("name", os.path.basename(rel_path))
        skills.append(f"**{skill_name}**: {description}")
    return skills


KERNEL_TOOLS = {"EXEC", "SPAWN", "KILL", "LIST"}


def discover_skill_tools() -> list[dict]:
    """Scan SKILL.md frontmatter for tool definitions (recursive).

    Returns tools in kernel format, with an added 'exec' field for dispatch.
    """
    tools = []
    seen = set()
    for rel_path, skill_md in walk_skills():
        with open(skill_md) as f:
            raw = f.read().strip()
        meta, _ = parse_skill_md(raw)
        skill_tools = meta.get("tools")
        if not skill_tools or not isinstance(skill_tools, list):
            continue
        for tool in skill_tools:
            tool_name = tool.get("name", "")
            if not tool_name:
                continue
            if tool_name in KERNEL_TOOLS:
                print(f"warning: tool {tool_name!r} in skill {rel_path!r} collides with kernel tool, skipping", file=sys.stderr)
                continue
            if tool_name in seen:
                # Duplicate tool — keep the first one, skip the duplicate.
                # This is a warning, not an error — boot continues.
                continue
            seen.add(tool_name)
            if "exec" not in tool:
                tool["exec"] = f"{VENV_PYTHON} skills/{rel_path}/run.py tool {tool_name}"
            tools.append(tool)
    return tools


def discover_slash_commands() -> list[dict]:
    """Scan SKILL.md for user-invocable skills (recursive).

    Returns list of {"name", "description", "body"} for gateway slash commands.
    Only includes skills with explicit `user-invocable: true` in frontmatter.
    """
    commands = []
    for rel_path, skill_md in walk_skills():
        with open(skill_md) as f:
            raw = f.read().strip()
        meta, body = parse_skill_md(raw)

        ui = meta.get("user-invocable", "")
        if ui.lower() not in ("true", "yes", "1"):
            continue

        skill_name = meta.get("name", os.path.basename(rel_path))
        description = meta.get("description", "")
        commands.append({
            "name": skill_name,
            "description": description,
            "body": body,
        })
    return commands


MCP_CONFIG = os.path.join(TABULA_HOME, "mcp", "servers.json")


def load_permissions() -> list[dict]:
    """Load permission rules from ~/.tabula/permissions.json."""
    if not os.path.isfile(PERMISSIONS_FILE):
        return []
    try:
        with open(PERMISSIONS_FILE, encoding="utf-8") as f:
            data = json.load(f)
        rules = data.get("rules", []) if isinstance(data, dict) else []
        return [r for r in rules if isinstance(r, dict) and "tool" in r and "effect" in r]
    except Exception as e:
        print(f"warning: failed to parse {PERMISSIONS_FILE}: {e}", file=sys.stderr)
        return []


def filter_denied_tools(tools: list[dict], permissions: list[dict]) -> list[dict]:
    """Remove tools that are unconditionally denied by permissions.

    Only filters tools matching a deny rule with no command pattern (fully denied).
    Tools with conditional deny (command-based) are kept since they're partially allowed.
    """
    if not permissions:
        return tools
    from fnmatch import fnmatch

    denied_patterns = [
        r["tool"] for r in permissions
        if r["effect"] == "deny" and not r.get("command")
    ]
    if not denied_patterns:
        return tools

    def is_denied(tool_name: str) -> bool:
        return any(fnmatch(tool_name, pat) for pat in denied_patterns)

    return [t for t in tools if not is_denied(t.get("name", ""))]


def discover_mcp_tools() -> dict[str, list[dict]]:
    """Run MCP discover to get tools from all configured servers."""
    if not os.path.isfile(MCP_CONFIG):
        return {}
    mcp_script = os.path.join(SKILLS_DIR, "mcp", "run.py")
    if not os.path.isfile(mcp_script):
        return {}
    try:
        result = subprocess.run(
            [VENV_PYTHON, mcp_script, "discover"],
            capture_output=True, text=True, timeout=30,
            env={**os.environ, "TABULA_HOME": TABULA_HOME},
        )
        if result.returncode == 0 and result.stdout.strip():
            return json.loads(result.stdout)
    except (Exception, KeyboardInterrupt) as e:
        print(f"warning: MCP discover failed: {e}", file=sys.stderr)
    return {}


def format_mcp_tools(tools_by_server: dict[str, list[dict]]) -> str:
    """Format discovered MCP tools for the system prompt."""
    lines = ["## MCP Tools", ""]
    lines.append("To call an MCP tool: `EXEC python3 skills/mcp/run.py call <server> <tool> '<json_args>'`")
    lines.append("")
    for server, tools in sorted(tools_by_server.items()):
        lines.append(f"**{server}** (MCP server):")
        for tool in tools:
            schema = tool.get("inputSchema", {})
            params = schema.get("properties", {})
            param_str = ", ".join(f"{k}: {v.get('type', 'any')}" for k, v in params.items())
            desc = tool.get("description", "")
            lines.append(f"- `{tool['name']}({param_str})` — {desc}")
        lines.append("")
    return "\n".join(lines)


def _read_template(name: str) -> str:
    """Read a template file from the templates directory."""
    path = os.path.join(TEMPLATES_DIR, name)
    with open(path) as f:
        return f.read().strip()


def _read_project_file(name: str) -> str:
    """Read a project file from TABULA_HOME. Returns empty string if not found."""
    path = os.path.join(TABULA_HOME, name)
    if not os.path.isfile(path):
        return ""
    with open(path) as f:
        return f.read().strip()


def ensure_project_files():
    """Create default project files in TABULA_HOME if they don't exist."""
    for name in PROJECT_FILES:
        dest = os.path.join(TABULA_HOME, name)
        if os.path.exists(dest):
            continue
        src = os.path.join(TEMPLATES_DIR, name)
        if not os.path.isfile(src):
            continue
        with open(src) as f:
            content = f.read()
        with open(dest, "x") as f:
            f.write(content)


def _section_project_files(subagent: bool = False) -> str:
    """Read and format project files (IDENTITY.md, SOUL.md, USER.md, AGENTS.md).

    For subagents, only AGENTS.md is included.
    """
    if subagent:
        files = [("AGENTS.md", "Workspace instructions")]
    else:
        files = [
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
    if not os.path.isfile(MEMORY_FILE):
        return ""
    with open(MEMORY_FILE) as f:
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


def _section_environment() -> str:
    return "\n".join([
        "## Environment",
        "",
        f"- Provider: {ACTIVE_PROVIDER}",
        f"- Date: {date.today().isoformat()}",
        f"- Working directory: {TABULA_HOME}",
    ])


def _is_first_run() -> bool:
    """Check if IDENTITY.md has unfilled fields (still has placeholder text)."""
    content = _read_project_file("IDENTITY.md")
    if not content:
        return True
    return "_(pick something" in content or "**Name:**\n" in content


FIRST_RUN_INSTRUCTION = (
    "IMPORTANT: This is your first conversation. Your identity is not configured yet. "
    "Before doing anything else, greet the user and start a conversation to set up together:\n"
    "1. Ask what they'd like to call you and what vibe they want\n"
    "2. Ask about them — name, timezone, what they're working on\n"
    "3. Update IDENTITY.md, USER.md, and SOUL.md via EXEC based on what you agree on\n"
    "Do NOT fill these files silently. Do NOT skip this. Start with a greeting and questions."
)


def build_system_prompt(skills: list[str], mcp_tools: dict[str, list[dict]] | None = None) -> str:
    """Assemble full system prompt for the main driver.

    Structure:
      Static (cacheable): identity, tools, guidelines, safety, project files
      CACHE_BOUNDARY
      Dynamic: skills, memory, MCP tools, environment
    """
    static = [
        _read_template("SYSTEM.md"),
    ]

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

    dynamic.append(_section_environment())

    return "\n\n".join(static) + CACHE_BOUNDARY + "\n\n".join(dynamic)


def build_subagent_prompt() -> str:
    """Assemble a minimal system prompt for subagents.

    Includes: identity, tools, guidelines, safety, AGENTS.md, environment.
    Excludes: SOUL.md, USER.md, skills, memory, MCP tools.
    """
    sections = [
        _read_template("SYSTEM.md"),
        _read_template("TOOLS.md"),
        _read_template("GUIDELINES.md"),
        _read_template("SAFETY.md"),
    ]

    project = _section_project_files(subagent=True)
    if project:
        sections.append(project)

    sections.append(_section_environment())

    return "\n\n".join(sections)


def has_crontab() -> bool:
    """Check if OS crontab is available."""
    try:
        result = subprocess.run(["crontab", "-l"], capture_output=True, text=True)
        return result.returncode == 0 or "no crontab" in result.stderr.lower()
    except FileNotFoundError:
        return False


def build_spawn() -> list[str]:
    """Determine which processes to spawn."""
    procs = []
    # Spawn cron daemon only when OS crontab is unavailable
    cron_skill = os.path.join(SKILLS_DIR, "cron", "run.py")
    if os.path.isfile(cron_skill) and not has_crontab():
        procs.append(f"{VENV_PYTHON} skills/cron/run.py daemon")
    # Spawn MCP pool when MCP servers are configured
    mcp_skill = os.path.join(SKILLS_DIR, "mcp", "run.py")
    if os.path.isfile(mcp_skill) and os.path.isfile(MCP_CONFIG):
        procs.append(f"{VENV_PYTHON} skills/mcp/run.py pool")
    # Spawn session registry daemon
    sessions_skill = os.path.join(SKILLS_DIR, "sessions", "run.py")
    if os.path.isfile(sessions_skill):
        procs.append(f"{VENV_PYTHON} skills/sessions/run.py daemon")
    # Spawn hook skills (search recursively, follows symlinks for bundles)
    for rel_path, skill_md in walk_skills():
        leaf = os.path.basename(rel_path)
        if not leaf.startswith("hook-"):
            continue
        # hook-permissions only spawns when permissions.json exists
        if leaf == "hook-permissions" and not os.path.isfile(PERMISSIONS_FILE):
            continue
        run_py = os.path.join(SKILLS_DIR, rel_path, "run.py")
        if os.path.isfile(run_py):
            procs.append(f"{VENV_PYTHON} skills/{rel_path}/run.py")
    return procs


def main():
    ensure_project_files()
    skills = scan_skills()
    mcp_tools = discover_mcp_tools()
    skill_tools = discover_skill_tools()
    slash_commands = discover_slash_commands()
    permissions = load_permissions()
    if permissions:
        skill_tools = filter_denied_tools(skill_tools, permissions)
    config = {
        "url": TABULA_URL,
        "system_prompt": build_system_prompt(skills, mcp_tools),
        "spawn": build_spawn(),
        "tools": skill_tools,
        "commands": slash_commands,
    }

    # Write subagent prompt to file for subagent skills to read
    subagent_prompt_path = os.path.join(TABULA_HOME, ".subagent_prompt")
    with open(subagent_prompt_path, "w") as f:
        f.write(build_subagent_prompt())

    json.dump(config, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        sys.exit(1)

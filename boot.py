#!/usr/bin/env python3
"""
Tabula boot script.

Scans skills, assembles system prompt, injects long-term memory,
and outputs full kernel config as JSON to stdout.

Contract: stdout must be a single JSON object with:
  - system_prompt: string
  - tools: list (optional, reserved)
  - spawn: list of commands to start
"""

import json
import os
import subprocess
import sys

TABULA_HOME = os.environ.get("TABULA_HOME", os.path.join(os.path.expanduser("~"), ".tabula"))
SKILLS_DIR = os.path.join(TABULA_HOME, "skills")
MEMORY_FILE = os.path.join(TABULA_HOME, "memory", "MEMORY.md")
if sys.platform == "win32":
    VENV_PYTHON = os.path.join(TABULA_HOME, ".venv", "Scripts", "python.exe")
else:
    VENV_PYTHON = os.path.join(TABULA_HOME, ".venv", "bin", "python3")
TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
TABULA_PROVIDER = os.environ.get("TABULA_PROVIDER", "anthropic").strip().lower() or "anthropic"
PROVIDER_ALIASES = {
    "anthropic": "anthropic",
    "claude": "anthropic",
    "openai": "openai",
    "gpt": "openai",
    "mock": "mock",
}
PROVIDER_API_KEYS = {
    "anthropic": "ANTHROPIC_API_KEY",
    "openai": "OPENAI_API_KEY",
}


def available_providers() -> list[str]:
    providers = []
    if not os.path.isdir(SKILLS_DIR):
        return providers
    for name in sorted(os.listdir(SKILLS_DIR)):
        if not name.startswith("llm-"):
            continue
        provider = name[len("llm-"):]
        if os.path.isfile(os.path.join(SKILLS_DIR, name, "run.py")):
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

    raise SystemExit("No LLM provider skills found. Expected skills/llm-anthropic or skills/llm-openai.")


ACTIVE_PROVIDER = resolve_provider()


def include_skill(name: str) -> bool:
    if name.startswith("llm-"):
        return name == f"llm-{ACTIVE_PROVIDER}"
    if name.startswith("subagent-"):
        return name == f"subagent-{ACTIVE_PROVIDER}"
    return True


def parse_skill_md(text: str) -> tuple[dict, str]:
    """Parse optional YAML frontmatter from SKILL.md.
    Returns (metadata dict, body text)."""
    if not text.startswith("---"):
        return {}, text
    end = text.find("\n---", 3)
    if end == -1:
        return {}, text
    front = text[3:end].strip()
    body = text[end + 4:].strip()
    meta = {}
    for line in front.split("\n"):
        if ":" in line:
            key, _, value = line.partition(":")
            meta[key.strip()] = value.strip().strip('"').strip("'")
    return meta, body


def scan_skills() -> list[str]:
    """Read SKILL.md from each skill subdirectory.

    Frontmatter format (OpenClaw-compatible):
      name: skill-name
      description: "short description"

    Hidden skills (not injected into system prompt) are marked via:
      metadata: { "tabula": { "inject": "none" } }
    If no description, the full body is injected.
    """
    skills = []
    if not os.path.isdir(SKILLS_DIR):
        return skills
    for name in sorted(os.listdir(SKILLS_DIR)):
        if not include_skill(name):
            continue
        skill_md = os.path.join(SKILLS_DIR, name, "SKILL.md")
        if not os.path.isfile(skill_md):
            continue
        with open(skill_md) as f:
            raw = f.read().strip()
        meta, body = parse_skill_md(raw)

        # Hidden skills (internal drivers, gateways, etc.)
        if meta.get("inject") == "none":
            continue

        description = meta.get("description", "")
        if description:
            skill_name = meta.get("name", name)
            skills.append(f"**{skill_name}**: {description}")
        else:
            skills.append(body)
    return skills


MCP_CONFIG = os.path.join(TABULA_HOME, "mcp", "servers.json")


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
    except Exception as e:
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


def build_system_prompt(skills: list[str], mcp_tools: dict[str, list[dict]] | None = None) -> str:
    """Assemble system prompt from skills and memory."""
    lines = []
    lines.append("You are Tabula, an AI agent.")
    lines.append(f"Active LLM provider: {ACTIVE_PROVIDER}.")
    lines.append("You have kernel tools: SPAWN, EXEC, KILL, LIST. Use them via tool calls.")
    lines.append("When using tools, do not repeat your previous response. Only present the final result once.")
    lines.append("")

    if skills:
        lines.append("## Available skills")
        lines.append("")
        for doc in skills:
            lines.append(doc)
            lines.append("")
    else:
        lines.append("No skills are currently available.")
        lines.append("")

    # Inject long-term memory
    if os.path.isfile(MEMORY_FILE):
        with open(MEMORY_FILE) as f:
            memory = f.read().strip()
        if memory:
            lines.append("## Long-term memory")
            lines.append("")
            lines.append("The following is your persistent memory. Use it to inform your responses.")
            lines.append("To save new memories, use the memory skill commands above.")
            lines.append("")
            lines.append(memory)
            lines.append("")

    # Inject MCP tools
    if mcp_tools:
        lines.append(format_mcp_tools(mcp_tools))

    lines.append("Be helpful, concise, and respond in Russian.")
    return "\n".join(lines)


def has_crontab() -> bool:
    """Check if OS crontab is available."""
    try:
        result = subprocess.run(["crontab", "-l"], capture_output=True, text=True)
        return result.returncode == 0 or "no crontab" in result.stderr.lower()
    except FileNotFoundError:
        return False


def build_spawn() -> list[str]:
    """Determine which processes to spawn."""
    key_env = PROVIDER_API_KEYS.get(ACTIVE_PROVIDER)
    if key_env and not os.environ.get(key_env):
        print(
            f"warning: {key_env} is not set; skills/llm-{ACTIVE_PROVIDER}/run.py may exit on startup",
            file=sys.stderr,
        )
    driver = f"{VENV_PYTHON} skills/llm-{ACTIVE_PROVIDER}/run.py"
    gateway_cmd = f"{VENV_PYTHON} skills/gateway-cli/run.py --driver '{driver}'"
    resume_session = os.environ.get("TABULA_RESUME_SESSION", "")
    if resume_session:
        gateway_cmd += f" --resume {resume_session}"
    procs = [
        gateway_cmd,
    ]
    # Spawn cron daemon only when OS crontab is unavailable
    cron_skill = os.path.join(SKILLS_DIR, "cron", "run.py")
    if os.path.isfile(cron_skill) and not has_crontab():
        procs.append(f"{VENV_PYTHON} skills/cron/run.py daemon")
        print("info: OS crontab unavailable, spawning cron daemon", file=sys.stderr)
    # Spawn MCP pool when MCP servers are configured
    mcp_skill = os.path.join(SKILLS_DIR, "mcp", "run.py")
    if os.path.isfile(mcp_skill) and os.path.isfile(MCP_CONFIG):
        procs.append(f"{VENV_PYTHON} skills/mcp/run.py pool")
        print("info: MCP servers configured, spawning mcp-pool", file=sys.stderr)
    # Spawn session registry daemon
    sessions_skill = os.path.join(SKILLS_DIR, "sessions", "run.py")
    if os.path.isfile(sessions_skill):
        procs.append(f"{VENV_PYTHON} skills/sessions/run.py daemon")
    return procs


def main():
    skills = scan_skills()
    mcp_tools = discover_mcp_tools()
    config = {
        "url": TABULA_URL,
        "system_prompt": build_system_prompt(skills, mcp_tools),
        "spawn": build_spawn(),
    }
    json.dump(config, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()

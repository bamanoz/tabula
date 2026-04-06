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
import sys

TABULA_HOME = os.environ.get("TABULA_HOME", os.path.join(os.path.expanduser("~"), ".tabula"))
SKILLS_DIR = os.path.join(TABULA_HOME, "skills")
MEMORY_FILE = os.path.join(TABULA_HOME, "memory", "MEMORY.md")
VENV_PYTHON = os.path.join(TABULA_HOME, ".venv", "bin", "python3")
TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")


def scan_skills() -> list[str]:
    """Read SKILL.md from each skill subdirectory."""
    skills = []
    if not os.path.isdir(SKILLS_DIR):
        return skills
    for name in sorted(os.listdir(SKILLS_DIR)):
        skill_md = os.path.join(SKILLS_DIR, name, "SKILL.md")
        if os.path.isfile(skill_md):
            with open(skill_md) as f:
                skills.append(f.read().strip())
    return skills


def build_system_prompt(skills: list[str]) -> str:
    """Assemble system prompt from skills and memory."""
    lines = []
    lines.append("You are Tabula, an AI agent.")
    lines.append("You have kernel tools: SPAWN, EXEC, KILL, LIST. Use them via tool calls.")
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

    lines.append("Be helpful, concise, and respond in Russian.")
    return "\n".join(lines)


def build_spawn() -> list[str]:
    """Determine which processes to spawn."""
    return [
        f"{VENV_PYTHON} skills/llm-anthropic/run.py",
        f"{VENV_PYTHON} skills/gateway-cli/run.py",
    ]


def main():
    skills = scan_skills()
    config = {
        "url": TABULA_URL,
        "system_prompt": build_system_prompt(skills),
        "spawn": build_spawn(),
    }
    json.dump(config, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()

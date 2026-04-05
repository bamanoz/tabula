#!/usr/bin/env python3
"""
Tabula bootstrap script.

Scans the skills directory, reads SKILL.md from each subdirectory,
and assembles a system prompt for the LLM.

Usage:
    bootstrap.py <skills_dir>

Output (JSON to stdout):
    { "prompt": "<assembled system prompt>" }
"""

import json
import os
import sys


def main():
    if len(sys.argv) < 2:
        print("Usage: bootstrap.py <skills_dir>", file=sys.stderr)
        sys.exit(1)

    skills_dir = sys.argv[1]

    skills = []

    if os.path.isdir(skills_dir):
        for name in sorted(os.listdir(skills_dir)):
            skill_path = os.path.join(skills_dir, name)
            if not os.path.isdir(skill_path):
                continue

            skill_md = os.path.join(skill_path, "SKILL.md")
            if os.path.isfile(skill_md):
                with open(skill_md) as f:
                    content = f.read().strip()
                skills.append(content)

    # Assemble prompt
    lines = []
    lines.append("You are Tabula, an AI agent running on a microkernel.")
    lines.append("You have kernel tools: SPAWN, EXEC, KILL, SEND, LIST, QUERY. Use them via tool calls.")
    lines.append("")

    if skills:
        lines.append("## Available skills")
        lines.append("")
        for skill_doc in skills:
            lines.append(skill_doc)
            lines.append("")
    else:
        lines.append("No skills are currently available.")
        lines.append("")

    lines.append("Use LIST to see running processes. Use QUERY for request-response with non-piped processes.")
    lines.append("")
    lines.append("## On startup")
    lines.append("")
    lines.append("1. SPAWN .venv/bin/python3 skills/gateway-cli/run.py — to accept user input")
    lines.append("2. Wait for user messages and respond via SEND")
    lines.append("3. Be helpful, concise, and respond in Russian.")

    prompt = "\n".join(lines)

    output = {"prompt": prompt}
    json.dump(output, sys.stdout)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()

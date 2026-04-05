#!/usr/bin/env python3
"""
Tabula system prompt assembler.

Scans skills directory, reads SKILL.md from each subdirectory,
and outputs a system prompt as JSON.

Usage:
    system_prompt.py <skills_dir>

Output (JSON to stdout):
    { "prompt": "<assembled system prompt>" }
"""

import json
import os
import sys


def main():
    if len(sys.argv) < 2:
        print("Usage: system_prompt.py <skills_dir>", file=sys.stderr)
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

    lines = []
    lines.append("You are Tabula, an AI agent running on a microkernel.")
    lines.append("You have kernel tools: SPAWN, EXEC, KILL, LIST. Use them via tool calls.")
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

    lines.append("Be helpful, concise, and respond in Russian.")

    prompt = "\n".join(lines)
    json.dump({"prompt": prompt}, sys.stdout)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()

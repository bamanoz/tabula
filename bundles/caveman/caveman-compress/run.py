#!/usr/bin/env python3
"""Thin Tabula wrapper — bridges JSON stdin/stdout to caveman-compress CLI.

The scripts/ directory contains the original caveman-compress package
from https://github.com/JuliusBrussee/caveman — no modifications.
"""

import json
import os
import subprocess
import sys

SKILL_DIR = os.path.dirname(os.path.abspath(__file__))


def handle_tool(tool_name: str):
    if tool_name != "caveman_compress":
        print(json.dumps({"error": f"unknown tool: {tool_name}"}))
        sys.exit(1)

    params = json.loads(sys.stdin.read())
    filepath = params.get("filepath", "")
    if not filepath:
        print(json.dumps({"error": "filepath parameter is required"}))
        sys.exit(1)

    # Call the original caveman-compress CLI: python -m scripts <filepath>
    result = subprocess.run(
        [sys.executable, "-m", "scripts", filepath],
        capture_output=True, text=True,
        cwd=SKILL_DIR,
    )

    output = {
        "stdout": result.stdout.strip(),
        "stderr": result.stderr.strip(),
        "exit_code": result.returncode,
    }
    print(json.dumps(output, ensure_ascii=False))


if __name__ == "__main__":
    if len(sys.argv) >= 3 and sys.argv[1] == "tool":
        handle_tool(sys.argv[2])
    else:
        print(f"Usage: {sys.argv[0]} tool <tool_name>", file=sys.stderr)
        sys.exit(1)

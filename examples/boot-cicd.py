#!/usr/bin/env python3
"""
CI/CD boot script for Tabula.

Minimal boot script that spawns only the LLM driver for non-interactive use.
No session registry, no MCP, no hooks — just the driver.

Usage:
  TABULA_HOME=/path/to/work TABULA_BOOT=python3 boot-cicd.py tabula run --prompt "..."
"""
from __future__ import annotations

import json
import os
import sys

TABULA_HOME = os.environ.get("TABULA_HOME", os.path.join(os.path.expanduser("~"), ".tabula"))
SKILLS_DIR = os.path.join(TABULA_HOME, "skills")
TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")
TABULA_PROVIDER = os.environ.get("TABULA_PROVIDER", "anthropic").strip().lower() or "anthropic"

VENV_PYTHON = os.path.join(TABULA_HOME, ".venv", "bin", "python3")


def find_driver() -> str | None:
    """Find the driver for the configured provider."""
    driver_name = f"driver-{TABULA_PROVIDER}"
    run_py = os.path.join(SKILLS_DIR, driver_name, "run.py")
    if os.path.isfile(run_py):
        return f"{VENV_PYTHON} skills/{driver_name}/run.py --session main"
    return None


def build_system_prompt() -> str:
    """Build a minimal system prompt for CI/CD use."""
    return (
        "You are a helpful assistant. Answer the user's question concisely."
    )


def main():
    driver = find_driver()
    if not driver:
        print(f"error: no driver found for provider {TABULA_PROVIDER!r}", file=sys.stderr)
        sys.exit(1)

    config = {
        "url": TABULA_URL,
        "system_prompt": build_system_prompt(),
        "spawn": [driver],
        "tools": [],
        "commands": [],
    }

    json.dump(config, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        sys.exit(1)

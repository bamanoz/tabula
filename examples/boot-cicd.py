#!/usr/bin/env python3
"""
CI/CD boot script for Tabula.

Minimal boot script that spawns only the LLM driver for non-interactive use.
No session registry, no MCP, no hooks — just the driver.

Usage:
  TABULA_HOME=/path/to/work TABULA_BOOT="python3 boot-cicd.py" tabula run --prompt "..."
"""
from __future__ import annotations

import json
import os
import sys

if TABULA_HOME := os.environ.get("TABULA_HOME"):
    if TABULA_HOME not in sys.path:
        sys.path.insert(0, TABULA_HOME)

TABULA_HOME = os.environ.get("TABULA_HOME", os.path.join(os.path.expanduser("~"), ".tabula"))
if TABULA_HOME not in sys.path:
    sys.path.insert(0, TABULA_HOME)


def load_env() -> None:
    """Load $TABULA_HOME/.env into os.environ without overriding shell vars."""
    env_file = os.path.join(TABULA_HOME, ".env")
    if not os.path.isfile(env_file):
        return
    with open(env_file) as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            key, _, value = line.partition("=")
            if key:
                os.environ.setdefault(key.strip(), value.strip())


load_env()

TABULA_URL = os.environ.get("TABULA_URL", "ws://localhost:8089/ws")

from skills._lib.provider_selection import build_driver_command, resolve_provider

VENV_PYTHON = os.path.join(TABULA_HOME, ".venv", "bin", "python3")


def find_driver() -> str | None:
    """Find the driver for the configured provider."""
    provider = resolve_provider(os.environ.get("TABULA_PROVIDER"), tabula_home=TABULA_HOME, require_ready=False)
    return build_driver_command(provider, tabula_home=TABULA_HOME, python_executable=VENV_PYTHON) + " --session main"


def main():
    driver = find_driver()

    config = {
        "url": TABULA_URL,
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

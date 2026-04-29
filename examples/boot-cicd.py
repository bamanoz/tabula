#!/usr/bin/env python3
"""Minimal CI/CD boot script for non-interactive Tabula runs."""
from __future__ import annotations

import json
import os
import sys

TABULA_HOME = os.environ.get("TABULA_HOME", os.path.join(os.path.expanduser("~"), ".tabula"))
HOME_LIB = os.path.join(TABULA_HOME, "_lib", "python", "src")
for path in (HOME_LIB, TABULA_HOME):
    if path not in sys.path:
        sys.path.insert(0, path)


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

def main():
    config = {
        "url": TABULA_URL,
        "skills": [],
    }

    json.dump(config, sys.stdout, ensure_ascii=False)
    sys.stdout.write("\n")


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        sys.exit(1)

#!/usr/bin/env python3
"""Lightweight diagnostic runner for OpenAI-backed subagent E2E."""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def main():
    proc = subprocess.run([sys.executable, str(ROOT / "tests" / "test_openai_subagent_e2e.py")], cwd=ROOT)
    raise SystemExit(proc.returncode)


if __name__ == "__main__":
    main()

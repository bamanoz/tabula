#!/usr/bin/env python3
"""Diagnostic runner for the mock driver E2E flow."""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def main():
    proc = subprocess.run([sys.executable, str(ROOT / "tests" / "test_mock_driver_e2e.py")], cwd=ROOT)
    raise SystemExit(proc.returncode)


if __name__ == "__main__":
    main()

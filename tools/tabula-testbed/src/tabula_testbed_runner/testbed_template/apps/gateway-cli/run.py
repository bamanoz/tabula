#!/usr/bin/env python3
from __future__ import annotations

import argparse


def main() -> int:
    parser = argparse.ArgumentParser(description="Testbed gateway CLI")
    parser.add_argument("--app")
    parser.add_argument("--kernel")
    parser.parse_args()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

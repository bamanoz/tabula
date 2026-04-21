#!/usr/bin/env python3
"""Compatibility shim: forwards to tabula_distro.cli install.

The real implementation lives in tools/tabula-distro. This script preserves
the old CLI surface used by scripts/install.sh and scripts/install-dev.sh:

    install-distro.py --home <home> <source>

It tries the installed ``tabula-distro`` package first, and falls back to
importing directly from the in-repo package when developing.
"""
from __future__ import annotations

import argparse
import os
import sys
from pathlib import Path


def _load_cli():
    try:
        from tabula_distro import cli  # installed
        return cli
    except ImportError:
        pass
    repo_root = Path(__file__).resolve().parents[1]
    pkg_src = repo_root / "tools" / "tabula-distro" / "src"
    if pkg_src.is_dir():
        sys.path.insert(0, str(pkg_src))
        from tabula_distro import cli  # type: ignore
        return cli
    raise SystemExit(
        "tabula-distro package not installed and in-repo sources not found; "
        "pip install tools/tabula-distro or run from a checkout"
    )


def main() -> int:
    parser = argparse.ArgumentParser(description="Install or switch a Tabula distro (legacy shim)")
    parser.add_argument("source", help="Local distro directory")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula")))
    parser.add_argument("--name", default="", help="Override installed distro name")
    args = parser.parse_args()

    cli = _load_cli()
    forwarded = ["--home", args.home, "install", args.source]
    if args.name:
        forwarded += ["--name", args.name]
    return cli.main(forwarded)


if __name__ == "__main__":
    raise SystemExit(main())

"""CLI entrypoint for ``tabula-install``."""
from __future__ import annotations

from . import cli


def main(argv: list[str] | None = None) -> int:
    return cli.main(argv, prog="tabula-install")

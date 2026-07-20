#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
import shutil
from pathlib import Path


PRESERVED_ROOTS = frozenset({"config"})


def overlay_payload(source: Path, destination: Path) -> None:
    """Overlay an installer payload without replacing existing user config."""
    source = source.resolve()
    destination.mkdir(parents=True, exist_ok=True)

    preserved = {
        name
        for name in PRESERVED_ROOTS
        if os.path.lexists(destination / name)
    }
    for entry in sorted(source.iterdir(), key=lambda item: item.name):
        if entry.name in preserved:
            continue
        _overlay_entry(entry, destination / entry.name)


def _overlay_entry(source: Path, destination: Path) -> None:
    if source.is_dir() and not source.is_symlink():
        if os.path.lexists(destination) and (destination.is_symlink() or not destination.is_dir()):
            _remove(destination)
        shutil.copytree(source, destination, dirs_exist_ok=True, copy_function=shutil.copy2)
        return

    if os.path.lexists(destination):
        _remove(destination)
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(source, destination, follow_symlinks=True)


def _remove(path: Path) -> None:
    if path.is_symlink() or path.is_file():
        path.unlink()
    elif path.is_dir():
        shutil.rmtree(path)


def main() -> int:
    parser = argparse.ArgumentParser(description="Overlay a Tabula release payload")
    parser.add_argument("source", type=Path)
    parser.add_argument("destination", type=Path)
    args = parser.parse_args()
    overlay_payload(args.source, args.destination)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

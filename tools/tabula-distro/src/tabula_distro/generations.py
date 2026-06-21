"""Generations: atomic switch with rollback.

Layout under ``$TABULA_HOME/distrib/<name>/``::

    generations/
      0001-2026-04-21T14-30-00Z/   # full staged tree
      0002-2026-04-21T15-12-44Z/
    current -> generations/0002-...
    distro.lock.json

The legacy layout (no ``generations/`` subdir, distro tree directly in
``distrib/<name>/``) is migrated on first install: existing files are moved
into a synthetic ``generations/0001-legacy/`` and the symlink is created.
"""
from __future__ import annotations

import datetime as _dt
import os
import shutil
from dataclasses import dataclass
from pathlib import Path


GENERATION_PREFIX_WIDTH = 4
LEGACY_MARKER = "legacy"


@dataclass(frozen=True)
class Generation:
    number: int
    name: str
    path: Path

    @property
    def is_legacy(self) -> bool:
        return self.name.endswith(LEGACY_MARKER)


def distro_root(home: Path, distro_name: str) -> Path:
    return home / "distrib" / distro_name


def generations_dir(home: Path, distro_name: str) -> Path:
    return distro_root(home, distro_name) / "generations"


def current_link(home: Path, distro_name: str) -> Path:
    return distro_root(home, distro_name) / "current"


def list_generations(home: Path, distro_name: str) -> list[Generation]:
    gdir = generations_dir(home, distro_name)
    if not gdir.is_dir():
        return []
    out: list[Generation] = []
    for entry in sorted(gdir.iterdir()):
        if not entry.is_dir():
            continue
        prefix, _, _ = entry.name.partition("-")
        try:
            num = int(prefix)
        except ValueError:
            continue
        out.append(Generation(number=num, name=entry.name, path=entry))
    return out


def current_generation(home: Path, distro_name: str) -> Generation | None:
    link = current_link(home, distro_name)
    if not link.exists():
        return None
    target = link.resolve()
    for g in list_generations(home, distro_name):
        if g.path.resolve() == target:
            return g
    return None


def next_generation_name(existing: list[Generation]) -> str:
    next_num = (existing[-1].number + 1) if existing else 1
    ts = _dt.datetime.now(_dt.timezone.utc).strftime("%Y-%m-%dT%H-%M-%SZ")
    return f"{next_num:0{GENERATION_PREFIX_WIDTH}d}-{ts}"


def migrate_legacy(home: Path, distro_name: str) -> Generation | None:
    """If distro tree lives directly in distrib/<name>/, move it into a legacy generation."""
    root = distro_root(home, distro_name)
    if not root.is_dir():
        return None
    if (root / "generations").exists() or (root / "current").exists():
        return None
    has_distro_files = any((root / marker).exists() for marker in ("skills", "templates", "distro.toml"))
    if not has_distro_files:
        return None
    legacy_name = f"{1:0{GENERATION_PREFIX_WIDTH}d}-{LEGACY_MARKER}"
    legacy_path = root / "generations" / legacy_name
    legacy_path.parent.mkdir(parents=True, exist_ok=True)
    for entry in list(root.iterdir()):
        if entry.name in {"generations", "current", "distro.lock.json"}:
            continue
        shutil.move(str(entry), legacy_path / entry.name)
    set_current(home, distro_name, Generation(number=1, name=legacy_name, path=legacy_path))
    return Generation(number=1, name=legacy_name, path=legacy_path)


def set_current(home: Path, distro_name: str, gen: Generation) -> None:
    """Atomically point ``current`` at ``gen``."""
    link = current_link(home, distro_name)
    target = Path("generations") / gen.name
    link.parent.mkdir(parents=True, exist_ok=True)
    tmp = link.with_name(link.name + ".tmp")
    if tmp.exists() or tmp.is_symlink():
        tmp.unlink()
    tmp.symlink_to(target)
    os.replace(tmp, link)


def prune(home: Path, distro_name: str, *, keep: int) -> list[Generation]:
    """Remove old generations, keeping the ``keep`` most recent + the current one."""
    gens = list_generations(home, distro_name)
    if len(gens) <= keep:
        return []
    cur = current_generation(home, distro_name)
    cur_num = cur.number if cur else -1
    to_keep_nums = {g.number for g in gens[-keep:]} | {cur_num}
    removed: list[Generation] = []
    for g in gens:
        if g.number in to_keep_nums:
            continue
        shutil.rmtree(g.path, ignore_errors=True)
        removed.append(g)
    return removed

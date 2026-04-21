from __future__ import annotations

import os
import shutil
from pathlib import Path


def materialize_flat_surface(home: Path, *, source_root: Path) -> None:
    """Expose boot.py, templates/, and skills/* as the flat runtime surface.

    The source of truth remains under ``distrib/familiar``; this helper creates the
    strict flat runtime contract that boot/runtime code expects.
    """
    _ensure_boot_alias(home, source_root=source_root)
    templates_src = _surface_source(source_root, "templates")
    if templates_src is not None:
        _link_children(templates_src, home / "templates")

    skills_src = _surface_source(source_root, "skills")
    if skills_src is not None:
        _link_children(skills_src, home / "skills", preserve={"lib"})


def _ensure_boot_alias(home: Path, *, source_root: Path) -> None:
    boot = home / "boot.py"
    if boot.exists() or boot.is_symlink():
        return
    boot_target = source_root / "distrib" / "familiar" / "boot.py"
    if not boot_target.is_file():
        return
    boot.symlink_to(Path("distrib/familiar/boot.py"))


def _surface_source(source_root: Path, name: str) -> Path | None:
    distro = source_root / "distrib" / "familiar" / name
    if distro.is_dir():
        return distro
    flat = source_root / name
    if flat.is_dir():
        return flat
    return None


def _link_children(src_dir: Path, dst_dir: Path, *, preserve: set[str] | None = None) -> None:
    preserve = preserve or set()
    if src_dir == dst_dir:
        return
    dst_dir.mkdir(parents=True, exist_ok=True)
    for entry in src_dir.iterdir():
        if entry.name in preserve:
            continue
        target = dst_dir / entry.name
        if target.exists() or target.is_symlink():
            if target.is_symlink() or target.is_file():
                target.unlink(missing_ok=True)
            elif target.is_dir():
                shutil.rmtree(target)
        target.symlink_to(Path(os.path.relpath(entry, start=dst_dir)))


def reset_and_materialize_flat_surface(home: Path, *, source_root: Path) -> None:
    """Rebuild flat runtime directories from scratch for temp test homes."""
    boot = home / "boot.py"
    if boot.exists() or boot.is_symlink():
        boot.unlink(missing_ok=True)
    templates = home / "templates"
    if templates.exists() or templates.is_symlink():
        shutil.rmtree(templates, ignore_errors=True)
    skills = home / "skills"
    skills.mkdir(parents=True, exist_ok=True)
    for entry in list(skills.iterdir()):
        if entry.name == "lib":
            continue
        if entry.is_dir() and not entry.is_symlink():
            shutil.rmtree(entry)
        else:
            entry.unlink(missing_ok=True)
    materialize_flat_surface(home, source_root=source_root)

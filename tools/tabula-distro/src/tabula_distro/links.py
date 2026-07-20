"""Helpers for filesystem references across Unix symlinks and Windows junctions."""
from __future__ import annotations

import os
import shutil
import subprocess
from pathlib import Path


def is_junction(path: Path) -> bool:
    checker = getattr(os.path, "isjunction", None)
    if checker is None:
        return False
    try:
        return bool(checker(path))
    except OSError:
        return False


def is_reference(path: Path) -> bool:
    return path.is_symlink() or is_junction(path)


def remove_path(path: Path) -> None:
    if not _path_exists(path) and not is_reference(path):
        return
    if is_junction(path):
        path.rmdir()
        return
    if path.is_symlink():
        path.unlink()
        return
    if path.is_dir():
        shutil.rmtree(path)
        return
    path.unlink()


def resolve_reference(path: Path) -> Path | None:
    if path.is_symlink():
        target = Path(os.readlink(path))
        if not target.is_absolute():
            target = path.parent / target
        return target.resolve()
    if is_junction(path):
        try:
            return path.resolve(strict=True)
        except OSError:
            return None
    return None


def create_directory_reference(link: Path, target: Path) -> None:
    _create_reference(link, target, directory=True)


def create_file_reference(link: Path, target: Path) -> None:
    _create_reference(link, target, directory=False)


def replace_directory_reference(link: Path, target: Path) -> None:
    link.parent.mkdir(parents=True, exist_ok=True)
    if os.name != "nt":
        tmp = link.with_name(link.name + ".tmp")
        remove_path(tmp)
        tmp.symlink_to(target, target_is_directory=True)
        os.replace(tmp, link)
        return
    remove_path(link)
    create_directory_reference(link, target)


def _create_reference(link: Path, target: Path, *, directory: bool) -> None:
    link.parent.mkdir(parents=True, exist_ok=True)
    if _path_exists(link) or is_reference(link):
        remove_path(link)
    try:
        link.symlink_to(target, target_is_directory=directory)
        return
    except OSError as exc:
        if os.name != "nt" or not _should_fallback(exc):
            raise
    absolute_target = _absolute_target(link, target)
    if directory:
        _create_junction(link, absolute_target)
        return
    try:
        os.link(absolute_target, link)
    except OSError:
        shutil.copy2(absolute_target, link)


def _should_fallback(exc: OSError) -> bool:
    return getattr(exc, "winerror", None) in {5, 1314}


def _absolute_target(link: Path, target: Path) -> Path:
    if target.is_absolute():
        return target.resolve()
    return (link.parent / target).resolve()


def _create_junction(link: Path, target: Path) -> None:
    result = subprocess.run(
        ["cmd", "/c", "mklink", "/J", str(link), str(target)],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        stderr = (result.stderr or result.stdout or "").strip()
        raise OSError(f"mklink /J failed for {link} -> {target}: {stderr}")


def _path_exists(path: Path) -> bool:
    return os.path.lexists(path)

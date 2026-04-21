#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
import re
import shutil
import sys
import tarfile
import tempfile
import urllib.request
from pathlib import Path


IGNORE_NAMES = {"__pycache__", ".pytest_cache"}


def should_ignore_name(name: str) -> bool:
    return name in IGNORE_NAMES or name.endswith(".pyc")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Install or switch a Tabula distro")
    parser.add_argument("source", help="Local distro directory or GitHub tree URL")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula")))
    parser.add_argument("--name", default="", help="Override installed distro name")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    home = Path(args.home).expanduser().resolve()
    home.mkdir(parents=True, exist_ok=True)

    with resolve_source(args.source) as source_dir:
        validate_distro_dir(source_dir)
        distro_name = args.name or source_dir.name
        install_distro(home, source_dir, distro_name)
    return 0


class SourceContext:
    def __init__(self, path: Path, cleanup_dir: str | None = None):
        self.path = path
        self.cleanup_dir = cleanup_dir

    def __enter__(self) -> Path:
        return self.path

    def __exit__(self, exc_type, exc, tb):
        if self.cleanup_dir:
            shutil.rmtree(self.cleanup_dir, ignore_errors=True)
        return False


def resolve_source(source: str) -> SourceContext:
    local = Path(source).expanduser()
    if local.is_dir():
        return SourceContext(local.resolve())

    match = re.match(r"^https://github\.com/([^/]+)/([^/]+)/tree/([^/]+)/(.+)$", source)
    if not match:
        raise SystemExit(f"unsupported distro source: {source}")

    owner, repo, ref, subpath = match.groups()
    temp_dir = tempfile.mkdtemp(prefix="tabula-distro-")
    archive_path = Path(temp_dir) / "repo.tar.gz"
    url = f"https://codeload.github.com/{owner}/{repo}/tar.gz/{ref}"
    urllib.request.urlretrieve(url, archive_path)
    with tarfile.open(archive_path, "r:gz") as tf:
        tf.extractall(temp_dir)
    extracted = next(p for p in Path(temp_dir).iterdir() if p.is_dir() and p.name != archive_path.name)
    distro_dir = extracted / subpath
    return SourceContext(distro_dir, cleanup_dir=temp_dir)


def validate_distro_dir(path: Path) -> None:
    missing = [name for name in ("boot.py", "templates", "skills") if not (path / name).exists()]
    if missing:
        raise SystemExit(f"invalid distro at {path}: missing {', '.join(missing)}")


def install_distro(home: Path, source_dir: Path, distro_name: str) -> None:
    distrib_root = home / "distrib"
    distrib_root.mkdir(parents=True, exist_ok=True)
    target_dir = distrib_root / distro_name
    if target_dir.exists() or target_dir.is_symlink():
        if target_dir.is_dir() and not target_dir.is_symlink():
            shutil.rmtree(target_dir)
        else:
            target_dir.unlink()
    copytree_filtered(source_dir, target_dir)
    materialize_linked_skills(source_dir, target_dir)
    set_active_distro(home, distro_name)


def copytree_filtered(src: Path, dst: Path) -> None:
    def _ignore(_dir: str, names: list[str]) -> set[str]:
        ignored = {name for name in names if should_ignore_name(name)}
        return ignored

    shutil.copytree(src, dst, symlinks=True, ignore=_ignore)


def bundle_root_for_skill(path: Path) -> Path | None:
    if "bundles" not in path.parts:
        return None
    parts = path.parts
    idx = parts.index("bundles")
    if idx + 1 >= len(parts):
        return None
    return Path(*parts[: idx + 2])


def copy_bundle_support_files(bundle_root: Path, target_skill_dir: Path) -> None:
    for entry in bundle_root.iterdir():
        if entry.is_dir():
            continue
        if should_ignore_name(entry.name) or entry.name == "README.md":
            continue
        shutil.copy2(entry, target_skill_dir / entry.name)


def materialize_linked_skills(source_dir: Path, target_dir: Path) -> None:
    source_skills_dir = source_dir / "skills"
    target_skills_dir = target_dir / "skills"
    if not source_skills_dir.is_dir() or not target_skills_dir.is_dir():
        return
    for entry in source_skills_dir.iterdir():
        if not entry.is_symlink():
            continue
        resolved = entry.resolve(strict=True)
        bundle_root = bundle_root_for_skill(resolved)
        if bundle_root is None:
            continue
        target = target_skills_dir / entry.name
        if target.exists() or target.is_symlink():
            if target.is_dir() and not target.is_symlink():
                shutil.rmtree(target)
            else:
                target.unlink()
        copytree_filtered(resolved, target)
        copy_bundle_support_files(bundle_root, target)


def set_active_distro(home: Path, distro_name: str) -> None:
    distrib_root = home / "distrib"
    active = distrib_root / "active"
    if active.exists() or active.is_symlink():
        if active.is_dir() and not active.is_symlink():
            shutil.rmtree(active)
        else:
            active.unlink()
    active.symlink_to(Path(distro_name))

    boot = home / "boot.py"
    if boot.exists() or boot.is_symlink():
        boot.unlink(missing_ok=True)
    boot.symlink_to(Path("distrib") / "active" / "boot.py")

    refresh_runtime_surface(home)


def refresh_runtime_surface(home: Path) -> None:
    link_runtime_surface(home / "distrib" / "active" / "templates", home / "templates")
    link_runtime_surface(home / "distrib" / "active" / "skills", home / "skills", preserve={"lib"})


def link_runtime_surface(src_dir: Path, dst_dir: Path, preserve: set[str] | None = None) -> None:
    preserve = preserve or set()
    dst_dir.mkdir(parents=True, exist_ok=True)
    for existing in list(dst_dir.iterdir()):
        if existing.name in preserve:
            continue
        if existing.is_dir() and not existing.is_symlink():
            shutil.rmtree(existing)
        else:
            existing.unlink(missing_ok=True)
    if not src_dir.is_dir():
        return
    for entry in src_dir.iterdir():
        if entry.name in preserve:
            continue
        target = dst_dir / entry.name
        target.symlink_to(Path(os.path.relpath(entry, start=dst_dir)))


if __name__ == "__main__":
    raise SystemExit(main())

"""Composition pipeline: resolve sources -> stage generation -> atomic switch."""
from __future__ import annotations

import os
import shutil
from dataclasses import dataclass
from pathlib import Path

from . import config as cfg
from . import generations as gens
from . import lock as lockmod
from . import sources as srcmod
from .cache import GitCache
from .sources import GitSource, LocalSource, Source


IGNORE_NAMES = {"__pycache__", ".pytest_cache", ".git", ".DS_Store"}


def _ignored(_dir: str, names: list[str]) -> set[str]:
    return {n for n in names if n in IGNORE_NAMES or n.endswith(".pyc")}


def _copytree(src: Path, dst: Path) -> None:
    shutil.copytree(src, dst, symlinks=False, ignore=_ignored)


def _replace_dir(target: Path) -> None:
    if target.is_symlink() or target.exists():
        if target.is_dir() and not target.is_symlink():
            shutil.rmtree(target)
        else:
            target.unlink()


class InstallError(RuntimeError):
    pass


@dataclass
class Plan:
    distro: cfg.DistroConfig
    home: Path
    offline: bool = False
    update: bool = False  # if True, ignore lock and re-resolve all git refs
    update_only: tuple[str, ...] = ()  # subset of names to update; () means all when update=True


def install(distro_dir: Path, home: Path, *,
            override_name: str | None = None,
            offline: bool = False,
            update: bool = False,
            update_only: tuple[str, ...] = (),
            keep_generations: int = 5) -> tuple[gens.Generation, lockmod.Lock]:
    """Install ``distro_dir`` into ``home``. Returns (new_generation, lock)."""
    distro = cfg.load(distro_dir, override_name=override_name)
    plan = Plan(distro=distro, home=home, offline=offline, update=update, update_only=tuple(update_only))

    home.mkdir(parents=True, exist_ok=True)
    gens.migrate_legacy(home, distro.name)
    existing_gens = gens.list_generations(home, distro.name)
    new_name = gens.next_generation_name(existing_gens)
    new_path = gens.generations_dir(home, distro.name) / new_name
    new_path.parent.mkdir(parents=True, exist_ok=True)

    staging = new_path.with_name(new_name + ".staging")
    if staging.exists():
        shutil.rmtree(staging)

    try:
        new_lock = _stage(plan, staging)
    except Exception:
        shutil.rmtree(staging, ignore_errors=True)
        raise

    os.replace(staging, new_path)
    new_gen = gens.Generation(number=int(new_name.split("-", 1)[0]), name=new_name, path=new_path)

    lockmod.save(gens.distro_root(home, distro.name) / "distro.lock.json", new_lock)

    gens.set_current(home, distro.name, new_gen)
    _expose_current(home, distro.name)
    _set_active(home, distro.name)
    _refresh_runtime_surface(home)
    gens.prune(home, distro.name, keep=keep_generations)
    return new_gen, new_lock


def _stage(plan: Plan, staging: Path) -> lockmod.Lock:
    """Build a fully composed distro tree at ``staging`` and return its lock."""
    distro = plan.distro
    _copytree(distro.path, staging)

    # Drop our own config artifacts from the staged tree (they live at distro root, not generation).
    for noise in ("distro.toml", "distro.override.toml", "distro.lock.json"):
        p = staging / noise
        if p.exists():
            p.unlink()

    skills_dir = staging / "skills"
    skills_dir.mkdir(parents=True, exist_ok=True)

    _materialize_inline_symlinks(distro.path / "skills", skills_dir)

    cache = GitCache(plan.home / "cache")
    prior_lock = lockmod.load(gens.distro_root(plan.home, distro.name) / "distro.lock.json")
    new_lock = lockmod.Lock(distro=distro.name)

    for entry in distro.skills:
        resolved_dir, lock_entry = _resolve(entry.source, distro.path, cache, plan,
                                           prior=_prior_skill(prior_lock, entry.name))
        _install_skill(resolved_dir, skills_dir / entry.name, override=entry.override, label=f"skill {entry.name}")
        new_lock.skills[entry.name] = lock_entry

    for entry in distro.bundles:
        resolved_dir, lock_entry = _resolve(entry.source, distro.path, cache, plan,
                                           prior=_prior_bundle(prior_lock, entry.name))
        _install_bundle(resolved_dir, skills_dir, allowlist=entry.skills,
                        override=entry.override, bundle_name=entry.name)
        new_lock.bundles[entry.name] = lock_entry

    return new_lock


def _prior_skill(lock: lockmod.Lock | None, name: str) -> lockmod.LockEntry | None:
    return None if lock is None else lock.skills.get(name)


def _prior_bundle(lock: lockmod.Lock | None, name: str) -> lockmod.LockEntry | None:
    return None if lock is None else lock.bundles.get(name)


def _should_use_lock_for(name: str, plan: Plan) -> bool:
    if not plan.update:
        return True
    if plan.update_only and name not in plan.update_only:
        return True
    return False


def _resolve(uri: str, base_dir: Path, cache: GitCache, plan: Plan, *,
             prior: lockmod.LockEntry | None) -> tuple[Path, lockmod.LockEntry]:
    src = srcmod.parse(uri, base_dir=base_dir)

    if isinstance(src, LocalSource):
        if not src.path.is_dir():
            raise InstallError(f"local source not a directory: {src.path}")
        return src.path, lockmod.LockEntry(source=uri, resolved_path=str(src.path),
                                           fetched_at=lockmod.now_iso())

    assert isinstance(src, GitSource)
    name_for_lock = uri  # we just use the URI as identity for prior_matches
    if prior is not None and prior.source == uri and prior.resolved_sha and _should_use_lock_for(name_for_lock, plan):
        # Reuse pinned sha from lock; treat ref as the pinned sha.
        pinned = GitSource(url=src.url, ref=prior.resolved_sha, subpath=src.subpath, pinned_sha=True)
        try:
            checkout = cache.fetch(pinned, offline=plan.offline)
        except Exception as exc:
            if plan.offline:
                raise InstallError(f"frozen install failed for {uri}: {exc}") from exc
            checkout = cache.fetch(src, offline=False)
            return _git_result(uri, src, checkout)
        return checkout.worktree if not src.subpath else (checkout.worktree / src.subpath), \
            lockmod.LockEntry(source=uri, resolved_sha=checkout.sha,
                              resolved_ref=prior.resolved_ref or src.ref,
                              subpath=src.subpath or None,
                              fetched_at=prior.fetched_at or lockmod.now_iso())

    checkout = cache.fetch(src, offline=plan.offline)
    return _git_result(uri, src, checkout)


def _git_result(uri: str, src: GitSource, checkout) -> tuple[Path, lockmod.LockEntry]:
    root = checkout.worktree if not src.subpath else (checkout.worktree / src.subpath)
    if not root.is_dir():
        raise InstallError(f"git source subpath not found: {src.subpath} in {src.url}")
    return root, lockmod.LockEntry(
        source=uri,
        resolved_sha=checkout.sha,
        resolved_ref=src.ref,
        subpath=src.subpath or None,
        fetched_at=lockmod.now_iso(),
    )


def _install_skill(src: Path, dst: Path, *, override: bool, label: str) -> None:
    if dst.exists() or dst.is_symlink():
        if not override:
            raise InstallError(
                f"{label}: target {dst.name!r} already exists; "
                f"set override = true on this entry to replace it"
            )
        _replace_dir(dst)
    _copytree(src, dst)


def _install_bundle(bundle_root: Path, skills_dir: Path, *, allowlist: tuple[str, ...],
                    override: bool, bundle_name: str) -> None:
    if not bundle_root.is_dir():
        raise InstallError(f"bundle {bundle_name}: source is not a directory: {bundle_root}")

    skills_to_install: list[Path] = []
    support_dirs: list[Path] = []
    for entry in sorted(bundle_root.iterdir()):
        if not entry.is_dir():
            continue
        if entry.name in IGNORE_NAMES or entry.name.startswith("."):
            continue
        if entry.name.startswith("_"):
            support_dirs.append(entry)
            continue
        if allowlist and entry.name not in allowlist:
            continue
        skills_to_install.append(entry)

    if allowlist:
        present = {e.name for e in skills_to_install}
        missing = [n for n in allowlist if n not in present]
        if missing:
            raise InstallError(f"bundle {bundle_name}: skills not found: {', '.join(missing)}")

    for entry in skills_to_install:
        _install_skill(entry, skills_dir / entry.name, override=override,
                       label=f"bundle {bundle_name} -> skill {entry.name}")

    for support in support_dirs:
        target = skills_dir / support.name
        if target.exists() or target.is_symlink():
            if not override:
                raise InstallError(
                    f"bundle {bundle_name}: support dir {support.name!r} already exists; "
                    f"set override = true on this entry to replace it"
                )
            _replace_dir(target)
        _copytree(support, target)


def _materialize_inline_symlinks(src_skills: Path, dst_skills: Path) -> None:
    """Resolve any symlinks present inside the source distro's skills/ tree.

    These are dev-time symlinks (e.g. distrib/ouroboros/skills/files ->
    ../../../skills/files). Copy the resolved target into the staged tree, and
    also pull any sibling _* support dirs if the symlink points into a
    bundles/<name>/<skill> location.
    """
    if not src_skills.is_dir():
        return
    for entry in src_skills.iterdir():
        if not entry.is_symlink():
            continue
        try:
            resolved = entry.resolve(strict=True)
        except FileNotFoundError:
            raise InstallError(f"dangling symlink in distro: {entry}")
        if not resolved.is_dir():
            continue
        target = dst_skills / entry.name
        _replace_dir(target)
        _copytree(resolved, target)
        bundle_root = _bundle_root_for(resolved)
        if bundle_root is not None:
            for sib in bundle_root.iterdir():
                if not sib.is_dir() or not sib.name.startswith("_"):
                    continue
                sib_target = dst_skills / sib.name
                if sib_target.exists() or sib_target.is_symlink():
                    continue  # don't override skills/_* placed by config
                _copytree(sib, sib_target)


def _bundle_root_for(path: Path) -> Path | None:
    parts = path.parts
    if "bundles" not in parts:
        return None
    idx = parts.index("bundles")
    if idx + 1 >= len(parts):
        return None
    return Path(*parts[: idx + 2])


# ── exposing current generation through stable paths ──────────────────────


def _expose_current(home: Path, distro_name: str) -> None:
    root = gens.distro_root(home, distro_name)
    for entry in ("boot.py", "skills", "templates"):
        link = root / entry
        if link.exists() or link.is_symlink():
            if link.is_dir() and not link.is_symlink():
                shutil.rmtree(link)
            else:
                link.unlink()
        link.symlink_to(Path("current") / entry)


def _set_active(home: Path, distro_name: str) -> None:
    distrib_root = home / "distrib"
    distrib_root.mkdir(parents=True, exist_ok=True)
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


def _refresh_runtime_surface(home: Path) -> None:
    _link_runtime(home / "distrib" / "active" / "templates", home / "templates")
    _link_runtime(home / "distrib" / "active" / "skills", home / "skills", preserve={"lib"})


def _link_runtime(src_dir: Path, dst_dir: Path, preserve: set[str] | None = None) -> None:
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


def rollback(home: Path, distro_name: str, *, to: int | None = None) -> gens.Generation:
    available = gens.list_generations(home, distro_name)
    if not available:
        raise InstallError(f"no generations for distro {distro_name!r}")
    current = gens.current_generation(home, distro_name)
    if to is None:
        candidates = [g for g in available if not current or g.number < current.number]
        if not candidates:
            raise InstallError(f"already at oldest generation for {distro_name!r}")
        target = candidates[-1]
    else:
        match = [g for g in available if g.number == to]
        if not match:
            raise InstallError(f"generation {to} not found for {distro_name!r}")
        target = match[0]
    gens.set_current(home, distro_name, target)
    _expose_current(home, distro_name)
    _set_active(home, distro_name)
    _refresh_runtime_surface(home)
    return target

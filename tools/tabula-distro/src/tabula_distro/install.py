"""Composition pipeline: resolve sources -> stage generation -> atomic switch."""
from __future__ import annotations

import hashlib
import os
import shutil
from dataclasses import dataclass
from pathlib import Path

from . import config as cfg
from . import generations as gens
from . import lock as lockmod
from . import sources as srcmod
from .cache import GitCache
from .manifest import ManifestError, load_bundle_manifest
from .semver import Constraint, Version, VersionError
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


_FINGERPRINT_FILE = ".fingerprint"


def _fingerprint_tree(root: Path) -> str:
    """Stable hash of a staged distro tree.

    Walks the tree deterministically and hashes (relative path, mode bit,
    file content / symlink target). The ``.fingerprint`` marker file itself
    is excluded so it doesn't perturb its own hash.
    """
    h = hashlib.sha256()
    root = root.resolve()
    entries: list[tuple[str, Path]] = []
    for dirpath, dirnames, filenames in os.walk(root, followlinks=False):
        dirnames.sort()
        for name in sorted(filenames):
            if name == _FINGERPRINT_FILE and Path(dirpath) == root:
                continue
            full = Path(dirpath) / name
            rel = full.relative_to(root).as_posix()
            entries.append((rel, full))
    for rel, full in entries:
        h.update(rel.encode("utf-8"))
        h.update(b"\x00")
        if full.is_symlink():
            h.update(b"L")
            h.update(os.readlink(full).encode("utf-8"))
        else:
            mode = "x" if os.access(full, os.X_OK) else "-"
            h.update(mode.encode("ascii"))
            with full.open("rb") as f:
                while chunk := f.read(65536):
                    h.update(chunk)
        h.update(b"\n")
    return h.hexdigest()


def _generation_fingerprint(gen_path: Path) -> str | None:
    """Return the cached fingerprint of an installed generation, if present."""
    marker = gen_path / _FINGERPRINT_FILE
    if marker.is_file():
        return marker.read_text(encoding="utf-8").strip()
    # Legacy generations installed before fingerprinting: compute on the fly so
    # the next no-op install can dedupe against them too.
    try:
        return _fingerprint_tree(gen_path)
    except FileNotFoundError:
        return None


def _read_kernel_version(home: Path) -> Version | None:
    """Read installed kernel version from ``$TABULA_HOME/VERSION``.

    Returns ``None`` if the file is absent (legacy install or uninitialised
    home). Raises ``InstallError`` if the file exists but is malformed.
    """
    vfile = home / "VERSION"
    if not vfile.is_file():
        return None
    raw = vfile.read_text(encoding="utf-8").strip()
    if not raw:
        return None
    try:
        return Version.parse(raw)
    except VersionError as exc:
        raise InstallError(f"$TABULA_HOME/VERSION is malformed: {exc}") from exc


def _check_kernel_compat(distro: cfg.DistroConfig, kernel: Version | None) -> None:
    """Hard-fail if the distro requires a kernel version that doesn't match."""
    if distro.requires_kernel is None:
        return
    if kernel is None:
        raise InstallError(
            f"distro {distro.name!r} requires kernel {distro.requires_kernel.raw}, "
            f"but no kernel version is recorded at $TABULA_HOME/VERSION "
            f"(install/upgrade the kernel first via install.sh / install-dev.sh)"
        )
    if not distro.requires_kernel.matches(kernel):
        raise InstallError(
            f"distro {distro.name!r} requires kernel {distro.requires_kernel.raw}, "
            f"but installed kernel is {kernel}"
        )


def _check_bundle_compat(bundle_name: str, manifest, kernel: Version | None) -> None:
    """Hard-fail if a bundle requires a kernel version that doesn't match."""
    if manifest.requires_kernel is None:
        return
    if kernel is None:
        raise InstallError(
            f"bundle {bundle_name!r} requires kernel {manifest.requires_kernel.raw}, "
            f"but no kernel version is recorded at $TABULA_HOME/VERSION"
        )
    if not manifest.requires_kernel.matches(kernel):
        raise InstallError(
            f"bundle {bundle_name!r} requires kernel {manifest.requires_kernel.raw}, "
            f"but installed kernel is {kernel}"
        )


def _resolve_distro_source(value: str | Path, home: Path, *, offline: bool) -> tuple[Path, str | None]:
    """Resolve a distro source (path / local: / git+) to a local directory.

    Returns ``(directory, original_uri_or_None)``. The URI is returned only if
    the source was non-local (so it can be persisted in the lockfile and re-used
    by ``tabula-distro update`` later).
    """
    if isinstance(value, Path):
        return value.resolve(), None

    text = str(value)
    if text.startswith("local:") or text.startswith("git+"):
        src = srcmod.parse(text, base_dir=Path.cwd())
        if isinstance(src, srcmod.LocalSource):
            return src.path, text
        cache = GitCache(home / "cache")
        checkout = cache.fetch(src, offline=offline)
        root = checkout.worktree if not src.subpath else checkout.worktree / src.subpath
        if not root.is_dir():
            raise InstallError(f"git source subpath not found: {src.subpath} in {src.url}")
        return root, text

    return Path(text).expanduser().resolve(), None


@dataclass
class Plan:
    distro: cfg.DistroConfig
    home: Path
    offline: bool = False
    update: bool = False  # if True, ignore lock and re-resolve all git refs
    update_only: tuple[str, ...] = ()  # subset of names to update; () means all when update=True


@dataclass(frozen=True)
class InstallResult:
    generation: gens.Generation
    lock: lockmod.Lock
    changed: bool

    def __iter__(self):
        # Preserve the historical `gen, lock = install(...)` API while letting
        # callers opt into the changed flag as `result.changed`.
        yield self.generation
        yield self.lock


def install(distro_dir: str | Path, home: Path, *,
            override_name: str | None = None,
            offline: bool = False,
            update: bool = False,
            update_only: tuple[str, ...] = (),
            keep_generations: int = 5) -> InstallResult:
    """Install a distro into ``home``.

    ``distro_dir`` may be:
      - a local path (``Path`` or ``str``),
      - a ``local:<path>`` URI,
      - a ``git+<url>@<ref>[#path=<subdir>]`` URI.

    Returns an ``InstallResult``. It remains unpackable as ``(generation, lock)``
    for compatibility; use ``result.changed`` to tell whether a new generation
    was promoted.
    """
    distro_path, distro_source_uri = _resolve_distro_source(
        distro_dir, home, offline=offline,
    )
    distro = cfg.load(distro_path, override_name=override_name)

    # Hard-fail before doing any work if the distro is incompatible with the
    # installed kernel.
    kernel_version = _read_kernel_version(home)
    _check_kernel_compat(distro, kernel_version)

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
        new_lock = _stage(plan, staging, kernel_version=kernel_version)
    except Exception:
        shutil.rmtree(staging, ignore_errors=True)
        raise

    if distro_source_uri is not None:
        new_lock.distro_source = distro_source_uri
    if distro.version is not None:
        new_lock.distro_version = str(distro.version)
    if kernel_version is not None:
        new_lock.kernel_version = str(kernel_version)

    # If the staged tree is byte-identical to the current generation, reuse it
    # instead of promoting a new generation. Keeps `distrib/<name>/generations/`
    # from growing on every no-op re-install.
    staging_fp = _fingerprint_tree(staging)
    current = gens.current_generation(home, distro.name)
    if current is not None and _generation_fingerprint(current.path) == staging_fp:
        shutil.rmtree(staging, ignore_errors=True)
        # Refresh lockfile/runtime surface with any non-tree updates (e.g. new
        # lock metadata) but keep the existing generation as current.
        lockmod.save(gens.distro_root(home, distro.name) / "distro.lock.json", new_lock)
        _expose_current(home, distro.name)
        _set_active(home, distro.name)
        _refresh_runtime_surface(home)
        return InstallResult(current, new_lock, False)

    (staging / ".fingerprint").write_text(staging_fp, encoding="utf-8")
    os.replace(staging, new_path)
    new_gen = gens.Generation(number=int(new_name.split("-", 1)[0]), name=new_name, path=new_path)

    lockmod.save(gens.distro_root(home, distro.name) / "distro.lock.json", new_lock)

    gens.set_current(home, distro.name, new_gen)
    _expose_current(home, distro.name)
    _set_active(home, distro.name)
    _refresh_runtime_surface(home)
    gens.prune(home, distro.name, keep=keep_generations)
    return InstallResult(new_gen, new_lock, True)


def _stage(plan: Plan, staging: Path, *, kernel_version: Version | None) -> lockmod.Lock:
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
        manifest = load_bundle_manifest(resolved_dir)
        _check_bundle_compat(entry.name, manifest, kernel_version)
        if manifest.version is not None:
            lock_entry.version = str(manifest.version)
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


def _install_bundle(bundle_root: Path, skills_dir: Path, *, allowlist: tuple[str, ...] | None,
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
        if not (entry / "SKILL.md").is_file():
            continue
        if allowlist is not None and entry.name not in allowlist:
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
    _link_runtime(home / "distrib" / "active" / "skills", home / "skills", preserve={"_pylib", "_tslib"})


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

"""Composition pipeline: resolve sources -> stage generation -> atomic switch."""
from __future__ import annotations

import hashlib
import os
import shutil
import time
import tomllib
from dataclasses import dataclass, replace
from pathlib import Path

from . import config as cfg
from . import generations as gens
from . import links
from . import lock as lockmod
from . import requirements as reqmod
from . import runtime_config as runtimecfg
from . import sources as srcmod
from .cache import GitCache
from .manifest import BundleManifest, ManifestError, load_bundle_manifest
from .plugin_manifest import (
    PluginManifest,
    PluginManifestError,
    PluginRequires,
    load_plugin_manifest,
)
from .semver import Constraint, Version, VersionError
from .sources import GitSource, LocalSource, Source


IGNORE_NAMES = {
    "__pycache__",
    ".pytest_cache",
    ".git",
    ".DS_Store",
    "node_modules",
}


def _write_kernel_config(home: Path) -> None:
    runtimecfg.write_kernel_config(home, url=os.environ.get("TABULA_URL", "ws://localhost:8089/ws"))


def _ignored(_dir: str, names: list[str]) -> set[str]:
    return {n for n in names if n in IGNORE_NAMES or n.endswith(".pyc")}


def _copytree(src: Path, dst: Path) -> None:
    dst.parent.mkdir(parents=True, exist_ok=True)
    shutil.copytree(src, dst, symlinks=False, ignore=_ignored)


def _replace_dir(target: Path) -> None:
    if links.is_reference(target) or target.exists():
        links.remove_path(target)


class InstallError(RuntimeError):
    pass


@dataclass(frozen=True)
class SDKPackageSurface:
    name: str
    language: str
    root: Path


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
        dirnames[:] = [d for d in sorted(dirnames) if d not in IGNORE_NAMES and not d.endswith(".pyc")]
        for name in dirnames:
            full = Path(dirpath) / name
            rel = full.relative_to(root).as_posix()
            entries.append((rel + "/", full))
        for name in sorted(filenames):
            if name in IGNORE_NAMES or name.endswith(".pyc"):
                continue
            if name == _FINGERPRINT_FILE and Path(dirpath) == root:
                continue
            full = Path(dirpath) / name
            rel = full.relative_to(root).as_posix()
            entries.append((rel, full))
    for rel, full in entries:
        h.update(rel.encode("utf-8"))
        h.update(b"\x00")
        if rel.endswith("/"):
            h.update(b"D")
        elif full.is_symlink():
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


# Cached protocol range read from $TABULA_HOME/PROTOCOL. Tuple of (min, max) or
# None if the file is absent / unreadable.
def _read_kernel_protocol_range(home: Path) -> tuple[int, int] | None:
    pfile = home / "PROTOCOL"
    if not pfile.is_file():
        return None
    import json as _json
    try:
        data = _json.loads(pfile.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        raise InstallError(f"$TABULA_HOME/PROTOCOL is malformed: {exc}") from exc
    lo = data.get("plugin_protocol_min")
    hi = data.get("plugin_protocol_max")
    if not isinstance(lo, int) or not isinstance(hi, int) or lo < 1 or hi < lo:
        raise InstallError(f"$TABULA_HOME/PROTOCOL has invalid plugin protocol range: {data!r}")
    return lo, hi


def _sdk_package_surfaces(staging: Path) -> dict[str, SDKPackageSurface]:
    """Return installed SDK package roots keyed by SDK requirement name.

    Declared package exports install under `packages/`; bundle-local `_lib`
    roots are not package inputs.
    """
    return {
        "tabula-plugin-sdk": SDKPackageSurface(
            name="tabula-plugin-sdk",
            language="python",
            root=staging / "packages" / "python" / "src" / "tabula_plugin_sdk",
        ),
        "@tabula/skill-sdk": SDKPackageSurface(
            name="@tabula/skill-sdk",
            language="typescript",
            root=staging / "packages" / "typescript" / "@tabula" / "skill-sdk",
        ),
    }


def _read_installed_sdk_version(staging: Path, sdk_name: str) -> Version | None:
    surface = _sdk_package_surfaces(staging).get(sdk_name)
    if surface is None or not surface.root.exists():
        return None
    if surface.language == "python":
        return _read_python_sdk_version(surface)
    if surface.language == "typescript":
        return _read_typescript_sdk_version(surface)
    return None


def _read_python_sdk_version(surface: SDKPackageSurface) -> Version:
    init = surface.root / "__init__.py"
    if not init.is_file():
        raise InstallError(f"installed SDK {surface.name} is missing __init__.py at {surface.root}")
    text = init.read_text(encoding="utf-8")
    import re as _re
    m = _re.search(r'^__version__\s*=\s*["\']([^"\']+)["\']', text, _re.M)
    if m is None:
        raise InstallError(f"installed SDK {surface.name} is missing __version__ at {init}")
    try:
        return Version.parse(m.group(1))
    except VersionError as exc:
        raise InstallError(f"installed SDK {surface.name} __version__ is malformed at {init}: {exc}") from exc


def _read_typescript_sdk_version(surface: SDKPackageSurface) -> Version:
    pkg = surface.root / "package.json"
    if not pkg.is_file():
        raise InstallError(f"installed SDK {surface.name} is missing package.json at {surface.root}")
    import json as _json
    try:
        data = _json.loads(pkg.read_text(encoding="utf-8"))
    except (OSError, ValueError) as exc:
        raise InstallError(f"installed SDK {surface.name} package.json is malformed at {pkg}: {exc}") from exc
    version_raw = data.get("version")
    if not isinstance(version_raw, str):
        raise InstallError(f"installed SDK {surface.name} package.json missing version at {pkg}")
    try:
        return Version.parse(version_raw)
    except VersionError as exc:
        raise InstallError(f"installed SDK {surface.name} package.json version is malformed at {pkg}: {exc}") from exc


def _check_plugin_compat(plugin_name: str, plugin_dir: Path, *,
                          kernel: Version | None,
                          kernel_protocol_range: tuple[int, int] | None,
                          staging: Path) -> PluginRequires | None:
    """Hard-fail on plugin manifest incompatibility.

    Reads ``plugin.toml`` from ``plugin_dir`` and checks:

    1. ``requires.kernel`` against the installed kernel version
    2. ``requires.protocol_version`` intersects the kernel's range
    3. ``requires.sdk`` matches the SDK version reported by the installed
       package surface

    Returns the parsed :class:`PluginRequires` (for callers that want to
    record it) or ``None`` if the plugin has no ``[requires]`` block (rollout
    tolerance — emits no error here).
    """
    try:
        manifest = load_plugin_manifest(plugin_dir)
    except PluginManifestError as exc:
        raise InstallError(f"plugin {plugin_name!r}: {exc}") from exc
    requires = manifest.requires
    if requires is None:
        # Tolerated during rollout. The kernel manifest loader is the
        # canonical enforcement point; it accepts missing [requires] today
        # and will be flipped to mandatory in lockstep with this installer.
        return None

    if requires.kernel is not None:
        if kernel is None:
            raise InstallError(
                f"plugin {plugin_name!r} requires kernel {requires.kernel.raw}, "
                f"but no kernel version is recorded at $TABULA_HOME/VERSION"
            )
        if not requires.kernel.matches(kernel):
            raise InstallError(
                f"plugin {plugin_name!r} requires kernel {requires.kernel.raw}, "
                f"but installed kernel is {kernel}"
            )

    if requires.protocol_versions:
        if kernel_protocol_range is None:
            raise InstallError(
                f"plugin {plugin_name!r} declares protocol_version "
                f"{list(requires.protocol_versions)}, but $TABULA_HOME/PROTOCOL "
                f"is missing (reinstall the kernel via install-dev.sh / install.sh)"
            )
        lo, hi = kernel_protocol_range
        kernel_versions = set(range(lo, hi + 1))
        intersection = sorted(set(requires.protocol_versions) & kernel_versions)
        if not intersection:
            raise InstallError(
                f"plugin {plugin_name!r} supports protocol_version "
                f"{list(requires.protocol_versions)}, but kernel speaks {lo}..{hi}"
            )

    if requires.sdk is not None:
        installed = _read_installed_sdk_version(staging, requires.sdk.name)
        if installed is None:
            raise InstallError(
                f"plugin {plugin_name!r} requires {requires.sdk.name}"
                f"{requires.sdk.constraint.raw}, but no copy of {requires.sdk.name} "
                f"is staged under packages/ (ensure a selected bundle exports it)"
            )
        if not requires.sdk.constraint.matches(installed):
            raise InstallError(
                f"plugin {plugin_name!r} requires {requires.sdk.name}"
                f"{requires.sdk.constraint.raw}, but installed {requires.sdk.name} "
                f"is {installed}"
            )

    return requires


def _resolve_distro_source(value: str | Path, home: Path, *, offline: bool, base_dir: Path | None = None) -> tuple[Path, str | None]:
    """Resolve a distro source (path / local: / git+) to a local directory.

    Returns ``(directory, source_string)``. The source string is what should be
    persisted in the lockfile so later commands like ``update`` and
    ``reinstall`` can resolve the same distro again. For direct local paths we
    normalize to an absolute path.
    """
    if isinstance(value, Path):
        resolved = value.resolve()
        return resolved, str(resolved)

    text = str(value)
    if text.startswith("local:") or text.startswith("git+"):
        src = srcmod.parse(text, base_dir=base_dir or Path.cwd())
        if isinstance(src, srcmod.LocalSource):
            return src.path, text
        cache = GitCache(home / "cache")
        checkout = cache.fetch(src, offline=offline)
        root = checkout.worktree if not src.subpath else checkout.worktree / src.subpath
        if not root.is_dir():
            raise InstallError(f"git source subpath not found: {src.subpath} in {src.url}")
        return root, text

    candidate = Path(text).expanduser()
    if not candidate.is_absolute():
        candidate = (base_dir or Path.cwd()) / candidate
    resolved = candidate.resolve()
    return resolved, str(resolved)


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


@dataclass(frozen=True)
class InstalledBundleComponents:
    skills: tuple[str, ...] = ()
    plugins: tuple[str, ...] = ()
    apps: tuple[str, ...] = ()


@dataclass(frozen=True)
class ResolvedBundleInstall:
    entry: cfg.BundleEntry
    resolved_dir: Path
    lock_entry: lockmod.LockEntry
    manifest: BundleManifest


def install(distro_dir: str | Path, home: Path, *,
            override_name: str | None = None,
            offline: bool = False,
            update: bool = False,
            update_only: tuple[str, ...] = (),
            tenant: str | None = None,
            expose_global_boot: bool = True,
            base_dir: Path | None = None,
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
        distro_dir, home, offline=offline, base_dir=base_dir,
    )
    distro = cfg.load(distro_path, override_name=override_name)

    # Hard-fail before doing any work if the distro is incompatible with the
    # installed kernel.
    kernel_version = _read_kernel_version(home)
    _check_kernel_compat(distro, kernel_version)
    statuses = reqmod.require_executables(distro)
    for warning in reqmod.warnings(statuses):
        print(f"tabula-distro: warning: {warning}")

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

    # Snapshot the live plugin protocol / SDK surface into the lock. These come
    # from the kernel install (PROTOCOL file) and the staged package surface, so
    # they reflect the contract that this generation actually satisfies.
    proto_range = _read_kernel_protocol_range(home)
    if proto_range is not None:
        new_lock.plugin_protocol_version = proto_range[1]
    for sdk_name in ("tabula-plugin-sdk", "@tabula/skill-sdk"):
        sdk_version = _read_installed_sdk_version(staging, sdk_name)
        if sdk_version is not None:
            new_lock.sdk_versions[sdk_name] = str(sdk_version)

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
        _set_active(home, distro.name, expose_global_boot=expose_global_boot)
        _refresh_runtime_surface(home, tenant=tenant)
        _write_kernel_config(home)
        return InstallResult(current, new_lock, False)

    (staging / ".fingerprint").write_text(staging_fp, encoding="utf-8")
    os.replace(staging, new_path)
    new_gen = gens.Generation(number=int(new_name.split("-", 1)[0]), name=new_name, path=new_path)

    lockmod.save(gens.distro_root(home, distro.name) / "distro.lock.json", new_lock)

    gens.set_current(home, distro.name, new_gen)
    _expose_current(home, distro.name)
    _set_active(home, distro.name, expose_global_boot=expose_global_boot)
    _refresh_runtime_surface(home, tenant=tenant)
    _write_kernel_config(home)
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
    plugins_dir = staging / "plugins"
    plugins_dir.mkdir(parents=True, exist_ok=True)
    apps_dir = staging / "apps"
    apps_dir.mkdir(parents=True, exist_ok=True)
    packages_dir = staging / "packages"
    packages_dir.mkdir(parents=True, exist_ok=True)

    cache = GitCache(plan.home / "cache")
    prior_lock = lockmod.load(gens.distro_root(plan.home, distro.name) / "distro.lock.json")
    new_lock = lockmod.Lock(distro=distro.name)
    installed_python_packages: dict[str, tuple[str, Path]] = {}
    installed_typescript_packages: dict[str, tuple[str, Path]] = {}

    _install_distro_python_exports(distro.path, distro.exports_python_packages, packages_dir, installed_python_packages, distro_name=distro.name)

    for entry in distro.skills:
        resolved_dir, lock_entry = _resolve(entry.source, distro.path, cache, plan,
                                            prior=_prior_skill(prior_lock, entry.name),
                                            lock_names=(entry.name, entry.source))
        _install_skill(resolved_dir, skills_dir / entry.name, override=entry.override, label=f"skill {entry.name}")
        new_lock.skills[entry.name] = lock_entry

    for entry in distro.plugins:
        resolved_dir, lock_entry = _resolve(entry.source, distro.path, cache, plan,
                                            prior=_prior_plugin(prior_lock, entry.name),
                                            lock_names=(entry.name, entry.source))
        _install_plugin(resolved_dir, plugins_dir / entry.name, override=entry.override, label=f"plugin {entry.name}")
        new_lock.plugins[entry.name] = lock_entry

    resolved_bundles: list[ResolvedBundleInstall] = []
    bundle_manifests: dict[str, BundleManifest] = {}
    for entry in distro.bundles:
        bundle_names = _bundle_update_names(entry, prior_lock)
        resolved_dir, lock_entry = _resolve(entry.source, distro.path, cache, plan,
                                            prior=_prior_bundle(prior_lock, entry.name),
                                            lock_names=bundle_names)
        manifest = load_bundle_manifest(resolved_dir)
        _check_bundle_compat(entry.name, manifest, kernel_version)
        if manifest.version is not None:
            lock_entry.version = str(manifest.version)
        resolved_bundles.append(ResolvedBundleInstall(entry=entry, resolved_dir=resolved_dir, lock_entry=lock_entry, manifest=manifest))
        bundle_manifests[entry.name] = manifest

    _validate_bundle_dependencies(bundle_manifests)

    for resolved in resolved_bundles:
        entry = resolved.entry
        lock_entry = resolved.lock_entry
        installed = _install_bundle(resolved.resolved_dir, skills_dir, plugins_dir, apps_dir, packages_dir, installed_python_packages, installed_typescript_packages, manifest=resolved.manifest,
                                      allowlist=entry.components, override=entry.override,
                                      bundle_name=entry.name, source_uri=lock_entry.source)
        new_lock.bundles[entry.name] = lock_entry
        for name in installed.skills:
            new_lock.skills[name] = replace(lock_entry)
        for name in installed.plugins:
            new_lock.plugins[name] = replace(lock_entry)
        for name in installed.apps:
            new_lock.apps[name] = replace(lock_entry)

    # Plugin compat checks run last so staged package exports are fully
    # populated by all bundles. We re-walk `staging/plugins` rather than
    # tracking install-time paths because bundle-sourced plugins are copied
    # into the staging tree by `_install_bundle`.
    kernel_protocol_range = _read_kernel_protocol_range(plan.home)
    for plugin_name in sorted(d.name for d in plugins_dir.iterdir() if d.is_dir()):
        _check_plugin_compat(
            plugin_name,
            plugins_dir / plugin_name,
            kernel=kernel_version,
            kernel_protocol_range=kernel_protocol_range,
            staging=staging,
        )

    return new_lock


def _prior_skill(lock: lockmod.Lock | None, name: str) -> lockmod.LockEntry | None:
    return None if lock is None else lock.skills.get(name)


def _prior_bundle(lock: lockmod.Lock | None, name: str) -> lockmod.LockEntry | None:
    return None if lock is None else lock.bundles.get(name)


def _prior_plugin(lock: lockmod.Lock | None, name: str) -> lockmod.LockEntry | None:
    return None if lock is None else lock.plugins.get(name)


def _bundle_update_names(entry: cfg.BundleEntry, prior_lock: lockmod.Lock | None) -> tuple[str, ...]:
    """Return names that should refresh this bundle during ``--update-only``.

    Bundle-sourced components are recorded in ``lock.skills``/``lock.plugins``
    with a copied bundle lock entry, not with their own source. Including those
    prior component names lets ``tabula-distro update --only <component>`` update
    the owning bundle source even when the current distro entry is a legacy
    walk-discovered bundle without an explicit ``components`` allowlist.
    """
    names: list[str] = [entry.name, entry.source]
    if entry.components is not None:
        names.extend(entry.components)
    if prior_lock is None:
        return tuple(dict.fromkeys(names))

    prior_bundle = _prior_bundle(prior_lock, entry.name)
    if prior_bundle is None:
        return tuple(dict.fromkeys(names))
    for collection in (prior_lock.skills, prior_lock.plugins):
        for component_name, component_entry in collection.items():
            if _same_lock_entry_origin(component_entry, prior_bundle):
                names.append(component_name)
    return tuple(dict.fromkeys(names))


def _same_lock_entry_origin(a: lockmod.LockEntry, b: lockmod.LockEntry) -> bool:
    return (
        a.source == b.source
        and a.resolved_sha == b.resolved_sha
        and a.resolved_ref == b.resolved_ref
        and a.subpath == b.subpath
        and a.resolved_path == b.resolved_path
    )


def _should_use_lock_for(names: str | tuple[str, ...], plan: Plan) -> bool:
    if not plan.update:
        return True
    identities = (names,) if isinstance(names, str) else names
    if plan.update_only and not any(name in plan.update_only for name in identities):
        return True
    return False


def _resolve(uri: str, base_dir: Path, cache: GitCache, plan: Plan, *,
             prior: lockmod.LockEntry | None,
             lock_names: tuple[str, ...] | None = None) -> tuple[Path, lockmod.LockEntry]:
    effective_uri = _expand_source_alias(uri, plan.distro.sources)
    src = srcmod.parse(effective_uri, base_dir=base_dir)

    if isinstance(src, LocalSource):
        if not src.path.is_dir():
            raise InstallError(f"local source not a directory: {src.path}")
        return src.path, lockmod.LockEntry(source=effective_uri, resolved_path=str(src.path),
                                            fetched_at=lockmod.now_iso())

    assert isinstance(src, GitSource)
    if prior is not None and prior.source == effective_uri and prior.resolved_sha and _should_use_lock_for(lock_names or (uri, effective_uri), plan):
        # Reuse pinned sha from lock; treat ref as the pinned sha.
        pinned = GitSource(url=src.url, ref=prior.resolved_sha, subpath=src.subpath, pinned_sha=True)
        try:
            checkout = cache.fetch(pinned, offline=plan.offline)
        except Exception as exc:
            if plan.offline:
                raise InstallError(f"frozen install failed for {effective_uri}: {exc}") from exc
            checkout = cache.fetch(src, offline=False)
            return _git_result(effective_uri, src, checkout)
        return checkout.worktree if not src.subpath else (checkout.worktree / src.subpath), \
            lockmod.LockEntry(source=effective_uri, resolved_sha=checkout.sha,
                              resolved_ref=prior.resolved_ref or src.ref,
                              subpath=src.subpath or None,
                              fetched_at=prior.fetched_at or lockmod.now_iso())

    checkout = cache.fetch(src, offline=plan.offline)
    return _git_result(effective_uri, src, checkout)


def _expand_source_alias(uri: str, sources: dict[str, cfg.SourceAlias]) -> str:
    expanded = uri
    seen: set[str] = set()
    while expanded.startswith("source:"):
        body, subpath = srcmod.split_fragment(expanded)
        alias = body[len("source:"):]
        if not alias:
            raise InstallError(f"empty source alias in {uri!r}")
        if alias in seen:
            raise InstallError(f"circular source alias in {uri!r}: {alias}")
        seen.add(alias)
        entry = sources.get(alias)
        if entry is None:
            raise InstallError(f"unknown source alias {alias!r} in {uri!r}")
        expanded = srcmod.join_path_fragment(entry.source, subpath)
    return expanded


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
    if not (src / "SKILL.md").is_file():
        raise InstallError(f"{label}: missing SKILL.md")
    _install_component(src, dst, override=override, label=label)


def _install_plugin(src: Path, dst: Path, *, override: bool, label: str) -> None:
    if not (src / "plugin.toml").is_file():
        raise InstallError(f"{label}: missing plugin.toml")
    _install_component(src, dst, override=override, label=label)


def _install_component(src: Path, dst: Path, *, override: bool, label: str) -> None:
    if dst.exists() or dst.is_symlink():
        if not override:
            raise InstallError(
                f"{label}: target {dst.name!r} already exists; "
                f"set override = true on this entry to replace it"
            )
        _replace_dir(dst)
    _copytree(src, dst)


def _install_app(src: Path, dst: Path, *, override: bool, label: str) -> None:
    _validate_app_manifest(src, label=label)
    _install_component(src, dst, override=override, label=label)


def _validate_app_manifest(src: Path, *, label: str) -> str:
    manifest = src / "app.toml"
    if not manifest.is_file():
        raise InstallError(f"{label}: missing app.toml")
    try:
        with manifest.open("rb") as f:
            data = tomllib.load(f)
    except tomllib.TOMLDecodeError as exc:
        raise InstallError(f"{label}: invalid app.toml: {exc}") from exc

    app_id = data.get("id")
    runtime = data.get("runtime")
    entry = data.get("entry")
    if not isinstance(app_id, str) or not app_id.strip():
        raise InstallError(f"{label}: app.toml id is required")
    app_id = app_id.strip()
    if app_id != src.name:
        raise InstallError(f"{label}: app.toml id {app_id!r} must match directory name {src.name!r}")
    if runtime not in {"python"}:
        raise InstallError(f"{label}: unsupported app runtime {runtime!r}")
    if not isinstance(entry, str) or not entry.strip():
        raise InstallError(f"{label}: app.toml entry is required")
    entry_path = (src / entry).resolve()
    try:
        entry_path.relative_to(src.resolve())
    except ValueError as exc:
        raise InstallError(f"{label}: app entry escapes component directory") from exc
    if not entry_path.is_file():
        raise InstallError(f"{label}: app entry does not exist: {entry}")

    bus = data.get("bus")
    if bus is not None:
        if not isinstance(bus, dict):
            raise InstallError(f"{label}: [bus] must be a TOML table")
        for key in ("sends", "receives"):
            values = bus.get(key)
            if values is not None and (not isinstance(values, list) or not all(isinstance(item, str) for item in values)):
                raise InstallError(f"{label}: bus.{key} must be an array of strings")
    return app_id


def _install_bundle(bundle_root: Path, skills_dir: Path, plugins_dir: Path, apps_dir: Path, packages_dir: Path,
                    installed_python_packages: dict[str, tuple[str, Path]],
                    installed_typescript_packages: dict[str, tuple[str, Path]], *,
                    manifest: BundleManifest, allowlist: tuple[str, ...] | None,
                    override: bool, bundle_name: str, source_uri: str) -> InstalledBundleComponents:
    if not bundle_root.is_dir():
        raise InstallError(f"bundle {bundle_name}: source is not a directory: {bundle_root}")

    candidates = _bundle_component_candidates(bundle_root, manifest, bundle_name=bundle_name)
    if allowlist is not None:
        allowed = set(allowlist)
        candidates = [c for c in candidates if c[0] in allowed]

    present = {name for name, _path in candidates}
    missing = [n for n in allowlist or () if n not in present]
    if missing:
        raise InstallError(f"bundle {bundle_name}: components not found: {', '.join(missing)}")

    installed_skills: list[str] = []
    installed_plugins: list[str] = []
    installed_apps: list[str] = []
    for name, entry in candidates:
        has_skill = (entry / "SKILL.md").is_file()
        has_plugin = (entry / "plugin.toml").is_file()
        has_app = (entry / "app.toml").is_file()
        kinds = sum(1 for present in (has_skill, has_plugin, has_app) if present)
        if kinds > 1:
            raise InstallError(f"bundle {bundle_name}: component {name!r} has multiple component manifests")
        if has_skill:
            _install_skill(entry, skills_dir / name, override=override,
                           label=f"bundle {bundle_name} -> skill {name}")
            installed_skills.append(name)
        elif has_plugin:
            _install_plugin(entry, plugins_dir / name, override=override,
                            label=f"bundle {bundle_name} -> plugin {name}")
            installed_plugins.append(name)
        elif has_app:
            app_id = _validate_app_manifest(entry, label=f"bundle {bundle_name} -> app {name}")
            _install_component(entry, apps_dir / app_id, override=override,
                               label=f"bundle {bundle_name} -> app {name}")
            installed_apps.append(app_id)
        else:
            raise InstallError(f"bundle {bundle_name}: component {name!r} has no SKILL.md, plugin.toml or app.toml")
    _install_bundle_python_exports(bundle_root, manifest, packages_dir, installed_python_packages, bundle_name=bundle_name)
    _install_bundle_typescript_exports(bundle_root, manifest, packages_dir, installed_typescript_packages, bundle_name=bundle_name)
    return InstalledBundleComponents(skills=tuple(installed_skills), plugins=tuple(installed_plugins), apps=tuple(installed_apps))


def _validate_bundle_dependencies(bundle_manifests: dict[str, BundleManifest]) -> None:
    python_export_map = {bundle_name: {item.name for item in manifest.exports_python_packages} for bundle_name, manifest in bundle_manifests.items()}
    typescript_export_map = {bundle_name: {item.name for item in manifest.exports_typescript_packages} for bundle_name, manifest in bundle_manifests.items()}
    for bundle_name, manifest in bundle_manifests.items():
        for dependency in manifest.dependencies:
            target = bundle_manifests.get(dependency.bundle)
            if target is None:
                raise InstallError(f"bundle {bundle_name!r} depends on bundle {dependency.bundle!r}, but it is not selected")
            missing = [pkg for pkg in dependency.python_packages if pkg not in python_export_map.get(dependency.bundle, set())]
            if missing:
                raise InstallError(
                    f"bundle {bundle_name!r} depends on python package(s) {', '.join(missing)} from bundle {dependency.bundle!r}, "
                    f"but bundle {dependency.bundle!r} does not export them"
                )
            missing = [pkg for pkg in dependency.typescript_packages if pkg not in typescript_export_map.get(dependency.bundle, set())]
            if missing:
                raise InstallError(
                    f"bundle {bundle_name!r} depends on typescript package(s) {', '.join(missing)} from bundle {dependency.bundle!r}, "
                    f"but bundle {dependency.bundle!r} does not export them"
                )
    graph = {bundle_name: [dependency.bundle for dependency in manifest.dependencies] for bundle_name, manifest in bundle_manifests.items()}
    visiting: list[str] = []
    visited: set[str] = set()

    def visit(node: str) -> None:
        if node in visited:
            return
        if node in visiting:
            cycle = visiting[visiting.index(node):] + [node]
            raise InstallError(f"bundle dependency cycle: {' -> '.join(cycle)}")
        visiting.append(node)
        for dep in graph.get(node, []):
            visit(dep)
        visiting.pop()
        visited.add(node)

    for node in graph:
        visit(node)


def _install_bundle_python_exports(bundle_root: Path, manifest: BundleManifest, packages_dir: Path,
                                   installed_python_packages: dict[str, tuple[str, Path]], *, bundle_name: str) -> None:
    root = bundle_root.resolve()
    python_src_root = packages_dir / "python" / "src"
    python_src_root.mkdir(parents=True, exist_ok=True)
    for export in manifest.exports_python_packages:
        src = (bundle_root / export.path).resolve()
        try:
            src.relative_to(root)
        except ValueError as exc:
            raise InstallError(f"bundle {bundle_name}: exported package path escapes bundle root: {export.path}") from exc
        if not src.is_dir():
            raise InstallError(f"bundle {bundle_name}: exported python package path not found: {export.path}")
        if src.name != export.name:
            raise InstallError(f"bundle {bundle_name}: exported package name {export.name!r} must match directory name {src.name!r}")
        if not (src / "__init__.py").is_file():
            raise InstallError(f"bundle {bundle_name}: exported python package {export.name!r} is missing __init__.py")
        if export.name in installed_python_packages:
            existing_bundle, existing_path = installed_python_packages[export.name]
            raise InstallError(
                f"bundle {bundle_name}: exported python package {export.name!r} conflicts with bundle {existing_bundle!r} at {existing_path}"
            )
        dst = python_src_root / export.name
        if dst.exists():
            raise InstallError(f"bundle {bundle_name}: exported python package target already exists: {dst}")
        _copytree(src, dst)
        installed_python_packages[export.name] = (bundle_name, src)


def _install_distro_python_exports(distro_root: Path, exports: tuple[cfg.PythonPackageExport, ...], packages_dir: Path,
                                   installed_python_packages: dict[str, tuple[str, Path]], *, distro_name: str) -> None:
    root = distro_root.resolve()
    python_src_root = packages_dir / "python" / "src"
    python_src_root.mkdir(parents=True, exist_ok=True)
    for export in exports:
        src = (distro_root / export.path).resolve()
        try:
            src.relative_to(root)
        except ValueError as exc:
            raise InstallError(f"distro {distro_name}: exported python package path escapes distro root: {export.path}") from exc
        if not src.is_dir():
            raise InstallError(f"distro {distro_name}: exported python package path not found: {export.path}")
        init = src / "__init__.py"
        if not init.is_file():
            raise InstallError(f"distro {distro_name}: exported python package {export.name!r} is missing __init__.py")
        if export.name in installed_python_packages:
            existing_owner, existing_path = installed_python_packages[export.name]
            raise InstallError(
                f"distro {distro_name}: exported python package {export.name!r} conflicts with {existing_owner!r} at {existing_path}"
            )
        dst = python_src_root / export.name
        if dst.exists():
            raise InstallError(f"distro {distro_name}: exported python package target already exists: {dst}")
        _copytree(src, dst)
        installed_python_packages[export.name] = (f"distro {distro_name}", src)


def _install_bundle_typescript_exports(bundle_root: Path, manifest: BundleManifest, packages_dir: Path,
                                       installed_typescript_packages: dict[str, tuple[str, Path]], *, bundle_name: str) -> None:
    root = bundle_root.resolve()
    typescript_root = packages_dir / "typescript"
    typescript_root.mkdir(parents=True, exist_ok=True)
    for export in manifest.exports_typescript_packages:
        src = (bundle_root / export.path).resolve()
        try:
            src.relative_to(root)
        except ValueError as exc:
            raise InstallError(f"bundle {bundle_name}: exported typescript package path escapes bundle root: {export.path}") from exc
        if not src.is_dir():
            raise InstallError(f"bundle {bundle_name}: exported typescript package path not found: {export.path}")
        package_json = src / "package.json"
        if not package_json.is_file():
            raise InstallError(f"bundle {bundle_name}: exported typescript package {export.name!r} is missing package.json")
        if export.name in installed_typescript_packages:
            existing_bundle, existing_path = installed_typescript_packages[export.name]
            raise InstallError(
                f"bundle {bundle_name}: exported typescript package {export.name!r} conflicts with bundle {existing_bundle!r} at {existing_path}"
            )
        dst = typescript_root / _typescript_package_path(export.name)
        if dst.exists():
            raise InstallError(f"bundle {bundle_name}: exported typescript package target already exists: {dst}")
        _copytree(src, dst)
        installed_typescript_packages[export.name] = (bundle_name, src)


def _typescript_package_path(name: str) -> Path:
    return Path(*name.split("/"))


def _bundle_component_candidates(bundle_root: Path, manifest: BundleManifest, *,
                                 bundle_name: str) -> list[tuple[str, Path]]:
    if manifest.components is not None:
        candidates: list[tuple[str, Path]] = []
        for component in manifest.components:
            entry = bundle_root / component
            if not entry.is_dir():
                raise InstallError(f"bundle {bundle_name}: component not found: {component}")
            candidates.append((component, entry))
        return candidates

    candidates = []
    for entry in sorted(bundle_root.iterdir()):
        if not entry.is_dir():
            continue
        if entry.name in IGNORE_NAMES or entry.name.startswith("."):
            continue
        if (entry / "SKILL.md").is_file() or (entry / "plugin.toml").is_file() or (entry / "app.toml").is_file():
            candidates.append((entry.name, entry))
    return candidates


# ── exposing current generation through stable paths ──────────────────────


def _expose_current(home: Path, distro_name: str) -> None:
    root = gens.distro_root(home, distro_name)
    for entry in ("skills", "plugins", "apps", "templates", "packages"):
        link = root / entry
        links.remove_path(link)
        links.create_directory_reference(link, Path("current") / entry)


def _set_active(home: Path, distro_name: str, *, expose_global_boot: bool = True) -> None:
    distrib_root = home / "distrib"
    distrib_root.mkdir(parents=True, exist_ok=True)
    active = distrib_root / "active"
    links.remove_path(active)
    links.create_directory_reference(active, Path(distro_name))



def _refresh_runtime_surface(home: Path, *, tenant: str | None = None) -> None:
    for obsolete in (home / "drivers", home / "gateways", home / "_lib"):
        links.remove_path(obsolete)
    _link_runtime(home / "distrib" / "active" / "packages", home / "packages")
    _link_runtime(home / "distrib" / "active" / "apps", home / "apps")
    _link_runtime(home / "distrib" / "active" / "templates", home / "templates")
    _link_runtime(home / "distrib" / "active" / "plugins", home / "plugins")
    _link_runtime(home / "distrib" / "active" / "skills", home / "skills")
    for tenant_dir in _tenant_roots(home, tenant=tenant):
        _refresh_tenant_runtime_surface(home, tenant_dir)
    _touch_reload_trigger(home, tenant=tenant)


def _tenant_roots(home: Path, *, tenant: str | None = None) -> list[Path]:
    tenants = home / "tenants"
    if not tenants.is_dir():
        return []
    roots = [entry for entry in sorted(tenants.iterdir()) if entry.is_dir() and not entry.name.startswith(".")]
    if tenant is None:
        return roots
    return [entry for entry in roots if entry.name == tenant]


def _refresh_tenant_runtime_surface(home: Path, tenant_dir: Path) -> None:
    obsolete = tenant_dir / "_lib"
    links.remove_path(obsolete)
    _link_runtime(home / "distrib" / "active" / "packages", tenant_dir / "packages")
    _link_runtime(home / "distrib" / "active" / "apps", tenant_dir / "apps")
    _link_runtime(home / "distrib" / "active" / "templates", tenant_dir / "templates")
    _link_runtime(home / "distrib" / "active" / "plugins", tenant_dir / "plugins")
    _link_runtime(home / "distrib" / "active" / "skills", tenant_dir / "skills")


def touch_reload_trigger(home: Path, *, tenant: str | None = None) -> None:
    _touch_reload_trigger(home, tenant=tenant)


def _touch_reload_trigger(home: Path, *, tenant: str | None = None) -> None:
    """Signal a running ``tabula serve`` to reload plugins.

    Touches ``$TABULA_HOME/run/reload.touch`` atomically. The kernel polls
    this file's mtime and calls ``Hub.ReloadPlugins`` when it changes. If
    the kernel is not running the file simply sits there and is read on
    next start as a no-op (current mtime is recorded as the baseline).
    """
    run_dir = home / "run"
    try:
        run_dir.mkdir(parents=True, exist_ok=True)
    except OSError:
        return
    trigger = run_dir / "reload.touch"
    tmp = run_dir / ".reload.touch.tmp"
    try:
        lines = [f"time={time.time()}"]
        if tenant:
            lines.append(f"tenant={tenant}")
        tmp.write_text("\n".join(lines) + "\n", encoding="utf-8")
        os.replace(tmp, trigger)
    except OSError:
        # Best-effort; reinstall succeeded even if the signal didn't.
        try:
            tmp.unlink()
        except OSError:
            pass


def _link_runtime(src_dir: Path, dst_dir: Path, preserve: set[str] | None = None) -> None:
    preserve = preserve or set()
    if links.is_reference(dst_dir) or (dst_dir.exists() and not dst_dir.is_dir()):
        links.remove_path(dst_dir)
    dst_dir.mkdir(parents=True, exist_ok=True)
    for existing in list(dst_dir.iterdir()):
        if existing.name in preserve:
            continue
        links.remove_path(existing)
    if not src_dir.is_dir():
        return
    for entry in src_dir.iterdir():
        if entry.name in preserve:
            continue
        target = dst_dir / entry.name
        relative = Path(os.path.relpath(entry, start=dst_dir))
        if entry.is_dir():
            links.create_directory_reference(target, relative)
        else:
            links.create_file_reference(target, relative)


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

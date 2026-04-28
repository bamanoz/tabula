"""Composition pipeline: resolve sources -> stage generation -> atomic switch."""
from __future__ import annotations

import hashlib
import os
import shutil
import tomllib
from dataclasses import dataclass, replace
from pathlib import Path

from . import config as cfg
from . import generations as gens
from . import lock as lockmod
from . import sources as srcmod
from .cache import GitCache
from .manifest import BundleManifest, ManifestError, load_bundle_manifest
from .semver import Constraint, Version, VersionError
from .sources import GitSource, LocalSource, Source


IGNORE_NAMES = {"__pycache__", ".pytest_cache", ".git", ".DS_Store"}


def _ignored(_dir: str, names: list[str]) -> set[str]:
    return {n for n in names if n in IGNORE_NAMES or n.endswith(".pyc")}


def _copytree(src: Path, dst: Path) -> None:
    dst.parent.mkdir(parents=True, exist_ok=True)
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


@dataclass(frozen=True)
class InstalledBundleComponents:
    skills: tuple[str, ...] = ()
    plugins: tuple[str, ...] = ()
    clients: tuple[str, ...] = ()


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
    plugins_dir = staging / "plugins"
    plugins_dir.mkdir(parents=True, exist_ok=True)
    clients_dir = staging / "clients"
    clients_dir.mkdir(parents=True, exist_ok=True)
    lib_dir = staging / "_lib"
    lib_dir.mkdir(parents=True, exist_ok=True)

    cache = GitCache(plan.home / "cache")
    prior_lock = lockmod.load(gens.distro_root(plan.home, distro.name) / "distro.lock.json")
    new_lock = lockmod.Lock(distro=distro.name)
    installed_lib_roots: set[Path] = set()

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

    for entry in distro.bundles:
        bundle_names = _bundle_update_names(entry, prior_lock)
        resolved_dir, lock_entry = _resolve(entry.source, distro.path, cache, plan,
                                           prior=_prior_bundle(prior_lock, entry.name),
                                           lock_names=bundle_names)
        manifest = load_bundle_manifest(resolved_dir)
        _check_bundle_compat(entry.name, manifest, kernel_version)
        if manifest.version is not None:
            lock_entry.version = str(manifest.version)
        installed = _install_bundle(resolved_dir, skills_dir, plugins_dir, clients_dir, lib_dir, installed_lib_roots, manifest=manifest,
                                     allowlist=entry.components, override=entry.override,
                                     bundle_name=entry.name)
        new_lock.bundles[entry.name] = lock_entry
        for name in installed.skills:
            new_lock.skills[name] = replace(lock_entry)
        for name in installed.plugins:
            new_lock.plugins[name] = replace(lock_entry)
        for name in installed.clients:
            new_lock.clients[name] = replace(lock_entry)

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
    src = srcmod.parse(uri, base_dir=base_dir)

    if isinstance(src, LocalSource):
        if not src.path.is_dir():
            raise InstallError(f"local source not a directory: {src.path}")
        return src.path, lockmod.LockEntry(source=uri, resolved_path=str(src.path),
                                           fetched_at=lockmod.now_iso())

    assert isinstance(src, GitSource)
    if prior is not None and prior.source == uri and prior.resolved_sha and _should_use_lock_for(lock_names or (uri,), plan):
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
    if not (src / "SKILL.md").is_file():
        raise InstallError(f"{label}: missing SKILL.md")
    _validate_skill_manifest_tools(src / "SKILL.md", label=label)
    _install_component(src, dst, override=override, label=label)


def _validate_skill_manifest_tools(path: Path, *, label: str) -> None:
    """Validate advertised SKILL.md tools without requiring a full YAML parser.

    Tabula skills use Anthropic-style YAML frontmatter. For this installer gate we
    only need the migration-critical fields in top-level ``tools[]`` entries:
    advertised tools must have non-empty ``name`` and ``exec``. Skills without a
    frontmatter ``tools`` block remain valid no-tool/legacy components, but they
    do not count as migrated per-call skills in the external evidence matrix.
    """
    frontmatter = _extract_skill_frontmatter(path)
    if frontmatter is None:
        return
    tools = _parse_skill_frontmatter_tools(frontmatter, label=label)
    seen: dict[str, int] = {}
    for idx, tool in enumerate(tools):
        name = (tool.get("name") or "").strip()
        exec_cmd = (tool.get("exec") or "").strip()
        if not name:
            raise InstallError(f"{label}: tools[{idx}].name is required for tools[].exec migration")
        if not exec_cmd:
            raise InstallError(f"{label}: tools[{idx}].exec is required for tools[].exec migration")
        if name in seen:
            raise InstallError(
                f"{label}: tools[{idx}].name duplicates tools[{seen[name]}].name {name!r}"
            )
        seen[name] = idx


def _extract_skill_frontmatter(path: Path) -> str | None:
    text = path.read_text(encoding="utf-8")
    lines = text.splitlines()
    if not lines or lines[0].strip() != "---":
        return None
    collected: list[str] = []
    for line in lines[1:]:
        if line.strip() == "---":
            return "\n".join(collected)
        collected.append(line)
    raise InstallError(f"{path.name}: unterminated YAML frontmatter")


def _parse_skill_frontmatter_tools(frontmatter: str, *, label: str) -> list[dict[str, str]]:
    lines = frontmatter.splitlines()
    for i, line in enumerate(lines):
        if line.startswith((" ", "\t")) or line.lstrip().startswith("#"):
            continue
        key, sep, value = line.partition(":")
        if sep and key.strip() == "tools":
            value = value.strip()
            if value.startswith("#"):
                value = ""
            if value == "":
                return _parse_skill_tools_block(lines[i + 1:], label=label)
            if value == "[]":
                return []
            if value in ("null", "~"):
                return []
            raise InstallError(f"{label}: tools must be a YAML list with tools[].name and tools[].exec")
    return []


def _parse_skill_tools_block(lines: list[str], *, label: str) -> list[dict[str, str]]:
    tools: list[dict[str, str]] = []
    current: dict[str, str] | None = None
    list_indent: int | None = None
    saw_indented_content = False
    for line in lines:
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        if not line.startswith((" ", "\t")):
            break
        saw_indented_content = True
        indent = len(line) - len(line.lstrip(" "))
        stripped = stripped.split(" #", 1)[0].strip()
        if stripped.startswith("- "):
            if list_indent is None:
                list_indent = indent
            if indent > list_indent:
                continue
            if indent < list_indent:
                break
            if current is not None:
                tools.append(current)
            current = {}
            item = stripped[2:].strip()
            if item:
                _parse_skill_tool_scalar(item, current, label=label)
            continue
        if list_indent is not None and indent > list_indent + 2:
            continue
        if current is None:
            raise InstallError(f"{label}: tools must be a YAML list with tools[].name and tools[].exec")
        _parse_skill_tool_scalar(stripped, current, label=label)
    if current is not None:
        tools.append(current)
    if saw_indented_content and not tools:
        raise InstallError(f"{label}: tools must be a YAML list with tools[].name and tools[].exec")
    return tools


def _parse_skill_tool_scalar(text: str, target: dict[str, str], *, label: str) -> None:
    key, sep, value = text.partition(":")
    if not sep:
        raise InstallError(f"{label}: invalid tools[] entry; expected key: value")
    key = key.strip()
    if key not in {"name", "exec"}:
        return
    target[key] = _strip_yaml_scalar(value.strip()).strip()


def _strip_yaml_scalar(value: str) -> str:
    if len(value) >= 2 and value[0] == value[-1] and value[0] in {'"', "'"}:
        return value[1:-1]
    return value


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


def _install_client(src: Path, dst: Path, *, override: bool, label: str) -> None:
    _validate_client_manifest(src, label=label)
    _install_component(src, dst, override=override, label=label)


def _validate_client_manifest(src: Path, *, label: str) -> None:
    manifest = src / "client.toml"
    if not manifest.is_file():
        raise InstallError(f"{label}: missing client.toml")
    try:
        with manifest.open("rb") as f:
            data = tomllib.load(f)
    except tomllib.TOMLDecodeError as exc:
        raise InstallError(f"{label}: invalid client.toml: {exc}") from exc

    client_id = data.get("id")
    runtime = data.get("runtime")
    entry = data.get("entry")
    if not isinstance(client_id, str) or not client_id.strip():
        raise InstallError(f"{label}: client.toml id is required")
    if client_id != src.name:
        raise InstallError(f"{label}: client.toml id {client_id!r} must match directory name {src.name!r}")
    if runtime not in {"python"}:
        raise InstallError(f"{label}: unsupported client runtime {runtime!r}")
    if not isinstance(entry, str) or not entry.strip():
        raise InstallError(f"{label}: client.toml entry is required")
    entry_path = (src / entry).resolve()
    try:
        entry_path.relative_to(src.resolve())
    except ValueError as exc:
        raise InstallError(f"{label}: client entry escapes component directory") from exc
    if not entry_path.is_file():
        raise InstallError(f"{label}: client entry does not exist: {entry}")

    bus = data.get("bus")
    if bus is not None:
        if not isinstance(bus, dict):
            raise InstallError(f"{label}: [bus] must be a TOML table")
        for key in ("sends", "receives"):
            values = bus.get(key)
            if values is not None and (not isinstance(values, list) or not all(isinstance(item, str) for item in values)):
                raise InstallError(f"{label}: bus.{key} must be an array of strings")


def _install_bundle(bundle_root: Path, skills_dir: Path, plugins_dir: Path, clients_dir: Path, lib_dir: Path,
                    installed_lib_roots: set[Path], *,
                    manifest: BundleManifest, allowlist: tuple[str, ...] | None,
                    override: bool, bundle_name: str) -> InstalledBundleComponents:
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
    installed_clients: list[str] = []
    for name, entry in candidates:
        has_skill = (entry / "SKILL.md").is_file()
        has_plugin = (entry / "plugin.toml").is_file()
        has_client = (entry / "client.toml").is_file()
        kinds = sum(1 for present in (has_skill, has_plugin, has_client) if present)
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
        elif has_client:
            _install_client(entry, clients_dir / name, override=override,
                            label=f"bundle {bundle_name} -> client {name}")
            installed_clients.append(name)
        else:
            raise InstallError(f"bundle {bundle_name}: component {name!r} has no SKILL.md, plugin.toml or client.toml")
    _install_bundle_lib(bundle_root, lib_dir, installed_lib_roots, override=override, bundle_name=bundle_name)
    return InstalledBundleComponents(skills=tuple(installed_skills), plugins=tuple(installed_plugins), clients=tuple(installed_clients))


def _install_bundle_lib(bundle_root: Path, lib_dir: Path, installed_lib_roots: set[Path], *,
                        override: bool, bundle_name: str) -> None:
    roots = []
    for src in (bundle_root / "_lib", bundle_root.parent / "_lib"):
        if src.is_dir() and src not in roots:
            roots.append(src)
    for src in roots:
        resolved = src.resolve()
        if resolved in installed_lib_roots:
            continue
        _install_lib_root(src, lib_dir, override=override, bundle_name=bundle_name)
        installed_lib_roots.add(resolved)


def _install_lib_root(src: Path, lib_dir: Path, *, override: bool, bundle_name: str) -> None:
    for entry in sorted(src.iterdir()):
        if not entry.is_dir() or entry.name.startswith(".") or entry.name in IGNORE_NAMES:
            continue
        dst = lib_dir / entry.name
        if dst.exists() and _trees_equal(entry, dst):
            continue
        _install_component(entry, dst, override=override, label=f"bundle {bundle_name} -> _lib/{entry.name}")


def _trees_equal(a: Path, b: Path) -> bool:
    if not a.is_dir() or not b.is_dir():
        return False
    return _fingerprint_tree(a) == _fingerprint_tree(b)


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
        if (entry / "SKILL.md").is_file() or (entry / "plugin.toml").is_file() or (entry / "client.toml").is_file():
            candidates.append((entry.name, entry))
    return candidates


# ── exposing current generation through stable paths ──────────────────────


def _expose_current(home: Path, distro_name: str) -> None:
    root = gens.distro_root(home, distro_name)
    for entry in ("boot.py", "skills", "plugins", "clients", "templates", "_lib"):
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
    for obsolete in (home / "drivers", home / "gateways"):
        if obsolete.exists() or obsolete.is_symlink():
            if obsolete.is_dir() and not obsolete.is_symlink():
                shutil.rmtree(obsolete)
            else:
                obsolete.unlink(missing_ok=True)
    _link_runtime(home / "distrib" / "active" / "_lib", home / "_lib")
    _link_runtime(home / "distrib" / "active" / "clients", home / "clients")
    _link_runtime(home / "distrib" / "active" / "templates", home / "templates")
    _link_runtime(home / "distrib" / "active" / "plugins", home / "plugins")
    _link_runtime(home / "distrib" / "active" / "skills", home / "skills")


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

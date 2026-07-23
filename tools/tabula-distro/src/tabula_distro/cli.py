"""CLI entrypoint for ``tabula-distro``."""
from __future__ import annotations

import argparse
import contextlib
import json
import os
import sys
from collections.abc import Iterator
from pathlib import Path

from tabula_distro import paths

from . import __version__
from . import tenant_bindings as bindmod
from . import config as cfg
from . import generations as gens
from . import install as installmod
from . import links
from . import lock as lockmod
from . import requirements as reqmod
from . import runtime_config as runtimecfg
from . import sources as srcmod
from . import tenant_materializer as tenantmod


def _default_home() -> Path:
    return paths.tabula_home()


def _source_arg(value: str) -> str:
    """Accept a local directory path or a git+... URI.

    Validation for local paths happens early to give a better error; git sources
    are passed through and resolved by the installer.
    """
    if value.startswith("git+") or value.startswith("local:"):
        return value
    p = Path(value).expanduser()
    if not p.exists():
        raise argparse.ArgumentTypeError(f"distro source not found: {value}")
    if not p.is_dir():
        raise argparse.ArgumentTypeError(f"distro source is not a directory: {value}")
    return str(p.resolve())


def main(argv: list[str] | None = None, *, prog: str = "tabula-distro") -> int:
    parser = argparse.ArgumentParser(prog=prog, description="Tabula installer")
    parser.add_argument("--version", action="version", version=f"%(prog)s {__version__}")
    parser.add_argument("--home", default=None, help="override $TABULA_HOME")
    sub = parser.add_subparsers(dest="cmd", required=True)

    if prog == "tabula-install":
        p_distro = sub.add_parser("distro", help="install and manage distros")
        distro_sub = p_distro.add_subparsers(dest="distro_cmd", required=True)
        _add_distro_commands(distro_sub)
        _add_use_command(sub, help_text="switch to a distro")
        p_tenant = sub.add_parser("tenant", help="install project-scoped tenants")
        tenant_sub = p_tenant.add_subparsers(dest="tenant_cmd", required=True)
        p_tenant_install = tenant_sub.add_parser("install", help="install and materialize a tenant")
        p_tenant_install.add_argument("source", type=_source_arg, help="full Git URI or local distro path")
        p_tenant_install.add_argument("--id", required=True, help="local tenant id")
        p_tenant_install.add_argument("--root", required=True, help="project workspace root")
        p_tenant_install.add_argument("--values", default="", help="distro-owned values TOML")
        p_tenant_install.add_argument("--display-name", default="", help="local tenant display name")
        p_tenant_install.add_argument("--frozen", action="store_true", help="require cached sources; no network access")
        p_tenant_install.add_argument("--update", action="store_true", help="resolve latest revisions")
        p_tenant_install.add_argument("--keep-generations", type=int, default=5)
        p_tenant_install.add_argument("--replace-binding", action="store_true", help="replace a directory binding owned by another tenant")
        p_tenant_install.set_defaults(func=_cmd_tenant_install)

        p_tenant_materialize = tenant_sub.add_parser("materialize", help="re-materialize an installed tenant")
        p_tenant_materialize.add_argument("tenant", help="installed tenant id")
        p_tenant_materialize.add_argument("--root", required=True, help="project workspace root")
        p_tenant_materialize.add_argument("--values", required=True, help="distro-owned values TOML")
        p_tenant_materialize.set_defaults(func=_cmd_tenant_materialize)

        p_tenant_bindings = tenant_sub.add_parser("bindings", help="print tenant binding registry")
        p_tenant_bindings.add_argument("--json", action="store_true", help="print JSON output")
        p_tenant_bindings.set_defaults(func=_cmd_tenant_bindings)

        p_tenant_bind = tenant_sub.add_parser("bind", help="bind an installed tenant")
        p_tenant_bind.add_argument("tenant", help="installed tenant id")
        bind_target = p_tenant_bind.add_mutually_exclusive_group(required=True)
        bind_target.add_argument("--directory", help="directory root to bind")
        bind_target.add_argument("--default", action="store_true", help="set fallback binding")
        p_tenant_bind.add_argument("--replace-binding", action="store_true", help="replace a binding owned by another tenant")
        p_tenant_bind.set_defaults(func=_cmd_tenant_bind)

        p_tenant_unbind = tenant_sub.add_parser("unbind", help="remove a directory binding")
        p_tenant_unbind.add_argument("--directory", required=True, help="directory root to unbind")
        p_tenant_unbind.set_defaults(func=_cmd_tenant_unbind)
    else:
        _add_distro_commands(sub)

    args = parser.parse_args(argv)
    home = Path(args.home).expanduser().resolve() if args.home else _default_home()
    home.mkdir(parents=True, exist_ok=True)
    try:
        return args.func(args, home)
    except (installmod.InstallError, cfg.ConfigError, lockmod.LockError, reqmod.RequirementsError, bindmod.BindingError, tenantmod.TenantMaterializationError) as exc:
        print(f"{prog}: {exc}", file=sys.stderr)
        return 1


def _add_distro_commands(sub: argparse._SubParsersAction[argparse.ArgumentParser]) -> None:
    p_install = sub.add_parser("install", help="install or re-apply a distro")
    p_install.add_argument("source", type=_source_arg, help="distro source directory")
    p_install.add_argument("--name", default="", help="override installed distro name")
    p_install.add_argument("--frozen", action="store_true",
                           help="require lockfile; no network access")
    p_install.add_argument("--update", action="store_true",
                           help="ignore pinned lock; re-resolve all git refs")
    p_install.add_argument("--update-only", action="append", default=[],
                           help="update only the named bundle/skill/plugin/component (repeatable)")
    p_install.add_argument("--tenant", default="",
                           help="refresh runtime surface only for the named tenant")
    p_install.add_argument("--keep-generations", type=int, default=5)
    p_install.set_defaults(func=_cmd_install)

    p_update = sub.add_parser("update", help="update pinned sources (alias for install --update)")
    p_update.add_argument("--name", required=False, default="",
                          help="distro name (defaults to the active one)")
    p_update.add_argument("name_positional", nargs="?", help=argparse.SUPPRESS)
    p_update.add_argument("--only", action="append", default=[],
                          help="only update this bundle/skill/plugin/component (repeatable)")
    p_update.set_defaults(func=_cmd_update)

    p_reinstall = sub.add_parser("reinstall", help="re-apply an installed distro from its saved source")
    p_reinstall.add_argument("name", nargs="?", default="", help="distro name (default: active)")
    p_reinstall.add_argument("--update", action="store_true",
                             help="ignore pinned lock; re-resolve all git refs")
    p_reinstall.add_argument("--update-only", action="append", default=[],
                             help="update only the named bundle/skill/plugin/component (repeatable)")
    p_reinstall.add_argument("--tenant", default="",
                             help="refresh runtime surface only for the named tenant")
    p_reinstall.add_argument("--keep-generations", type=int, default=5)
    p_reinstall.set_defaults(func=_cmd_reinstall)

    _add_use_command(sub, help_text="switch to a distro by reinstalling saved source or a local checkout")

    p_rollback = sub.add_parser("rollback", help="switch to a previous generation")
    p_rollback.add_argument("name", nargs="?", default="", help="distro name (default: active)")
    p_rollback.add_argument("--to", type=int, default=None, help="specific generation number")
    p_rollback.set_defaults(func=_cmd_rollback)

    p_list = sub.add_parser("list", help="list installed distros and generations")
    p_list.set_defaults(func=_cmd_list)

    p_lock = sub.add_parser("lock", help="print the lockfile of an installed distro as JSON")
    p_lock.add_argument("name", nargs="?", default="", help="distro name (default: active)")
    p_lock.set_defaults(func=_cmd_lock)

    p_gc = sub.add_parser("gc", help="garbage-collect unused cached git worktrees")
    p_gc.set_defaults(func=_cmd_gc)


def _cmd_tenant_materialize(args: argparse.Namespace, home: Path) -> int:
    tenantmod.rematerialize(home, args.tenant, Path(args.root), Path(args.values))
    print(f"materialized tenant {args.tenant}")
    return 0


def _cmd_tenant_install(args: argparse.Namespace, home: Path) -> int:
    result = tenantmod.install(
        args.source,
        home,
        tenant_id=args.id,
        project_root=Path(args.root),
        values_path=Path(args.values) if args.values else None,
        display_name=args.display_name,
        offline=bool(args.frozen),
        update=bool(args.update),
        keep_generations=args.keep_generations,
        replace_binding=bool(args.replace_binding),
    )
    print(f"installed tenant {args.id}")
    print(f"  distro: {result.distro.lock.distro} ({result.distro.generation.name})")
    print(f"  tenant: {result.tenant_dir}")
    print(f"  lock:   {result.tenant_dir / 'install.lock.json'}")
    if result.materialized:
        print("  materializer: ran")
    return 0


def _cmd_install(args: argparse.Namespace, home: Path) -> int:
    result = _install_with_local_alias_overrides(
        args.source, home,
        override_name=args.name or None,
        offline=args.frozen,
        update=args.update,
        update_only=tuple(args.update_only),
        tenant=args.tenant or None,
        keep_generations=args.keep_generations,
    )
    if not _sync_runtime_config_for_active_distro(home, result.generation.path):
        return 1
    _print_summary(home, result.lock.distro, result.generation, result.lock, changed=result.changed)
    return 0


def _cmd_update(args: argparse.Namespace, home: Path) -> int:
    distro_name = args.name or args.name_positional or _active_name(home)
    if not distro_name:
        print("tabula-distro: no active distro; pass a name", file=sys.stderr)
        return 2
    source = _active_source(home, distro_name)
    if source is None:
        print(f"tabula-distro: cannot locate source for distro {distro_name!r}; "
              f"re-run `tabula-distro install <source>` explicitly", file=sys.stderr)
        return 2
    result = _install_with_local_alias_overrides(
        source, home,
        override_name=distro_name,
        update=True,
        update_only=tuple(args.only),
    )
    if not _sync_runtime_config_for_active_distro(home, result.generation.path):
        return 1
    _print_summary(home, result.lock.distro, result.generation, result.lock, changed=result.changed)
    return 0


def _cmd_reinstall(args: argparse.Namespace, home: Path) -> int:
    distro_name = args.name or _active_name(home)
    if not distro_name:
        print("tabula-distro: no active distro; pass a name", file=sys.stderr)
        return 2
    source = _active_source(home, distro_name)
    if source is None:
        print(f"tabula-distro: cannot locate source for distro {distro_name!r}; "
              f"re-run `tabula-distro install <source>` explicitly", file=sys.stderr)
        return 2
    result = _install_with_local_alias_overrides(
        source, home,
        override_name=distro_name,
        update=bool(args.update),
        update_only=tuple(args.update_only),
        tenant=args.tenant or None,
        keep_generations=args.keep_generations,
    )
    if not _sync_runtime_config_for_active_distro(home, result.generation.path):
        return 1
    _print_summary(home, result.lock.distro, result.generation, result.lock, changed=result.changed)
    return 0


def _cmd_use(args: argparse.Namespace, home: Path) -> int:
    distro_name = args.name or _active_name(home)
    if not distro_name:
        print("tabula-distro: no active distro; pass a name", file=sys.stderr)
        return 2
    source = _active_source(home, distro_name)
    if source is None:
        source = _discover_local_distro_source(distro_name, source_root=args.source_root or None)
    if source is None:
        if args.source_root:
            candidate = Path(args.source_root).expanduser().resolve() / distro_name
            print(f"tabula-distro: distro source not found: {candidate}", file=sys.stderr)
            return 2
        print(
            f"tabula-distro: cannot locate source for distro {distro_name!r}; "
            f"install it once explicitly or run from a checkout with sibling tabula-distrib/{distro_name}",
            file=sys.stderr,
        )
        return 2
    result = _install_with_local_alias_overrides(
        source, home,
        override_name=distro_name,
        update=bool(args.update),
        update_only=tuple(args.update_only),
        tenant=args.tenant or None,
        keep_generations=args.keep_generations,
    )
    if not _sync_runtime_config_for_active_distro(home, result.generation.path):
        return 1
    _print_summary(home, result.lock.distro, result.generation, result.lock, changed=result.changed)
    return 0


def _add_use_command(sub: argparse._SubParsersAction[argparse.ArgumentParser], *, help_text: str) -> None:
    p_use = sub.add_parser("use", help=help_text)
    p_use.add_argument("name", nargs="?", default="", help="distro name (default: active)")
    p_use.add_argument("--source-root", default="",
                       help="directory that contains local distro checkouts by name")
    p_use.add_argument("--update", action="store_true",
                       help="ignore pinned lock; re-resolve all git refs")
    p_use.add_argument("--update-only", action="append", default=[],
                       help="update only the named bundle/skill/plugin/component (repeatable)")
    p_use.add_argument("--tenant", default="",
                       help="refresh runtime surface only for the named tenant")
    p_use.add_argument("--keep-generations", type=int, default=5)
    p_use.set_defaults(func=_cmd_use)


def _cmd_rollback(args: argparse.Namespace, home: Path) -> int:
    distro_name = args.name or _active_name(home)
    if not distro_name:
        print("tabula-distro: no active distro; pass a name", file=sys.stderr)
        return 2
    target = installmod.rollback(home, distro_name, to=args.to)
    print(f"{distro_name}: switched to generation {target.number} ({target.name})")
    return 0


def _cmd_list(args: argparse.Namespace, home: Path) -> int:
    distrib = home / "distrib"
    if not distrib.is_dir():
        print("(no distros installed)")
        return 0
    active = _active_name(home)
    rows: list[str] = []
    for entry in sorted(distrib.iterdir()):
        if entry.name == "active" or not entry.is_dir():
            continue
        gens_list = gens.list_generations(home, entry.name)
        current = gens.current_generation(home, entry.name)
        marker = "*" if entry.name == active else " "
        cur_label = f"gen {current.number} ({current.name})" if current else "(no current)"
        rows.append(f"  {marker} {entry.name:24s} {cur_label}  [{len(gens_list)} generations]")
    if not rows:
        print("(no distros installed)")
    else:
        print("installed distros:")
        print("\n".join(rows))
    return 0


def _cmd_lock(args: argparse.Namespace, home: Path) -> int:
    distro_name = args.name or _active_name(home)
    if not distro_name:
        print("tabula-distro: no active distro; pass a name", file=sys.stderr)
        return 2
    path = gens.distro_root(home, distro_name) / "distro.lock.json"
    lock = lockmod.load(path)
    if lock is None:
        print(f"tabula-distro: no lockfile at {path}", file=sys.stderr)
        return 1
    print(json.dumps(lock.to_json(), indent=2, sort_keys=True))
    return 0


def _cmd_gc(args: argparse.Namespace, home: Path) -> int:
    from .cache import GitCache
    cache = GitCache(home / "cache")
    keep: set[str] = set()
    distrib = home / "distrib"
    if distrib.is_dir():
        for entry in distrib.iterdir():
            if entry.name == "active" or not entry.is_dir():
                continue
            lock_path = entry / "distro.lock.json"
            lock = lockmod.load(lock_path)
            if lock is None:
                continue
            for lck in list(lock.bundles.values()) + list(lock.skills.values()) + list(lock.plugins.values()) + list(lock.apps.values()):
                if lck.resolved_sha:
                    keep.add(lck.resolved_sha)
    removed = cache.gc(keep)
    print(f"removed {len(removed)} cached worktree(s)")
    return 0


def _cmd_tenant_bindings(args: argparse.Namespace, home: Path) -> int:
    registry = bindmod.load(home)
    if args.json:
        print(json.dumps(registry.to_json(), indent=2, sort_keys=True))
        return 0
    if registry.default is not None:
        print(f"default: tenant={registry.default.tenant}")
    if registry.directories:
        print("directories:")
        for binding in registry.directories:
            print(f"  {binding.root} -> tenant={binding.tenant}")
    if registry.default is None and not registry.directories:
        print("(no tenant bindings)")
    return 0


def _cmd_tenant_bind(args: argparse.Namespace, home: Path) -> int:
    bindmod.require_tenant_exists(home, args.tenant)
    registry = bindmod.load(home)
    if args.default:
        registry = bindmod.bind_default(registry, args.tenant, replace=bool(args.replace_binding))
        bindmod.save(home, registry)
        print(f"bound default -> tenant={args.tenant}")
        return 0
    registry = bindmod.bind_directory(
        registry, args.directory, args.tenant, replace=bool(args.replace_binding)
    )
    bindmod.save(home, registry)
    print(f"bound {Path(args.directory).expanduser().resolve()} -> tenant={args.tenant}")
    return 0


def _cmd_tenant_unbind(args: argparse.Namespace, home: Path) -> int:
    registry = bindmod.unbind_directory(bindmod.load(home), args.directory)
    bindmod.save(home, registry)
    print(f"unbound {Path(args.directory).expanduser().resolve()}")
    return 0


def _active_name(home: Path) -> str:
    active = home / "distrib" / "active"
    target = links.resolve_reference(active)
    if target is None:
        return ""
    return target.name


def _active_source(home: Path, distro_name: str) -> str | None:
    """Return the original source URI that was used to install ``distro_name``.

    Reads it back from the lockfile written by a previous ``install``. Returns
    ``None`` if no lockfile exists yet.
    """
    lock_path = gens.distro_root(home, distro_name) / "distro.lock.json"
    lock = lockmod.load(lock_path)
    if lock is None:
        return None
    return lock.distro_source


def _discover_local_distro_source(distro_name: str, *, source_root: str | None) -> str | None:
    candidates: list[Path] = []
    if source_root:
        candidates.append(Path(source_root).expanduser().resolve() / distro_name)
    else:
        cwd = Path.cwd().resolve()
        for base in (cwd, *cwd.parents):
            candidates.append(base / "tabula-distrib" / distro_name)
    seen: set[Path] = set()
    for candidate in candidates:
        resolved = candidate.resolve()
        if resolved in seen:
            continue
        seen.add(resolved)
        if resolved.is_dir():
            return str(resolved)
    return None


def _install_with_local_alias_overrides(source: str, home: Path, *, base_dir: Path | None = None, **kwargs) -> installmod.InstallResult:
    with _temporary_local_alias_overrides(source, base_dir=base_dir):
        return installmod.install(source, home, base_dir=base_dir, **kwargs)


@contextlib.contextmanager
def _temporary_local_alias_overrides(source: str, *, base_dir: Path | None = None) -> Iterator[None]:
    overrides = _local_alias_overrides(source, base_dir=base_dir)
    previous: dict[str, str | None] = {key: os.environ.get(key) for key in overrides}
    try:
        for key, value in overrides.items():
            os.environ[key] = value
        yield
    finally:
        for key, value in previous.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value


def _local_alias_overrides(source: str, *, base_dir: Path | None = None) -> dict[str, str]:
    distro_dir = _local_distro_dir(source, base_dir=base_dir)
    if distro_dir is None:
        return {}
    repo_root = distro_dir.parent
    workspace_root = repo_root.parent
    distro = cfg.load(distro_dir)
    overrides: dict[str, str] = {}
    for alias, entry in distro.sources.items():
        env_name = cfg._source_alias_env_name(alias)
        if os.environ.get(env_name, "").strip():
            continue
        if not entry.source.startswith("git+"):
            continue
        candidate = (workspace_root / alias).resolve()
        if candidate.is_dir():
            overrides[env_name] = f"local:{candidate}"
    return overrides


def _local_distro_dir(source: str, *, base_dir: Path | None = None) -> Path | None:
    text = str(source).strip()
    if not text or text.startswith("git+"):
        return None
    if text.startswith("local:"):
        parsed = srcmod.parse(text, base_dir=base_dir or Path.cwd())
        if isinstance(parsed, srcmod.LocalSource):
            return parsed.path
        return None
    path = Path(text).expanduser().resolve()
    if not path.is_dir():
        return None
    return path


def _sync_runtime_config_for_active_distro(home: Path, boot_path: Path) -> bool:
    try:
        runtimecfg.sync_for_distro(home, boot_path)
    except runtimecfg.RuntimeConfigError as exc:
        print(f"tabula-distro: runtime config sync failed: {exc}", file=sys.stderr)
        return False
    return True


def _print_summary(home: Path, distro_name: str, gen, lock: lockmod.Lock, *, changed: bool = True) -> None:
    if changed:
        print(f"installed distro {distro_name} as generation {gen.number} ({gen.name})")
    else:
        print(f"distro {distro_name} unchanged at generation {gen.number} ({gen.name})")
    if lock.bundles:
        print("  bundles:")
        for name, entry in sorted(lock.bundles.items()):
            print(f"    {name:20s} {_describe_lock(entry)}")
    if lock.skills:
        print("  skills:")
        for name, entry in sorted(lock.skills.items()):
            print(f"    {name:20s} {_describe_lock(entry)}")
    if lock.plugins:
        print("  plugins:")
        for name, entry in sorted(lock.plugins.items()):
            print(f"    {name:20s} {_describe_lock(entry)}")
    if lock.apps:
        print("  apps:")
        for name, entry in sorted(lock.apps.items()):
            print(f"    {name:20s} {_describe_lock(entry)}")
    if not lock.bundles and not lock.skills and not lock.plugins and not lock.apps:
        print("  (no external sources)")


def _describe_lock(entry: lockmod.LockEntry) -> str:
    if entry.resolved_sha:
        ref = entry.resolved_ref or ""
        return f"{entry.source}  -> {entry.resolved_sha[:12]}" + (f"  ({ref})" if ref and ref != entry.resolved_sha else "")
    if entry.resolved_path:
        return f"{entry.source}  -> {entry.resolved_path}"
    return entry.source


if __name__ == "__main__":
    raise SystemExit(main())

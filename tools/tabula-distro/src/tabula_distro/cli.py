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
from . import app_bindings as bindmod
from . import app_manifest as appmod
from . import app_run as runmod
from . import config as cfg
from . import generations as gens
from . import install as installmod
from . import lock as lockmod
from . import requirements as reqmod
from . import runtime_config as runtimecfg
from . import sources as srcmod


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


def _app_manifest_arg(parser: argparse.ArgumentParser) -> None:
    parser.add_argument(
        "manifest",
        nargs="?",
        default=None,
        help="path to tabula.app.toml (defaults to ./tabula.app.toml, then ./.tabula/app.toml)",
    )


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
    else:
        _add_distro_commands(sub)

    p_app = sub.add_parser("app", help="work with runnable agent application manifests")
    app_sub = p_app.add_subparsers(dest="app_cmd", required=True)

    p_app_lock = app_sub.add_parser("lock", help="resolve and write an app lockfile")
    _app_manifest_arg(p_app_lock)
    p_app_lock.add_argument("--frozen", action="store_true", help="require existing lockfile; no network access")
    p_app_lock.add_argument("--update", action="store_true", help="refresh resolved app lock data")
    p_app_lock.set_defaults(func=_cmd_app_lock)

    p_app_audit = app_sub.add_parser("audit", help="print app manifest resolution summary")
    _app_manifest_arg(p_app_audit)
    p_app_audit.add_argument("--frozen", action="store_true", help="require existing lockfile; no network access")
    p_app_audit.add_argument("--update", action="store_true", help="refresh resolved app lock data for audit")
    p_app_audit.add_argument("--json", action="store_true", help="print JSON output")
    p_app_audit.set_defaults(func=_cmd_app_audit)

    p_app_apply = app_sub.add_parser("apply", help="materialize app metadata without launching")
    _app_manifest_arg(p_app_apply)
    p_app_apply.add_argument("--frozen", action="store_true", help="require existing lockfile; no network access")
    p_app_apply.add_argument("--update", action="store_true", help="refresh resolved app lock data")
    p_app_apply.set_defaults(func=_cmd_app_apply)

    p_app_install = app_sub.add_parser("install", help="install and bind an app without launching")
    _app_manifest_arg(p_app_install)
    p_app_install.add_argument("--frozen", action="store_true", help="require existing lockfile; no network access")
    p_app_install.add_argument("--update", action="store_true", help="refresh resolved app lock data")
    install_binding = p_app_install.add_mutually_exclusive_group()
    install_binding.add_argument("--global", dest="global_bind", action="store_true", help="install as the fallback app binding")
    install_binding.add_argument("--workspace", help="install for a specific workspace directory")
    install_binding.add_argument("--no-bind", action="store_true", help="install without changing app bindings")
    p_app_install.set_defaults(func=_cmd_app_install)

    p_app_prepare = app_sub.add_parser("prepare", help="install and materialize an app manifest without launching")
    _app_manifest_arg(p_app_prepare)
    p_app_prepare.add_argument("--frozen", action="store_true", help="require existing lockfile; no network access")
    p_app_prepare.add_argument("--update", action="store_true", help="refresh resolved app lock data")
    p_app_prepare.set_defaults(func=_cmd_app_prepare)

    p_app_run = app_sub.add_parser("run", help="apply and launch an app manifest")
    _app_manifest_arg(p_app_run)
    p_app_run.add_argument("--frozen", action="store_true", help="require existing lockfile; no network access")
    p_app_run.add_argument("--update", action="store_true", help="refresh resolved app lock data")
    p_app_run.add_argument("--dry-run", action="store_true", help="print launch plan without starting processes")
    p_app_run.add_argument("--foreground", action="store_true", help="run the managed app kernel in the foreground")
    p_app_run.add_argument("--tabula-bin", default="tabula", help="tabula binary to start managed kernels")
    p_app_run.set_defaults(func=_cmd_app_run)

    p_app_bindings = app_sub.add_parser("bindings", help="print app binding registry")
    p_app_bindings.add_argument("--json", action="store_true", help="print JSON output")
    p_app_bindings.set_defaults(func=_cmd_app_bindings)

    p_app_inspect = app_sub.add_parser("inspect", help="inspect an applied app tenant")
    p_app_inspect.add_argument("app", help="application id")
    p_app_inspect.add_argument("--json", action="store_true", help="print JSON output")
    p_app_inspect.set_defaults(func=_cmd_app_inspect)

    p_app_bind = app_sub.add_parser("bind", help="bind an app to a directory or default")
    p_app_bind.add_argument("app", help="application id")
    group = p_app_bind.add_mutually_exclusive_group(required=True)
    group.add_argument("--directory", help="directory root to bind")
    group.add_argument("--default", action="store_true", help="set fallback binding")
    p_app_bind.add_argument("--kernel", required=True, help="kernel id for this binding")
    p_app_bind.set_defaults(func=_cmd_app_bind)

    p_app_unbind = app_sub.add_parser("unbind", help="remove a directory binding")
    p_app_unbind.add_argument("--directory", required=True, help="directory root to unbind")
    p_app_unbind.set_defaults(func=_cmd_app_unbind)

    args = parser.parse_args(argv)
    home = Path(args.home).expanduser().resolve() if args.home else _default_home()
    home.mkdir(parents=True, exist_ok=True)
    try:
        return args.func(args, home)
    except (installmod.InstallError, cfg.ConfigError, lockmod.LockError, reqmod.RequirementsError, appmod.AppManifestError, bindmod.BindingError, runmod.AppRunError) as exc:
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
            for lck in list(lock.bundles.values()) + list(lock.skills.values()) + list(lock.plugins.values()) + list(lock.clients.values()):
                if lck.resolved_sha:
                    keep.add(lck.resolved_sha)
    removed = cache.gc(keep)
    print(f"removed {len(removed)} cached worktree(s)")
    return 0


def _cmd_app_lock(args: argparse.Namespace, home: Path) -> int:
    manifest, lock_path, lock = _resolve_app_lock(args, home)
    appmod.save_lock(lock_path, lock)
    print(f"wrote app lock {lock_path}")
    print(f"  app:    {manifest.application.id}")
    print(f"  distro: {lock.distro_name} ({lock.distro_source})")
    return 0


def _cmd_app_audit(args: argparse.Namespace, home: Path) -> int:
    manifest, lock_path, lock = _resolve_app_lock(args, home)
    if args.json:
        print(json.dumps(_app_audit_json(manifest, lock_path, lock, home), indent=2, sort_keys=True))
        return 0
    _print_app_audit(manifest, lock_path, lock, home)
    return 0


def _cmd_app_apply(args: argparse.Namespace, home: Path) -> int:
    manifest, lock_path, lock = _resolve_app_lock(args, home)
    appmod.save_lock(lock_path, lock)
    tenant_dir = appmod.materialize_metadata(manifest, lock, home)
    _install_with_local_alias_overrides(
        manifest.distro.source,
        home,
        base_dir=manifest.path.parent,
        offline=bool(args.frozen),
        update=bool(args.update),
        tenant=manifest.application.id,
        expose_global_boot=False,
    )
    materialized = appmod.run_materializer(manifest, home, lock_path=lock_path, phase="apply")
    appmod.compile_plugin_configs(home, tenant_dir)
    if materialized:
        installmod.touch_reload_trigger(home, tenant=manifest.application.id)
    registry = bindmod.apply_manifest_bindings(home, manifest.bindings)
    print(f"applied app {manifest.application.id}")
    print(f"  tenant: {tenant_dir}")
    print(f"  lock:   {lock_path}")
    if materialized:
        print("  materializer: ran")
    print(f"  bindings: {bindmod.registry_path(home)} ({len(registry.directories)} director{'y' if len(registry.directories) == 1 else 'ies'})")
    return 0


def _cmd_app_install(args: argparse.Namespace, home: Path) -> int:
    manifest, lock_path, lock = _resolve_app_lock(args, home)
    bindings = _install_binding_override(args, manifest)
    if bindings is not None:
        manifest = appmod.with_bindings(manifest, bindings)
        lock = appmod.with_lock_bindings(lock, bindings)
    appmod.save_lock(lock_path, lock)
    tenant_dir = appmod.materialize_metadata(manifest, lock, home)
    install_result = _install_with_local_alias_overrides(
        manifest.distro.source,
        home,
        base_dir=manifest.path.parent,
        offline=bool(args.frozen),
        update=bool(args.update),
        tenant=manifest.application.id,
        expose_global_boot=False,
    )
    materialized = appmod.run_materializer(manifest, home, lock_path=lock_path, phase="apply")
    appmod.compile_plugin_configs(home, tenant_dir)
    registry = bindmod.apply_manifest_bindings(home, manifest.bindings)
    runmod.write_runtime_config(manifest, home, distro_dir=install_result.generation.path)
    installmod.touch_reload_trigger(home, tenant=manifest.application.id)
    print(f"installed app {manifest.application.id}")
    print(f"  tenant: {tenant_dir}")
    print(f"  lock:   {lock_path}")
    if materialized:
        print("  materializer: ran")
    print(f"  bindings: {bindmod.registry_path(home)} ({_binding_summary(manifest.bindings, registry)})")
    return 0


def _cmd_app_prepare(args: argparse.Namespace, home: Path) -> int:
    manifest, lock_path, lock = _resolve_app_lock(args, home)
    appmod.save_lock(lock_path, lock)
    tenant_dir = appmod.materialize_metadata(manifest, lock, home)
    install_result = _install_with_local_alias_overrides(
        manifest.distro.source,
        home,
        base_dir=manifest.path.parent,
        offline=bool(args.frozen),
        update=bool(args.update),
        tenant=manifest.application.id,
        expose_global_boot=False,
    )
    materialized = appmod.run_materializer(manifest, home, lock_path=lock_path, phase="run", dry_run=True)
    appmod.compile_plugin_configs(home, tenant_dir)
    registry = bindmod.apply_manifest_bindings(home, manifest.bindings)
    runmod.write_runtime_config(manifest, home, distro_dir=install_result.generation.path)
    _ = install_result
    installmod.touch_reload_trigger(home, tenant=manifest.application.id)
    print(f"prepared app {manifest.application.id}")
    print(f"  tenant: {tenant_dir}")
    print(f"  lock:   {lock_path}")
    if materialized:
        print("  materializer: ran")
    print(f"  bindings: {bindmod.registry_path(home)} ({len(registry.directories)} director{'y' if len(registry.directories) == 1 else 'ies'})")
    _print_run_plan(runmod.plan(manifest), tenant_dir)
    return 0


def _install_binding_override(args: argparse.Namespace, manifest: appmod.AppManifest) -> appmod.Bindings | None:
    if getattr(args, "global_bind", False):
        return appmod.Bindings(
            default=appmod.DefaultBinding(app=manifest.application.id, kernel=manifest.kernel.id)
        )
    workspace = getattr(args, "workspace", None)
    if workspace:
        root = str(Path(workspace).expanduser().resolve())
        return appmod.Bindings(
            directories=(appmod.DirectoryBinding(root=root, app=manifest.application.id, kernel=manifest.kernel.id),)
        )
    if getattr(args, "no_bind", False):
        return appmod.Bindings()
    return None


def _binding_summary(bindings: appmod.Bindings, registry: bindmod.Registry) -> str:
    parts: list[str] = []
    if bindings.default is not None:
        parts.append("default")
    if bindings.directories:
        count = len(bindings.directories)
        parts.append(f"{count} director{'y' if count == 1 else 'ies'}")
    if not parts:
        parts.append("unchanged")
    count = len(registry.directories)
    return ", ".join(parts) + f"; registry has {count} director{'y' if count == 1 else 'ies'}"


def _cmd_app_run(args: argparse.Namespace, home: Path) -> int:
    manifest, lock_path, lock = _resolve_app_lock(args, home)
    appmod.save_lock(lock_path, lock)
    tenant_dir = appmod.materialize_metadata(manifest, lock, home)
    install_result = _install_with_local_alias_overrides(
        manifest.distro.source,
        home,
        base_dir=manifest.path.parent,
        offline=bool(args.frozen),
        update=bool(args.update),
        tenant=manifest.application.id,
        expose_global_boot=False,
    )
    materialized = appmod.run_materializer(manifest, home, lock_path=lock_path, phase="run", dry_run=bool(args.dry_run))
    appmod.compile_plugin_configs(home, tenant_dir)
    bindmod.apply_manifest_bindings(home, manifest.bindings)
    runmod.write_runtime_config(manifest, home, distro_dir=install_result.generation.path)
    _ = materialized
    installmod.touch_reload_trigger(home, tenant=manifest.application.id)
    plan = runmod.plan(manifest)
    _print_run_plan(plan, tenant_dir)
    if args.dry_run:
        return 0
    result = runmod.execute(manifest, home, tabula_bin=_resolve_tabula_bin_arg(args.tabula_bin, home), foreground=bool(args.foreground), boot_path=install_result.generation.path)
    if result.started_kernel:
        print(f"started kernel {result.plan.kernel_id}")
    elif result.reused_kernel:
        print(f"reused kernel {result.plan.kernel_id}")
    return 0


def _resolve_tabula_bin_arg(value: str, home: Path) -> str:
    if value != "tabula":
        return value
    installed = home / "bin" / "tabula"
    if installed.is_file():
        return str(installed)
    return value


def _cmd_app_bindings(args: argparse.Namespace, home: Path) -> int:
    registry = bindmod.load(home)
    if args.json:
        print(json.dumps(registry.to_json(), indent=2, sort_keys=True))
        return 0
    if registry.default is not None:
        print(f"default: app={registry.default.app} kernel={registry.default.kernel}")
    if registry.directories:
        print("directories:")
        for binding in registry.directories:
            print(f"  {binding.root} -> app={binding.app} kernel={binding.kernel}")
    if registry.default is None and not registry.directories:
        print("(no app bindings)")
    return 0


def _cmd_app_inspect(args: argparse.Namespace, home: Path) -> int:
    payload = _app_inspect_json(home, args.app)
    if args.json:
        print(json.dumps(payload, indent=2, sort_keys=True))
        return 0
    _print_app_inspect(payload)
    return 0


def _cmd_app_bind(args: argparse.Namespace, home: Path) -> int:
    bindmod.require_app_exists(home, args.app)
    registry = bindmod.load(home)
    if args.default:
        registry = bindmod.bind_default(registry, args.app, args.kernel)
        bindmod.save(home, registry)
        print(f"bound default -> app={args.app} kernel={args.kernel}")
        return 0
    registry = bindmod.bind_directory(registry, args.directory, args.app, args.kernel)
    bindmod.save(home, registry)
    print(f"bound {Path(args.directory).expanduser().resolve()} -> app={args.app} kernel={args.kernel}")
    return 0


def _cmd_app_unbind(args: argparse.Namespace, home: Path) -> int:
    registry = bindmod.unbind_directory(bindmod.load(home), args.directory)
    bindmod.save(home, registry)
    print(f"unbound {Path(args.directory).expanduser().resolve()}")
    return 0


def _resolve_app_lock(args: argparse.Namespace, home: Path) -> tuple[appmod.AppManifest, Path, appmod.AppLock]:
    manifest_path = _resolve_app_manifest_path(args.manifest)
    manifest = appmod.load(manifest_path, tabula_home=home)
    lock_path = appmod.default_lock_path(manifest_path)
    existing = appmod.load_lock(lock_path)
    frozen = bool(getattr(args, "frozen", False))
    update = bool(getattr(args, "update", False))
    if frozen:
        if existing is None:
            raise appmod.AppManifestError(f"--frozen requires app lockfile at {lock_path}")
        return manifest, lock_path, existing
    if existing is not None and not update:
        return manifest, lock_path, existing
    return manifest, lock_path, appmod.create_lock(manifest, home, offline=False, update=update)


def _resolve_app_manifest_path(value: str | None) -> Path:
    if value:
        path = Path(value).expanduser().resolve()
        if not path.is_file():
            raise appmod.AppManifestError(f"app manifest not found: {path}")
        return path
    candidates = (Path.cwd() / "tabula.app.toml", Path.cwd() / ".tabula" / "app.toml")
    for path in candidates:
        if path.is_file():
            return path.resolve()
    raise appmod.AppManifestError(
        "app manifest not found; expected ./tabula.app.toml or ./.tabula/app.toml, or pass an explicit manifest path"
    )


def _app_audit_json(manifest: appmod.AppManifest, lock_path: Path, lock: appmod.AppLock, home: Path) -> dict:
    tenant_dir = home / "tenants" / manifest.application.id
    runtime_cfg = _read_runtime_config(home / "config" / "runtime.toml", manifest.application.id)
    materializer = _materializer_status(tenant_dir, expected=appmod.materializer_declared(manifest, home))
    runtime_requirements = _runtime_requirements_for_manifest(manifest, home)
    issues = _inspect_issues(tenant_dir, runtime_cfg, materializer, runtime_requirements=runtime_requirements)
    applied_lock = appmod.load_lock(tenant_dir / "app.lock.json")
    if applied_lock is None:
        issues.append("applied app lock is missing")
    elif applied_lock.to_json() != lock.to_json():
        issues.append("applied app lock is stale")
    audit = {
        "application": {"id": manifest.application.id, "name": manifest.application.name},
        "manifest_path": str(manifest.path),
        "lock_path": str(lock_path),
        "applied": tenant_dir.is_dir(),
        "tenant_dir": str(tenant_dir),
        "distro": {"name": lock.distro_name, "source": lock.distro_source},
        "kernel": lock.kernel,
        "runtimes": list(lock.runtimes),
        "bindings": lock.bindings,
        "runtime_surface": runtime_cfg,
        "runtime_requirements": runtime_requirements,
        "materializer": materializer,
        "issues": issues,
    }
    return audit


def _app_inspect_json(home: Path, app_id: str) -> dict:
    tenant_dir = home / "tenants" / app_id
    if not tenant_dir.is_dir():
        raise bindmod.BindingError(f"unknown app {app_id!r}; apply the app manifest first")
    lock = appmod.load_lock(tenant_dir / "app.lock.json")
    registry = bindmod.load(home)
    runtime_cfg = _read_runtime_config(home / "config" / "runtime.toml", app_id)
    materializer = _materializer_status(tenant_dir, expected=_materializer_expected(home, lock))
    runtime_requirements = _runtime_requirements_for_lock(home, lock)
    bindings = {
        "directory": [item.__dict__ for item in registry.directories if item.app == app_id],
        "default": registry.default.__dict__ if registry.default is not None and registry.default.app == app_id else None,
    }
    return {
        "application": {
            "id": app_id,
            "name": lock.application_name if lock is not None else "",
        },
        "tenant_dir": str(tenant_dir),
        "files": {
            "tenant": str(tenant_dir / "tenant.toml"),
            "app_manifest": str(tenant_dir / "app.toml"),
            "app_lock": str(tenant_dir / "app.lock.json"),
            "values": str(tenant_dir / "values.toml"),
            "tenant_config": str(tenant_dir / "config" / "tenant.toml"),
        },
        "present": {
            "tenant": (tenant_dir / "tenant.toml").is_file(),
            "app_manifest": (tenant_dir / "app.toml").is_file(),
            "app_lock": (tenant_dir / "app.lock.json").is_file(),
            "values": (tenant_dir / "values.toml").is_file(),
            "tenant_config": (tenant_dir / "config" / "tenant.toml").is_file(),
            "plugins": (tenant_dir / "plugins").is_dir(),
            "skills": (tenant_dir / "skills").is_dir(),
        },
        "distro": {
            "name": lock.distro_name if lock is not None else "",
            "source": lock.distro_source if lock is not None else "",
            "layout": "tenant runtime surface with lock/source metadata",
        },
        "kernel": lock.kernel if lock is not None else {},
        "runtimes": list(lock.runtimes) if lock is not None else [],
        "bindings": bindings,
        "runtime_surface": runtime_cfg,
        "runtime_requirements": runtime_requirements,
        "materializer": materializer,
        "issues": _inspect_issues(tenant_dir, runtime_cfg, materializer, runtime_requirements=runtime_requirements),
    }


def _read_runtime_config(path: Path, app_id: str) -> dict:
    if not path.is_file():
        return {"config_path": str(path), "configured": False, "tenants": []}
    try:
        import tomllib
        data = tomllib.loads(path.read_text(encoding="utf-8"))
    except Exception:
        return {"config_path": str(path), "configured": False, "tenants": [], "error": "runtime config is unreadable"}
    tenants = []
    for item in data.get("tenant") or []:
        if isinstance(item, dict) and str(item.get("id") or "") == app_id:
            tenants.append({
                "id": app_id,
                "plugin_dirs": list(item.get("plugin_dirs") or []),
                "skill_dirs": list(item.get("skill_dirs") or []),
            })
    return {"config_path": str(path), "configured": bool(tenants), "tenants": tenants}


def _materializer_status(tenant_dir: Path, *, expected: bool) -> dict:
    config_dir = tenant_dir / "config"
    plugin_cfg = config_dir / "plugins"
    client_cfg = config_dir / "clients"
    return {
        "expected": expected,
        "ran": config_dir.is_dir(),
        "tenant_config": str(config_dir / "tenant.toml"),
        "plugin_configs": sorted(path.parent.name for path in plugin_cfg.glob("*/config.toml")) if plugin_cfg.is_dir() else [],
        "client_configs": sorted(path.parent.name for path in client_cfg.glob("*/config.toml")) if client_cfg.is_dir() else [],
    }


def _materializer_expected(home: Path, lock: appmod.AppLock | None) -> bool:
    if lock is None or not lock.manifest_path.strip():
        return False
    try:
        manifest = appmod.load(Path(lock.manifest_path), tabula_home=home)
    except appmod.AppManifestError:
        return False
    return appmod.materializer_declared(manifest, home)


def _inspect_issues(tenant_dir: Path, runtime_cfg: dict, materializer: dict, *, runtime_requirements: dict | None = None) -> list[str]:
    issues: list[str] = []
    required = {
        "tenant.toml": tenant_dir / "tenant.toml",
        "app.toml": tenant_dir / "app.toml",
        "app.lock.json": tenant_dir / "app.lock.json",
        "values.toml": tenant_dir / "values.toml",
    }
    for name, path in required.items():
        if not path.is_file():
            issues.append(f"missing {name}")
    if not runtime_cfg.get("configured"):
        issues.append("runtime config does not include this app tenant")
    if materializer.get("expected") and not materializer.get("ran"):
        issues.append("materializer output config is missing")
    for item in (runtime_requirements or {}).get("executables", []):
        if item.get("required") and not item.get("found"):
            issues.append(f"missing required runtime executable: {item.get('name')}")
    return issues


def _runtime_requirements_for_manifest(manifest: appmod.AppManifest, home: Path) -> dict:
    try:
        distro = cfg.load(appmod.resolve_distro_path(manifest, home))
    except (cfg.ConfigError, appmod.AppManifestError):
        return {"executables": []}
    return {"executables": [status.to_json() for status in reqmod.check_executables(distro)]}


def _runtime_requirements_for_lock(home: Path, lock: appmod.AppLock | None) -> dict:
    if lock is None or not lock.manifest_path.strip():
        return {"executables": []}
    try:
        manifest = appmod.load(Path(lock.manifest_path), tabula_home=home)
    except appmod.AppManifestError:
        return {"executables": []}
    return _runtime_requirements_for_manifest(manifest, home)


def _print_app_inspect(payload: dict) -> None:
    app = payload["application"]
    print(f"app:      {app.get('id')}" + (f" ({app.get('name')})" if app.get("name") else ""))
    print(f"tenant:   {payload['tenant_dir']}")
    distro = payload["distro"]
    print(f"distro:   {distro.get('name')}  {distro.get('source')}")
    kernel = payload.get("kernel") or {}
    print(f"kernel:   {kernel.get('id')}  {kernel.get('mode')}  {kernel.get('url')}")
    runtime_surface = payload.get("runtime_surface") or {}
    print(f"runtime:  configured={runtime_surface.get('configured')}  {runtime_surface.get('config_path')}")
    _print_runtime_requirements(payload.get("runtime_requirements") or {})
    materializer = payload.get("materializer") or {}
    print(f"materializer: ran={materializer.get('ran')} plugin_configs={len(materializer.get('plugin_configs') or [])}")
    issues = payload.get("issues") or []
    if issues:
        print("issues:")
        for issue in issues:
            print(f"  - {issue}")
    else:
        print("issues: none")


def _print_app_audit(manifest: appmod.AppManifest, lock_path: Path, lock: appmod.AppLock, home: Path) -> None:
    audit = _app_audit_json(manifest, lock_path, lock, home)
    print(f"app:      {manifest.application.id}" + (f" ({manifest.application.name})" if manifest.application.name else ""))
    print(f"manifest: {manifest.path}")
    print(f"lock:     {lock_path}")
    print(f"tenant:   {audit['tenant_dir']}  applied={audit['applied']}")
    print(f"distro:   {lock.distro_name}  {lock.distro_source}")
    print(f"kernel:   {lock.kernel.get('id')}  {lock.kernel.get('mode')}  {lock.kernel.get('url')}")
    print("runtimes:")
    for runtime in lock.runtimes:
        exec_cfg = runtime.get("exec") if isinstance(runtime.get("exec"), dict) else {}
        print(f"  - {runtime.get('id')}  mode={runtime.get('mode')}  backend={exec_cfg.get('backend')}  tenants={runtime.get('tenants')}")
    _print_runtime_requirements(audit.get("runtime_requirements") or {})
    if lock.bindings:
        print("bindings:")
        default = lock.bindings.get("default")
        if isinstance(default, dict):
            print(f"  default: app={default.get('app')} kernel={default.get('kernel')}")
        for item in lock.bindings.get("directory", []) if isinstance(lock.bindings.get("directory"), list) else []:
            print(f"  directory: root={item.get('root')} app={item.get('app')} kernel={item.get('kernel')}")
    issues = audit.get("issues") or []
    if issues:
        print("issues:")
        for issue in issues:
            print(f"  - {issue}")
    else:
        print("issues: none")


def _print_runtime_requirements(payload: dict) -> None:
    executables = payload.get("executables") or []
    if not executables:
        return
    print("requirements:")
    for item in executables:
        state = "found" if item.get("found") else "missing"
        required = "required" if item.get("required") else "optional"
        detail = f" ({', '.join(item.get('required_for') or [])})" if item.get("required_for") else ""
        print(f"  - {item.get('name')}: {state} {required}{detail}")


def _print_run_plan(plan: runmod.RunPlan, tenant_dir: Path) -> None:
    print(f"run plan: app={plan.app_id} tenant={tenant_dir}")
    print(f"  kernel: id={plan.kernel_id} mode={plan.kernel_mode} url={plan.kernel_url}")
    print(f"  serve runtime mode: {plan.runtime_mode}")
    for runtime_id, backend in zip(plan.runtime_ids, plan.execution_backends):
        print(f"  runtime: id={runtime_id} backend={backend}")


def _active_name(home: Path) -> str:
    active = home / "distrib" / "active"
    if not active.is_symlink():
        return ""
    target = os.readlink(active)
    return Path(target).name


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
    if lock.clients:
        print("  clients:")
        for name, entry in sorted(lock.clients.items()):
            print(f"    {name:20s} {_describe_lock(entry)}")
    if not lock.bundles and not lock.skills and not lock.plugins and not lock.clients:
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

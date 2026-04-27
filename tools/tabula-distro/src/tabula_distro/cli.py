"""CLI entrypoint for ``tabula-distro``."""
from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path

from . import __version__
from . import config as cfg
from . import generations as gens
from . import install as installmod
from . import lock as lockmod


def _default_home() -> Path:
    return Path(os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula")))


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


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="tabula-distro", description="Tabula distribution installer")
    parser.add_argument("--version", action="version", version=f"%(prog)s {__version__}")
    parser.add_argument("--home", default=None, help="override $TABULA_HOME")
    sub = parser.add_subparsers(dest="cmd", required=True)

    p_install = sub.add_parser("install", help="install or re-apply a distro")
    p_install.add_argument("source", type=_source_arg, help="distro source directory")
    p_install.add_argument("--name", default="", help="override installed distro name")
    p_install.add_argument("--frozen", action="store_true",
                           help="require lockfile; no network access")
    p_install.add_argument("--update", action="store_true",
                           help="ignore pinned lock; re-resolve all git refs")
    p_install.add_argument("--update-only", action="append", default=[],
                           help="update only the named bundle/skill/plugin/component (repeatable)")
    p_install.add_argument("--keep-generations", type=int, default=5)
    p_install.set_defaults(func=_cmd_install)

    p_update = sub.add_parser("update", help="update pinned sources (alias for install --update)")
    p_update.add_argument("--name", required=False, default="",
                          help="distro name (defaults to the active one)")
    p_update.add_argument("name_positional", nargs="?", help=argparse.SUPPRESS)
    p_update.add_argument("--only", action="append", default=[],
                          help="only update this bundle/skill/plugin/component (repeatable)")
    p_update.set_defaults(func=_cmd_update)

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

    args = parser.parse_args(argv)
    home = Path(args.home).expanduser().resolve() if args.home else _default_home()
    home.mkdir(parents=True, exist_ok=True)
    try:
        return args.func(args, home)
    except (installmod.InstallError, cfg.ConfigError, lockmod.LockError) as exc:
        print(f"tabula-distro: {exc}", file=sys.stderr)
        return 1


def _cmd_install(args: argparse.Namespace, home: Path) -> int:
    result = installmod.install(
        args.source, home,
        override_name=args.name or None,
        offline=args.frozen,
        update=args.update,
        update_only=tuple(args.update_only),
        keep_generations=args.keep_generations,
    )
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
    result = installmod.install(
        source, home,
        override_name=distro_name,
        update=True,
        update_only=tuple(args.only),
    )
    _print_summary(home, result.lock.distro, result.generation, result.lock, changed=result.changed)
    return 0


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
            for lck in list(lock.bundles.values()) + list(lock.skills.values()) + list(lock.plugins.values()):
                if lck.resolved_sha:
                    keep.add(lck.resolved_sha)
    removed = cache.gc(keep)
    print(f"removed {len(removed)} cached worktree(s)")
    return 0


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
    if not lock.bundles and not lock.skills and not lock.plugins:
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

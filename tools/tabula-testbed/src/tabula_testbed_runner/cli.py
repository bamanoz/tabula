from __future__ import annotations

import argparse
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path

from . import runner


def cache_tabula_ref(ref: str) -> Path:
    safe = re.sub(r"[^A-Za-z0-9_.-]+", "_", ref)
    cache = Path(os.environ.get("TABULA_TESTBED_CACHE", "~/.cache/tabula-testbed")).expanduser()
    root = cache / "tabula" / safe
    if (root / "cmd" / "tabula").is_dir():
        return root.resolve()
    root.parent.mkdir(parents=True, exist_ok=True)
    tmp = root.with_suffix(".tmp")
    if tmp.exists():
        shutil.rmtree(tmp)
    result = subprocess.run([
        "git", "clone", "--depth", "1", "--branch", ref,
        "https://github.com/bamanoz/tabula.git", str(tmp),
    ], check=False, stdout=sys.stderr, stderr=sys.stderr)
    if result.returncode != 0:
        shutil.rmtree(tmp, ignore_errors=True)
        subprocess.run(["git", "clone", "--filter=blob:none", "https://github.com/bamanoz/tabula.git", str(tmp)], check=True, stdout=sys.stderr, stderr=sys.stderr)
        subprocess.run(["git", "fetch", "--depth", "1", "origin", ref], cwd=str(tmp), check=True, stdout=sys.stderr, stderr=sys.stderr)
        subprocess.run(["git", "checkout", "--detach", "FETCH_HEAD"], cwd=str(tmp), check=True, stdout=sys.stderr, stderr=sys.stderr)
    tmp.replace(root)
    return root.resolve()


def discover_tabula_root(raw: str | None, version: str | None = None) -> Path:
    if version and not raw:
        return cache_tabula_ref(version)
    candidates: list[Path] = []
    if raw:
        candidates.append(Path(raw))
    if os.environ.get("TABULA_ROOT"):
        candidates.append(Path(os.environ["TABULA_ROOT"]))
    cwd = Path.cwd()
    candidates.extend([cwd, cwd / "tabula", cwd.parent / "tabula"])
    for candidate in candidates:
        root = candidate.expanduser().resolve()
        if (root / "cmd" / "tabula").is_dir() and (root / "tools" / "tabula-distro").is_dir():
            return root
    raise SystemExit("could not find tabula root; pass --tabula-root /path/to/tabula, --tabula-version REF, or set TABULA_ROOT")


def normalize_source(value: str) -> str:
    alias, sep, source = value.partition("=")
    if not sep or not alias or not source:
        raise argparse.ArgumentTypeError("sources must be ALIAS=SOURCE")
    if not source.startswith(("local:", "git+", "source:")):
        source = "local:" + str(Path(source).expanduser().resolve())
    return f"{alias}={source}"


def add_common(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--tabula-root", default="", help="Path to tabula source checkout. Defaults to TABULA_ROOT or nearby ./tabula.")
    parser.add_argument("--tabula-version", default="", help="Git ref to fetch/cache when --tabula-root is not provided, e.g. v0.9.0.")
    parser.add_argument("--testbed-dir", default="", help="Path to testbed distro template.")
    parser.add_argument("--source", action="append", default=[], type=normalize_source, help="Add source alias: ALIAS=SOURCE. Relative paths imply local:/abs/path.")
    parser.add_argument("--suite", action="append", default=[], help="Suite to run.")
    parser.add_argument("--set", default="", help="Bundle set to install.")
    parser.add_argument("--all", action="store_true", help="Use all bundle set / all discovered suites.")
    parser.add_argument("--bundle", action="append", default=[], help="Add bundle to generated distro.")
    parser.add_argument("--without", action="append", default=[], help="Remove bundle from generated distro.")
    parser.add_argument("--component", action="append", default=[], help="Add BUNDLE:COMPONENT to generated distro.")
    parser.add_argument("--json", action="store_true", help="Emit machine-readable JSON summary to stdout; logs go to stderr.")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="tabula-testbed", description="Run Tabula testbed suites in an isolated runtime.")
    sub = parser.add_subparsers(dest="command", required=True)

    for name, help_text in (
        ("list", "List discovered suites."),
        ("lint", "Validate selected suite manifests/components."),
        ("direct", "Run lint plus Python syntax checks."),
        ("run", "Run selected suites in an isolated Tabula runtime."),
    ):
        cmd = sub.add_parser(name, help=help_text)
        add_common(cmd)
        if name == "run":
            cmd.add_argument("--keep", action="store_true", help="Keep temporary TABULA_HOME after run.")
            cmd.add_argument("--home", default="", help="Use this TABULA_HOME and keep it.")
    return parser


def to_runner_args(args: argparse.Namespace) -> list[str]:
    tabula_root = discover_tabula_root(args.tabula_root, getattr(args, "tabula_version", "") or None)
    out = ["--repo-root", str(tabula_root)]
    for attr in ("testbed_dir", "set"):
        value = getattr(args, attr, "")
        if value:
            out.extend(["--" + attr.replace("_", "-"), value])
    if getattr(args, "all", False):
        out.append("--all")
    if getattr(args, "json", False):
        out.append("--json")
    for attr in ("source", "suite", "bundle", "without", "component"):
        for value in getattr(args, attr, []) or []:
            out.extend(["--" + attr.replace("_", "-"), value])
    if args.command == "list":
        out.append("--list-suites")
    elif args.command == "lint":
        out.append("--lint")
    elif args.command == "direct":
        out.append("--direct")
    elif args.command == "run":
        if getattr(args, "keep", False):
            out.append("--keep")
        if getattr(args, "home", ""):
            out.extend(["--home", args.home])
    return out


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv or sys.argv[1:])
    return runner.main(to_runner_args(args))


if __name__ == "__main__":
    raise SystemExit(main())

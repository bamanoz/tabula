#!/usr/bin/env python3
from __future__ import annotations

import argparse
import ctypes
import json
import os
import re
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import tomllib
import venv as venv_module
from dataclasses import dataclass
from pathlib import Path
from typing import Any


JSON_MODE = False


@dataclass(frozen=True)
class SuiteSpec:
    name: str
    set_name: str
    bundles: tuple[str, ...]
    components: tuple[str, ...]
    tests: tuple[Path, ...]
    manifest: Path
    runtime_tenants: tuple[str, ...] = ()


def free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def prune_old_testbed_homes(parent: Path, *, current: Path | None = None) -> tuple[int, int]:
    removed = 0
    errors = 0
    current_resolved = current.resolve() if current is not None else None
    for path in parent.glob("tabula-testbed.*"):
        try:
            if current_resolved is not None and path.resolve() == current_resolved:
                continue
            if not path.is_dir():
                continue
            shutil.rmtree(path)
            removed += 1
        except OSError:
            errors += 1
    return removed, errors


def log(message: str) -> None:
    print(message, file=sys.stderr if JSON_MODE else sys.stdout)


def run(cmd: list[str], *, env: dict[str, str] | None = None, cwd: Path | None = None) -> None:
    if JSON_MODE:
        subprocess.run(cmd, cwd=str(cwd) if cwd else None, env=env, check=True, stdout=sys.stderr, stderr=sys.stderr)
    else:
        subprocess.run(cmd, cwd=str(cwd) if cwd else None, env=env, check=True)


def run_with_retries(cmd: list[str], *, attempts: int = 3, env: dict[str, str] | None = None, cwd: Path | None = None) -> None:
    last: subprocess.CalledProcessError | None = None
    for attempt in range(attempts):
        try:
            run(cmd, env=env, cwd=cwd)
            return
        except subprocess.CalledProcessError as exc:
            last = exc
            if attempt == attempts - 1:
                break
            time.sleep(0.5 * (attempt + 1))
    if last is not None:
        raise last


def output(cmd: list[str], *, cwd: Path | None = None) -> str:
    return subprocess.check_output(cmd, cwd=str(cwd) if cwd else None, text=True).strip()


def virtualenv_layout(root: Path) -> tuple[Path, Path]:
    context = venv_module.EnvBuilder(with_pip=False).ensure_directories(str(root))
    return Path(context.env_exe), Path(context.bin_path)


def write_diagnostic(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run Tabula testbed in an isolated TABULA_HOME")
    parser.add_argument("--repo-root", required=True, help="Path to tabula repository root")
    parser.add_argument("--testbed-dir", default="", help="Path to testbed distro template")
    parser.add_argument("--keep", action="store_true", help="Keep temporary TABULA_HOME after run")
    parser.add_argument("--home", default="", help="Use this TABULA_HOME and keep it")
    parser.add_argument("--set", default="", help="Testbed bundle set to install")
    parser.add_argument("--all", action="store_true", help="Use the all bundle set and all discovered suites")
    parser.add_argument("--bundle", action="append", default=[], help="Add a bundle to generated distro")
    parser.add_argument("--without", action="append", default=[], help="Remove a bundle from generated distro")
    parser.add_argument("--component", action="append", default=[], help="Add BUNDLE:COMPONENT to generated distro")
    parser.add_argument("--source", action="append", default=[], help="Add source alias: ALIAS=SOURCE")
    parser.add_argument("--suite", action="append", default=[], help="Suite to run")
    parser.add_argument("--list-suites", action="store_true", help="List discovered suites and exit")
    parser.add_argument("--lint", action="store_true", help="Validate selected suite manifests/components without starting kernel")
    parser.add_argument("--direct", action="store_true", help="Run lint plus Python syntax checks without starting kernel")
    parser.add_argument("--json", action="store_true", help="Emit machine-readable JSON summary to stdout; logs go to stderr")
    parser.add_argument("--bootstrap-check", action="store_true", help="Run scripts/bootstrap.sh after distro install and before suite execution")
    return parser.parse_args(argv)


def load_toml(path: Path) -> dict[str, Any]:
    return tomllib.loads(path.read_text(encoding="utf-8"))


def entry_protocol_markers(entry_path: Path, python_lib_dir: Path | None = None) -> list[str]:
    sources: list[tuple[str, Path]] = [("entry", entry_path)]
    if python_lib_dir is not None:
        sdk_dir = python_lib_dir / "tabula_plugin_sdk"
        for sdk_path in (sdk_dir / "api.py", sdk_dir / "protocol.py"):
            if sdk_path.is_file():
                sources.append((f"sdk:{sdk_path.name}", sdk_path))
    markers: list[str] = []
    checks = (
        ("legacy-register-request", "register_request"),
        ("legacy-register-request-error", "expected register_request as first plugin message"),
        ("m2-worker-init-ack", "init_ack"),
        ("m2-worker-tools-updated", "tools_updated"),
    )
    for label, path in sources:
        try:
            source = path.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            markers.append(f"{label}:not-utf8")
            continue
        for marker, needle in checks:
            if needle in source:
                markers.append(f"{label}:{marker}")
    return markers or ["none"]


def format_protocol_markers(markers: list[str]) -> str:
    return ", ".join(markers)


def _plugin_owned_path(root: Path, value: str) -> Path | None:
    raw = value.strip()
    if not raw:
        return None
    path = Path(raw)
    if path.is_absolute():
        return path
    if "/" not in raw and "\\" not in raw:
        return None
    return (root / path).resolve()


def manifest_launch_details(manifest_path: Path, manifest: dict[str, Any], python_lib_dir: Path | None = None) -> dict[str, Any]:
    root = manifest_path.parent
    worker = manifest.get("worker") if isinstance(manifest.get("worker"), dict) else None
    if worker is not None and "command" in worker:
        command = worker.get("command")
        if not isinstance(command, list) or not command:
            raise SystemExit(f"plugin manifest worker.command must be a non-empty argv list: {manifest_path}")
        argv = [str(item).strip() for item in command]
        if any(not item for item in argv):
            raise SystemExit(f"plugin manifest worker.command contains empty argv item: {manifest_path}")
        marker_path: Path | None = None
        for candidate in argv[1:] + argv[:1]:
            resolved = _plugin_owned_path(root, candidate)
            if resolved is not None and resolved.is_file():
                marker_path = resolved
                break
        return {
            "summary": [
                f"worker.command = {argv!r}",
                f"worker.mode = {worker.get('mode') or manifest.get('worker_mode') or 'warm'}",
                f"entry_path = {marker_path or argv[0]}",
                f"entry_protocol_markers = {format_protocol_markers(entry_protocol_markers(marker_path, python_lib_dir) if marker_path is not None else ['none'])}",
            ],
        }
    runtime_name = str(manifest.get("runtime") or "")
    entry_name = str(manifest.get("entry") or "")
    if not runtime_name:
        raise SystemExit(f"plugin manifest missing runtime field: {manifest_path}")
    if not entry_name:
        raise SystemExit(f"plugin manifest missing entry field: {manifest_path}")
    entry_path = (root / entry_name).resolve()
    if not entry_path.is_file():
        raise SystemExit(f"plugin entry file is missing: {entry_path}")
    markers = entry_protocol_markers(entry_path, python_lib_dir)
    return {
        "summary": [
            f"runtime = {runtime_name}",
            f"entry = {entry_name}",
            f"entry_path = {entry_path}",
            f"entry_protocol_markers = {format_protocol_markers(markers)}",
        ],
    }


def parse_source(value: str) -> tuple[str, str]:
    alias, sep, source = value.partition("=")
    if not sep or not alias or not source:
        raise SystemExit(f"invalid --source {value!r}; expected ALIAS=SOURCE")
    return alias, source


def local_source_root(source: str) -> Path | None:
    if not source.startswith("local:"):
        return None
    raw = source[len("local:"):].split("#", 1)[0]
    return Path(raw).expanduser().resolve()


def describe_testbed_sources(source_roots: dict[str, str]) -> str:
    if not source_roots:
        return "no external sources configured\n"
    sections: list[str] = []
    for alias, source in sorted(source_roots.items()):
        lines = [f"## {alias}", f"source = {source}"]
        root = local_source_root(source)
        if root is None:
            lines.append("local_root = <non-local>")
        else:
            lines.append(f"local_root = {root}")
            lines.append(f"exists = {str(root.is_dir()).lower()}")
            if root.is_dir():
                for label, cmd in (
                    ("git_head", ["git", "rev-parse", "HEAD"]),
                    ("git_branch", ["git", "rev-parse", "--abbrev-ref", "HEAD"]),
                    ("git_remote", ["git", "remote", "get-url", "origin"]),
                    ("git_status_short", ["git", "status", "--short"]),
                ):
                    try:
                        value = output(cmd, cwd=root)
                    except Exception as exc:
                        value = f"<unavailable: {exc}>"
                    lines.append(f"{label} = {value or '<clean>'}")
        sections.append("\n".join(lines))
    return "\n\n".join(sections).rstrip() + "\n"


def discover_suite_specs(testbed_dir: Path, source_roots: dict[str, str]) -> tuple[dict[str, SuiteSpec], list[Path]]:
    specs: dict[str, SuiteSpec] = {}
    manifests: list[Path] = []

    def add_specs(manifest_path: Path, tests_dir: Path) -> None:
        manifests.append(manifest_path)
        manifest = load_toml(manifest_path)
        for name, data in manifest.get("suites", {}).items():
            tests = tuple((tests_dir / test).resolve() for test in data.get("tests", []))
            specs[name] = SuiteSpec(
                name=name,
                set_name=str(data.get("set") or "baseline"),
                bundles=tuple(str(v) for v in data.get("bundles", [])),
                components=tuple(str(v) for v in data.get("components", [])),
                tests=tests,
                manifest=manifest_path,
                runtime_tenants=tuple(str(v) for v in data.get("runtime_tenants", [])),
            )

    add_specs(testbed_dir / "testbed.toml", testbed_dir)
    for source in source_roots.values():
        root = local_source_root(source)
        if root and root.is_dir():
            for manifest_path in sorted(root.glob("*/tests/testbed.toml")):
                add_specs(manifest_path, manifest_path.parent)
    return specs, manifests


def resolve_selection(args: argparse.Namespace, suite_specs: dict[str, SuiteSpec]) -> tuple[str, bool, list[str], list[str], list[str], list[Path], list[str]]:
    requested_suites = list(args.suite)
    components = list(args.component)
    bundles = list(args.bundle)
    without = list(args.without)
    all_set = bool(args.all)

    if all_set:
        requested_suites = requested_suites or sorted(suite_specs)
        set_name = args.set or "all"
    elif requested_suites:
        unknown = [name for name in requested_suites if name not in suite_specs]
        if unknown:
            raise SystemExit(f"unknown testbed suites: {', '.join(unknown)}; available: {', '.join(sorted(suite_specs))}")
        set_names = {suite_specs[name].set_name for name in requested_suites}
        set_name = args.set or (set_names.pop() if len(set_names) == 1 else "baseline")
        for name in requested_suites:
            bundles.extend(suite_specs[name].bundles)
            components.extend(suite_specs[name].components)
    else:
        set_name = args.set or "baseline"
        requested_suites = ["baseline"]
        if set_name == "caveman" or "caveman" in bundles:
            requested_suites.append("caveman")
            spec = suite_specs.get("caveman", SuiteSpec("caveman", "empty", (), (), (), Path()))
            bundles.extend(spec.bundles)
            for component in spec.components:
                components.append(component)

    tests: list[Path] = []
    for name in requested_suites:
        spec = suite_specs.get(name)
        if spec is None:
            continue
        tests.extend(spec.tests)
    if not tests:
        raise SystemExit(f"no tests found for suites: {', '.join(requested_suites)}")
    return set_name, all_set, bundles, without, components, tests, requested_suites


def runtime_tenants_for_suites(suite_specs: dict[str, SuiteSpec], suites: list[str]) -> tuple[str, ...]:
    selected = {suite_specs[name].runtime_tenants for name in suites if name in suite_specs and suite_specs[name].runtime_tenants}
    if len(selected) > 1:
        formatted = ", ".join("[" + ",".join(items) + "]" for items in sorted(selected))
        raise SystemExit(f"selected suites require conflicting runtime_tenants: {formatted}")
    return next(iter(selected)) if selected else ()


def set_runtime_tenants(config_path: Path, tenants: tuple[str, ...]) -> None:
    if not tenants:
        return
    if not config_path.exists():
        home = config_path.parent.parent
        plugin_manifests = sorted(str(path) for path in (home / "plugins").glob("*/plugin.toml"))
        value = "[" + ", ".join(json.dumps(item) for item in tenants) + "]"
        text = (
            f'plugin_dirs = [{", ".join(json.dumps(item) for item in plugin_manifests)}]\n'
            f'skill_dirs = [{json.dumps(str(home / "skills"))}]\n\n'
            '[[kernel]]\n'
            '  id = "main"\n'
            f'  url = "unix://{home / "run" / "runtime.sock"}"\n'
            f'  token_file = {json.dumps(str(home / "run" / "runtime-token"))}\n'
            f'  tenants = {value}\n'
            '  ca_file = ""\n'
            '  cert_file = ""\n'
            '  key_file = ""\n'
            '  tls_insecure_skip_verify = false\n'
        )
        config_path.parent.mkdir(parents=True, exist_ok=True)
        config_path.write_text(text, encoding="utf-8")
        return
    text = config_path.read_text(encoding="utf-8")
    value = "[" + ", ".join(json.dumps(item) for item in tenants) + "]"
    replacement = f"tenants = {value}"
    if re.search(r"(?m)^\s*tenants\s*=\s*\[[^\n]*\]", text):
        text = re.sub(r"(?m)^\s*tenants\s*=\s*\[[^\n]*\]", replacement, text, count=1)
    else:
        text = re.sub(r"(?m)^(\s*token_file\s*=\s*[^\n]+)$", r"\1\n  " + replacement, text, count=1)
    config_path.write_text(text, encoding="utf-8")


def merge_manifests(paths: list[Path]) -> dict[str, Any]:
    merged: dict[str, Any] = {"sources": {}, "sets": {}, "bundles": {}, "suites": {}}
    for path in paths:
        data = load_toml(path)
        for key in merged:
            merged[key].update(data.get(key, {}))
    return merged


def ordered_unique(values: list[str]) -> list[str]:
    out: list[str] = []
    seen: set[str] = set()
    for value in values:
        if value not in seen:
            seen.add(value)
            out.append(value)
    return out


def parse_component(value: str) -> tuple[str, str]:
    bundle, sep, component = value.partition(":")
    if not sep or not bundle or not component:
        raise SystemExit(f"invalid component {value!r}; expected BUNDLE:COMPONENT")
    return bundle, component


def split_source_path(source: str) -> tuple[str, str]:
    body, sep, fragment = source.partition("#")
    subpath = ""
    if sep:
        for part in fragment.split("&"):
            if part.startswith("path="):
                subpath = part[len("path="):].strip("/")
    return body, subpath


def resolve_bundle_root(bundle_name: str, manifest: dict[str, Any], source_roots: dict[str, str]) -> Path | None:
    entry = manifest.get("bundles", {}).get(bundle_name)
    if not entry:
        return None
    source = str(entry.get("source") or "")
    if source.startswith("source:"):
        alias_body, subpath = split_source_path(source)
        alias = alias_body[len("source:"):]
        root_source = source_roots.get(alias) or manifest.get("sources", {}).get(alias, {}).get("source", "")
        root = local_source_root(root_source)
        if root is None:
            return None
        return (root / subpath).resolve() if subpath else root
    body, subpath = split_source_path(source)
    root = local_source_root(body)
    if root is None:
        return None
    return (root / subpath).resolve() if subpath else root


def selected_bundles(manifest: dict[str, Any], set_name: str, all_set: bool, bundles: list[str], without: list[str], components: list[str]) -> tuple[list[str], dict[str, list[str]]]:
    effective_set = "all" if all_set else set_name
    sets = manifest.get("sets", {})
    if effective_set not in sets:
        raise SystemExit(f"unknown testbed set {effective_set!r}; available: {', '.join(sorted(sets))}")
    selected = list(sets[effective_set])
    unfiltered_set_bundles = set(selected) if effective_set in {"baseline", "all"} else set()
    selected.extend(bundles)
    component_map: dict[str, list[str]] = {}
    for raw in components:
        bundle, component = parse_component(raw)
        selected.append(bundle)
        component_map.setdefault(bundle, []).append(component)
    selected = [name for name in ordered_unique(selected) if name not in set(without)]
    known = manifest.get("bundles", {})
    unknown = [name for name in selected if name not in known]
    if unknown:
        raise SystemExit(f"unknown testbed bundles: {', '.join(unknown)}; available: {', '.join(sorted(known))}")
    return selected, {name: ordered_unique(values) for name, values in component_map.items() if name in selected and name not in unfiltered_set_bundles}


def resolve_bundle_dependency_closure(
    selected: list[str],
    component_map: dict[str, list[str]],
    manifest: dict[str, Any],
    source_roots: dict[str, str],
    excluded: set[str],
) -> dict[str, Path]:
    roots: dict[str, Path] = {}
    visiting: list[str] = []
    visited: set[str] = set()
    explicitly_restricted = set(component_map)

    def visit(bundle: str) -> None:
        if bundle in visited:
            return
        if bundle in visiting:
            cycle = visiting[visiting.index(bundle):] + [bundle]
            raise SystemExit(f"bundle dependency cycle: {' -> '.join(cycle)}")
        if bundle in excluded:
            parent = visiting[-1] if visiting else "selection"
            raise SystemExit(f"bundle {parent!r} depends on explicitly excluded bundle {bundle!r}")

        root = resolve_bundle_root(bundle, manifest, source_roots)
        if root is None and visiting:
            root = roots[visiting[-1]].parent / bundle
        if root is None:
            raise SystemExit(f"bundle {bundle!r} has no locally resolvable source for dependency validation")
        if not root.is_dir():
            raise SystemExit(f"bundle {bundle!r} root does not exist: {root}")
        if not (root / "bundle.toml").is_file():
            raise SystemExit(f"bundle {bundle!r} missing bundle.toml: {root}")

        visiting.append(bundle)
        roots[bundle] = root
        data = load_toml(root / "bundle.toml")
        dependencies = data.get("dependencies") or []
        if not isinstance(dependencies, list):
            raise SystemExit(f"bundle {bundle!r} dependencies must be an array of tables")
        for entry in dependencies:
            if not isinstance(entry, dict):
                raise SystemExit(f"bundle {bundle!r} dependencies entries must be tables")
            dependency = str(entry.get("bundle") or "").strip()
            if not dependency:
                raise SystemExit(f"bundle {bundle!r} dependency entry missing bundle")
            required = entry.get("components") or []
            if not isinstance(required, list) or not all(isinstance(value, str) and value.strip() for value in required):
                raise SystemExit(f"bundle {bundle!r} dependency {dependency!r} components must be list[str]")
            visit(dependency)
            if dependency not in selected:
                selected.append(dependency)
                component_map[dependency] = ordered_unique(required)
            elif required and dependency in component_map:
                missing = [value for value in required if value not in component_map[dependency]]
                if missing and dependency in explicitly_restricted:
                    allowed = ", ".join(component_map[dependency]) or "none"
                    raise SystemExit(
                        f"bundle {dependency!r} selection allows components {allowed}, "
                        f"but bundle {bundle!r} requires {', '.join(missing)}"
                    )
                component_map[dependency] = ordered_unique(component_map[dependency] + required)
        visiting.pop()
        visited.add(bundle)

    roots_in_order = list(selected)
    for bundle in roots_in_order:
        visit(bundle)

    ordered_roots = {bundle: roots[bundle] for bundle in selected}
    return ordered_roots


def lint_selection(manifest: dict[str, Any], source_roots: dict[str, str], set_name: str, all_set: bool,
                   bundles: list[str], without: list[str], components: list[str], tests: list[Path]) -> tuple[list[str], dict[str, list[str]], dict[str, Path]]:
    selected, component_map = selected_bundles(manifest, set_name, all_set, bundles, without, components)
    roots = resolve_bundle_dependency_closure(selected, component_map, manifest, source_roots, set(without))
    for bundle, root in roots.items():
        for component in component_map.get(bundle, []):
            if not (root / component).is_dir():
                raise SystemExit(f"bundle {bundle!r} missing component {component!r}: {root / component}")
    validate_bundle_dependencies(selected, roots)
    missing_tests = [str(path) for path in tests if not path.is_file()]
    if missing_tests:
        raise SystemExit("missing test files:\n" + "\n".join(missing_tests))
    return selected, component_map, roots


def validate_bundle_dependencies(selected: list[str], roots: dict[str, Path]) -> None:
    selected_set = set(selected)
    for bundle, root in sorted(roots.items()):
        data = load_toml(root / "bundle.toml")
        dependencies = data.get("dependencies") or []
        if not isinstance(dependencies, list):
            raise SystemExit(f"bundle {bundle!r} dependencies must be an array of tables")
        for entry in dependencies:
            if not isinstance(entry, dict):
                raise SystemExit(f"bundle {bundle!r} dependencies entries must be tables")
            dep = str(entry.get("bundle") or "").strip()
            if not dep:
                raise SystemExit(f"bundle {bundle!r} dependency entry missing bundle")
            if dep not in selected_set:
                raise SystemExit(f"bundle {bundle!r} depends on unselected bundle {dep!r}")


def direct_checks(roots: dict[str, Path], component_map: dict[str, list[str]], tests: list[Path]) -> None:
    py_files: list[str] = []
    for bundle, root in roots.items():
        components = component_map.get(bundle)
        search_roots = [root / c for c in components] if components else [root]
        for search_root in search_roots:
            if search_root.is_dir():
                py_files.extend(str(path) for path in search_root.rglob("*.py") if "__pycache__" not in path.parts)
    py_files.extend(str(path) for path in tests)
    if py_files:
        run([sys.executable, "-m", "py_compile", *sorted(set(py_files))])


def verify_runtime_sidecar_layout(home: Path, bin_dir: Path, logs_dir: Path) -> None:
    runtime_bin = bin_dir / ("tabula-runtime.exe" if os.name == "nt" else "tabula-runtime")
    if not runtime_bin.is_file():
        raise SystemExit(f"missing runtime sidecar binary: {runtime_bin}")
    run([str(runtime_bin), "--version"])

    runtime_toml = home / "config" / "runtime.toml"
    if not runtime_toml.is_file():
        raise SystemExit(f"missing runtime config: {runtime_toml}")
    cfg = load_toml(runtime_toml)
    kernels = cfg.get("kernel", [])
    if len(kernels) != 1:
        raise SystemExit(f"runtime config expected exactly one [[kernel]] entry, got {len(kernels)}")
    kernel = kernels[0]
    token_file = str(kernel.get("token_file") or "")
    url = str(kernel.get("url") or "")
    plugin_dirs = cfg.get("plugin_dirs", [])
    if not token_file.endswith("runtime-token"):
        raise SystemExit(f"runtime config token_file is unexpected: {token_file}")
    if not url.startswith("unix://"):
        raise SystemExit(f"runtime config url must use unix://, got {url}")
    if not plugin_dirs:
        raise SystemExit("runtime config plugin_dirs is empty")
    manifest_summary = logs_dir / "runtime-plugin-manifests.txt"
    python_lib_dir = home / "packages" / "python" / "src"
    sections: list[str] = []
    for entry in plugin_dirs:
        manifest_path = Path(str(entry))
        if manifest_path.is_dir():
            manifests = sorted(manifest_path.glob("*/plugin.toml"))
            if not manifests:
                continue
            for child in manifests:
                manifest = load_toml(child)
                details = manifest_launch_details(child, manifest, python_lib_dir)
                sections.append(
                    f"## {child}\n"
                    + "\n".join(details["summary"])
                    + "\n\n"
                    f"{child.read_text(encoding='utf-8').rstrip()}\n"
                )
            continue
        if not manifest_path.is_file():
            raise SystemExit(f"runtime config plugin_dirs entry is missing: {manifest_path}")
        manifest = load_toml(manifest_path)
        details = manifest_launch_details(manifest_path, manifest, python_lib_dir)
        sections.append(
            f"## {manifest_path}\n"
            + "\n".join(details["summary"])
            + "\n\n"
            f"{manifest_path.read_text(encoding='utf-8').rstrip()}\n"
        )
    write_diagnostic(manifest_summary, "\n".join(sections).rstrip() + "\n")


def verify_kernel_config(home: Path, expected_url: str) -> None:
    kernel_toml = home / "config" / "kernel.toml"
    if not kernel_toml.is_file():
        raise SystemExit(f"missing kernel config: {kernel_toml}")
    cfg = load_toml(kernel_toml)
    kernel = cfg.get("kernel") if isinstance(cfg.get("kernel"), dict) else {}
    if str(kernel.get("url") or "") != expected_url:
        raise SystemExit(f"kernel config url is unexpected: {kernel.get('url')!r} (want {expected_url!r})")
    forbidden = sorted(set(cfg) & {"meta", "workspace", "default_provider", "prompt_skills", "external_skills"})
    if forbidden:
        raise SystemExit(f"kernel config contains non-kernel fields: {', '.join(forbidden)}")


def attached_runtime_pid(body: dict[str, Any]) -> int:
    runtimes = body.get("runtimes", [])
    for runtime in runtimes:
        if runtime.get("id") != "local" or not runtime.get("attached"):
            continue
        pid = int(runtime.get("pid") or 0)
        if pid > 0:
            return pid
    return 0


def find_runtime_pid(home: Path) -> int:
    if os.name == "nt":
        return 0
    try:
        raw = subprocess.check_output(["pgrep", "-f", str(home / "config" / "runtime.toml")], text=True)
    except Exception:
        return 0
    for line in raw.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            return int(line)
        except ValueError:
            continue
    return 0


def wait_for_supervised_runtime(bin_dir: Path, home: Path, logs_dir: Path) -> int:
    tabula_bin = bin_dir / ("tabula.exe" if os.name == "nt" else "tabula")
    env = os.environ.copy()
    env["TABULA_HOME"] = str(home)
    deadline = time.time() + 20
    last: str | None = None
    status_path = logs_dir / "runtime-status-last.json"
    error_path = logs_dir / "runtime-status-last.error.txt"
    if status_path.exists():
        status_path.unlink()
    if error_path.exists():
        error_path.unlink()
    while time.time() < deadline:
        try:
            raw = subprocess.check_output([str(tabula_bin), "status", "--json"], env=env, text=True)
            write_diagnostic(status_path, raw)
            if error_path.exists():
                error_path.unlink()
            body = json.loads(raw)
            runtime_pid = attached_runtime_pid(body) or find_runtime_pid(home)
            if any(runtime.get("id") == "local" and runtime.get("attached") for runtime in body.get("runtimes", []) if isinstance(runtime, dict)):
                return runtime_pid
            last = raw.strip()
        except Exception as exc:
            last = str(exc)
            write_diagnostic(error_path, last + "\n")
        time.sleep(0.5)
    raise SystemExit(f"runtime did not attach in status output: {last}")


def process_alive(pid: int) -> bool:
    if pid <= 0:
        return False
    if os.name == "nt":
        process_query_limited_information = 0x1000
        still_active = 259
        handle = ctypes.windll.kernel32.OpenProcess(process_query_limited_information, False, pid)
        if not handle:
            return ctypes.get_last_error() == 5
        try:
            exit_code = ctypes.c_ulong()
            if not ctypes.windll.kernel32.GetExitCodeProcess(handle, ctypes.byref(exit_code)):
                return False
            return exit_code.value == still_active
        finally:
            ctypes.windll.kernel32.CloseHandle(handle)
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return False
    except PermissionError:
        return True
    return True


def wait_for_process_exit(pid: int, timeout: float) -> None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        if not process_alive(pid):
            return
        time.sleep(0.1)
    raise SystemExit(f"runtime child pid {pid} did not exit after kernel shutdown")


def verify_runtime_teardown(home: Path, runtime_pid: int) -> None:
    if runtime_pid <= 0:
        runtime_pid = find_runtime_pid(home)
    wait_for_process_exit(runtime_pid, 20)
    runtime_socket = home / "run" / "runtime.sock"
    if runtime_socket.exists():
        raise SystemExit(f"runtime socket still exists after kernel shutdown: {runtime_socket}")


def list_suites(specs: dict[str, SuiteSpec]) -> None:
    if JSON_MODE:
        print(json.dumps({"suites": [
            {
                "name": spec.name,
                "set": spec.set_name,
                "bundles": list(spec.bundles),
                "components": list(spec.components),
                "tests": [str(path) for path in spec.tests],
                "manifest": str(spec.manifest),
            }
            for spec in (specs[name] for name in sorted(specs))
        ]}, sort_keys=True))
        return
    for name in sorted(specs):
        spec = specs[name]
        print(name)
        print(f"  set: {spec.set_name}")
        if spec.bundles:
            print(f"  bundles: {', '.join(spec.bundles)}")
        if spec.components:
            print(f"  components: {', '.join(spec.components)}")
        print(f"  tests: {', '.join(str(path) for path in spec.tests)}")
        print(f"  manifest: {spec.manifest}")


def wait_for_kernel(python: Path, url: str, env: dict[str, str]) -> None:
    code = r'''
import json
import os
from pathlib import Path
import sys
import time
import websocket

url = sys.argv[1]
tabula_home = os.environ.get("TABULA_HOME", "").strip()
env_token = os.environ.get("TABULA_KERNEL_TOKEN", "").strip()

def kernel_token():
    if tabula_home:
        try:
            token = (Path(tabula_home) / "run" / "kernel-client-token").read_text(encoding="utf-8").strip()
            if token:
                return token
        except OSError:
            pass
    return env_token

deadline = time.time() + 20
last = None
while time.time() < deadline:
    try:
        ws = websocket.create_connection(url, timeout=1)
        ws.send(json.dumps({"v": 3, "type": "hello", "data": {"name": "testbed-isolated-ready", "send_topics": [], "receive_topics": [], "auth_token": kernel_token()}}))
        msg = json.loads(ws.recv())
        ws.close()
        if msg.get("type") == "hello_ack":
            sys.exit(0)
        last = RuntimeError(str(msg))
    except Exception as exc:
        last = exc
        time.sleep(0.5)
print(f"kernel did not become ready at {url}: {last}", file=sys.stderr)
sys.exit(1)
'''
    run([str(python), "-c", code, url], env=env)


def main(argv: list[str] | None = None) -> int:
    global JSON_MODE
    if hasattr(sys.stdout, "reconfigure"):
        sys.stdout.reconfigure(line_buffering=True)
    args = parse_args(argv or sys.argv[1:])
    JSON_MODE = bool(args.json)
    repo_root = Path(args.repo_root).resolve()
    packaged_testbed = Path(__file__).resolve().parent / "testbed_template"
    if args.testbed_dir:
        testbed_dir = Path(args.testbed_dir).resolve()
    elif packaged_testbed.is_dir():
        testbed_dir = packaged_testbed
    else:
        raise SystemExit("packaged testbed template is missing; reinstall tabula-testbed or pass --testbed-dir")
    source_roots: dict[str, str] = {}
    for raw in args.source:
        alias, source = parse_source(raw)
        source_roots[alias] = source
    suite_specs, manifest_paths = discover_suite_specs(testbed_dir, source_roots)
    if args.list_suites:
        list_suites(suite_specs)
        return 0
    set_name, all_set, bundles, without, components, tests, suites = resolve_selection(args, suite_specs)
    runtime_tenants = runtime_tenants_for_suites(suite_specs, suites)
    manifest = merge_manifests(manifest_paths)

    if args.lint or args.direct:
        selected, component_map, roots = lint_selection(manifest, source_roots, set_name, all_set, bundles, without, components, tests)
        log(f"==> Suites: {','.join(suites)}")
        log(f"==> Bundles: {','.join(selected)}")
        if component_map:
            log("==> Components: " + ",".join(f"{k}:{'|'.join(v)}" for k, v in sorted(component_map.items())))
        if args.direct:
            direct_checks(roots, component_map, tests)
            log("==> Direct checks passed")
            if JSON_MODE:
                print(json.dumps({"ok": True, "mode": "direct", "suites": suites, "bundles": selected, "components": component_map}, sort_keys=True))
        else:
            log("==> Lint passed")
            if JSON_MODE:
                print(json.dumps({"ok": True, "mode": "lint", "suites": suites, "bundles": selected, "components": component_map}, sort_keys=True))
        return 0

    requested, requested_components = selected_bundles(
        manifest, set_name, all_set, bundles, without, components
    )
    selected, component_map, _ = lint_selection(
        manifest, source_roots, set_name, all_set, bundles, without, components, tests
    )

    home = Path(args.home).resolve() if args.home else Path(tempfile.mkdtemp(prefix="tabula-testbed."))
    keep = args.keep or bool(args.home)
    if not keep:
        removed, errors = prune_old_testbed_homes(home.parent, current=home)
        if removed or errors:
            log(f"==> Pruned old testbed homes: removed={removed} errors={errors}")
    kernel_port = free_port()
    observer_port = free_port()
    kernel_url = f"ws://127.0.0.1:{kernel_port}/ws"
    observer_url = f"http://127.0.0.1:{observer_port}/metrics"
    venv = home / ".venv"
    bin_dir = home / "bin"
    logs_dir = home / "logs"
    kernel: subprocess.Popen | None = None
    runtime_pid = 0
    success = False

    log(f"==> Testbed home: {home}")
    log(f"==> Kernel URL: {kernel_url}")
    log(f"==> Observer URL: {observer_url}")
    log(f"==> Suites: {','.join(suites)}")

    try:
        bin_dir.mkdir(parents=True, exist_ok=True)
        logs_dir.mkdir(parents=True, exist_ok=True)
        (home / "config").mkdir(parents=True, exist_ok=True)
        write_diagnostic(logs_dir / "testbed-sources.txt", describe_testbed_sources(source_roots))

        log("==> Installing isolated Python environment")
        run([sys.executable, "-m", "venv", "--without-pip", str(venv)])
        python, venv_bin = virtualenv_layout(venv)
        run_with_retries([str(python), "-m", "ensurepip", "--upgrade"])
        run_with_retries([str(python), "-m", "pip", "install", "-q", "--upgrade", "pip"])
        run_with_retries([str(python), "-m", "pip", "install", "-q", "-r", str(repo_root / "scripts" / "requirements-dev.txt")])
        run_with_retries([str(python), "-m", "pip", "install", "-q", "-e", str(repo_root / "tools" / "tabula-distro")])

        log("==> Building isolated kernel/runtime binaries")
        version = (repo_root / "VERSION").read_text(encoding="utf-8").strip()
        try:
            commit = output(["git", "rev-parse", "--short", "HEAD"], cwd=repo_root)
        except Exception:
            commit = "unknown"
        date = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        ldflags = f"-X main.version={version} -X main.commit={commit} -X main.date={date}"
        go_exe = output(["go", "env", "GOEXE"], cwd=repo_root)
        tabula_bin = bin_dir / f"tabula{go_exe}"
        runtime_bin = bin_dir / f"tabula-runtime{go_exe}"
        prebuilt_dir_raw = os.environ.get("TABULA_TESTBED_BIN_DIR", "").strip()
        if prebuilt_dir_raw:
            prebuilt_dir = Path(prebuilt_dir_raw).expanduser().resolve()
            prebuilt_tabula = prebuilt_dir / tabula_bin.name
            prebuilt_runtime = prebuilt_dir / runtime_bin.name
            missing = [str(path) for path in (prebuilt_tabula, prebuilt_runtime) if not path.is_file()]
            if missing:
                raise SystemExit("TABULA_TESTBED_BIN_DIR is missing required binaries: " + ", ".join(missing))
            log(f"==> Using prebuilt core binaries from {prebuilt_dir}")
            shutil.copy2(prebuilt_tabula, tabula_bin)
            shutil.copy2(prebuilt_runtime, runtime_bin)
        else:
            run(["go", "build", "-ldflags", ldflags, "-o", str(tabula_bin), "./cmd/tabula/"], cwd=repo_root)
            run(["go", "build", "-ldflags", ldflags, "-o", str(runtime_bin), "./cmd/tabula-runtime/"], cwd=repo_root)
        (home / "VERSION").write_text(version + "\n", encoding="utf-8")

        # Snapshot the runtime plugin compatibility range so the distro
        # installer's compat check has the same source of truth as install-dev.sh.
        protocol_blob = output([str(tabula_bin), "--protocol"])
        (home / "PROTOCOL").write_text(protocol_blob + "\n", encoding="utf-8")

        generated = home / "generated-testbed"
        generate_args = [
            str(python), str(testbed_dir / "generate.py"),
            "--output", str(generated),
            "--set", set_name,
        ]
        for manifest_path in manifest_paths:
            generate_args.extend(["--manifest", str(manifest_path)])
        for alias, source in sorted(source_roots.items()):
            generate_args.extend(["--source", f"{alias}={source}"])
        if all_set:
            generate_args.append("--all")
        for value in requested:
            generate_args.extend(["--bundle", value])
        for value in without:
            generate_args.extend(["--without", value])
        for bundle, values in sorted(requested_components.items()):
            if not values:
                generate_args.extend(["--empty-components", bundle])
            for component in values:
                generate_args.extend(["--component", f"{bundle}:{component}"])

        log("==> Generating concrete testbed distro")
        run(generate_args)

        env = os.environ.copy()
        python_package_root = home / "packages" / "python" / "src"
        python_path = [str(python_package_root)]
        if env.get("PYTHONPATH"):
            python_path.append(env["PYTHONPATH"])
        env.update({
            "TABULA_HOME": str(home),
            "TABULA_URL": kernel_url,
            "TABULA_OBSERVER_PORT": str(observer_port),
            "TABULA_CRON_DISABLE_OS_CRONTAB": "1",
            "TABULA_CRON_POLL_INTERVAL": "1",
            "TABULA_PROVIDER": "anthropic",
            "TABULA_PATH": os.pathsep.join((str(venv_bin), str(bin_dir), env.get("PATH", ""))),
            "PYTHONPATH": os.pathsep.join(python_path),
        })
        env.setdefault("TABULA_GATEWAY_WEB_PORT", str(free_port()))
        log(f"==> Installing generated testbed distro from {generated}")
        run([str(python), "-m", "tabula_distro.cli", "--home", str(home), "install", str(generated)], env=env)
        if args.bootstrap_check:
            log("==> Running bootstrap readiness check")
            run([str(repo_root / "scripts" / "bootstrap.sh"), "--tabula-home", str(home), "--timeout", "20"], env=env, cwd=repo_root)
        set_runtime_tenants(home / "config" / "runtime.toml", runtime_tenants)
        verify_kernel_config(home, kernel_url)
        log("==> Starting isolated kernel")
        out = (logs_dir / "kernel.out.log").open("w", encoding="utf-8")
        err = (logs_dir / "kernel.err.log").open("w", encoding="utf-8")
        creationflags = subprocess.CREATE_NEW_PROCESS_GROUP if os.name == "nt" else 0
        kernel = subprocess.Popen(
            [str(tabula_bin), "serve", "--runtime-mode", "managed"],
            env=env,
            stdout=out,
            stderr=err,
            creationflags=creationflags,
        )

        log("==> Waiting for isolated kernel")
        wait_for_kernel(python, kernel_url, env)

        log("==> Verifying runtime sidecar layout")
        verify_runtime_sidecar_layout(home, bin_dir, logs_dir)

        log("==> Waiting for supervised runtime attachment")
        runtime_pid = wait_for_supervised_runtime(bin_dir, home, logs_dir)

        log("==> Running testbed smoke tests")
        runner_lib = Path(__file__).resolve().parents[1]
        smoke_env = env.copy()
        smoke_env["TABULA_TESTBED_LIVE"] = "1"
        smoke_env["TABULA_ROOT"] = str(repo_root)
        smoke_env["PYTHONPATH"] = os.pathsep.join(
            (str(runner_lib), str(home / "packages" / "python" / "src"), str(testbed_dir / "tests"))
        )
        for test in tests:
            run([
                str(python), str(test),
                "--url", kernel_url,
                "--observer-url", observer_url,
                "--home", str(home),
            ], env=smoke_env)
        log("==> Testbed smoke passed")
        success = True
        if JSON_MODE:
            print(json.dumps({
                "ok": True,
                "mode": "run",
                "suites": suites,
                "home": str(home),
                "kernel_url": kernel_url,
                "observer_url": observer_url,
                "kept": keep,
            }, sort_keys=True))
        return 0
    finally:
        teardown_error: str | None = None
        if kernel is not None and kernel.poll() is None:
            shutdown_signal = signal.CTRL_BREAK_EVENT if os.name == "nt" else signal.SIGTERM
            kernel.send_signal(shutdown_signal)
            try:
                kernel.wait(timeout=5)
            except subprocess.TimeoutExpired:
                kernel.kill()
                kernel.wait(timeout=5)
        if runtime_pid > 0:
            try:
                verify_runtime_teardown(home, runtime_pid)
            except SystemExit as exc:
                success = False
                teardown_error = str(exc)
        if not success:
            keep = True
            diagnostics = {
                "home": str(home),
                "generated_distro": str(home / "generated-testbed" / "distro.toml"),
                "testbed_sources": str(logs_dir / "testbed-sources.txt"),
                "runtime_config": str(home / "config" / "runtime.toml"),
                "protocol_file": str(home / "PROTOCOL"),
                "plugin_manifests": str(logs_dir / "runtime-plugin-manifests.txt"),
                "bootstrap_status": str(logs_dir / "bootstrap-status-last.json"),
                "bootstrap_status_error": str(logs_dir / "bootstrap-status-last.error.txt"),
                "runtime_status": str(logs_dir / "runtime-status-last.json"),
                "runtime_status_error": str(logs_dir / "runtime-status-last.error.txt"),
                "bootstrap_stdout": str(logs_dir / "bootstrap-kernel.out.log"),
                "bootstrap_stderr": str(logs_dir / "bootstrap-kernel.err.log"),
                "kernel_stdout": str(logs_dir / "kernel.out.log"),
                "kernel_stderr": str(logs_dir / "kernel.err.log"),
            }
            if teardown_error:
                diagnostics["teardown_error"] = teardown_error
            log("==> Testbed failed; keeping diagnostics")
            log(f"    home: {diagnostics['home']}")
            log(f"    generated distro: {diagnostics['generated_distro']}")
            log(f"    testbed sources: {diagnostics['testbed_sources']}")
            log(f"    runtime config: {diagnostics['runtime_config']}")
            log(f"    protocol file: {diagnostics['protocol_file']}")
            log(f"    plugin manifests: {diagnostics['plugin_manifests']}")
            log(f"    bootstrap status: {diagnostics['bootstrap_status']}")
            log(f"    bootstrap status error: {diagnostics['bootstrap_status_error']}")
            log(f"    runtime status: {diagnostics['runtime_status']}")
            log(f"    runtime status error: {diagnostics['runtime_status_error']}")
            log(f"    bootstrap stdout: {diagnostics['bootstrap_stdout']}")
            log(f"    bootstrap stderr: {diagnostics['bootstrap_stderr']}")
            log(f"    kernel stdout: {diagnostics['kernel_stdout']}")
            log(f"    kernel stderr: {diagnostics['kernel_stderr']}")
            if teardown_error:
                log(f"    teardown error: {teardown_error}")
            if JSON_MODE:
                print(json.dumps({"ok": False, "mode": "run", "suites": suites, "diagnostics": diagnostics}, sort_keys=True))
        if keep:
            log(f"==> Testbed home kept at {home}")
        else:
            shutil.rmtree(home, ignore_errors=True)
        if teardown_error:
            raise SystemExit(teardown_error)


if __name__ == "__main__":
    raise SystemExit(main())

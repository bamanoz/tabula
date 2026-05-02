#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import tomllib
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


def free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def log(message: str) -> None:
    print(message, file=sys.stderr if JSON_MODE else sys.stdout)


def run(cmd: list[str], *, env: dict[str, str] | None = None, cwd: Path | None = None) -> None:
    if JSON_MODE:
        subprocess.run(cmd, cwd=str(cwd) if cwd else None, env=env, check=True, stdout=sys.stderr, stderr=sys.stderr)
    else:
        subprocess.run(cmd, cwd=str(cwd) if cwd else None, env=env, check=True)


def output(cmd: list[str], *, cwd: Path | None = None) -> str:
    return subprocess.check_output(cmd, cwd=str(cwd) if cwd else None, text=True).strip()


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
    return parser.parse_args(argv)


def load_toml(path: Path) -> dict[str, Any]:
    return tomllib.loads(path.read_text(encoding="utf-8"))


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
    return selected, {name: ordered_unique(values) for name, values in component_map.items() if name in selected}


def lint_selection(manifest: dict[str, Any], source_roots: dict[str, str], set_name: str, all_set: bool,
                   bundles: list[str], without: list[str], components: list[str], tests: list[Path]) -> tuple[list[str], dict[str, list[str]], dict[str, Path]]:
    selected, component_map = selected_bundles(manifest, set_name, all_set, bundles, without, components)
    roots: dict[str, Path] = {}
    for bundle in selected:
        root = resolve_bundle_root(bundle, manifest, source_roots)
        if root is None:
            continue
        roots[bundle] = root
        if not root.is_dir():
            raise SystemExit(f"bundle {bundle!r} root does not exist: {root}")
        if not (root / "bundle.toml").is_file():
            raise SystemExit(f"bundle {bundle!r} missing bundle.toml: {root}")
        for component in component_map.get(bundle, []):
            if not (root / component).is_dir():
                raise SystemExit(f"bundle {bundle!r} missing component {component!r}: {root / component}")
    missing_tests = [str(path) for path in tests if not path.is_file()]
    if missing_tests:
        raise SystemExit("missing test files:\n" + "\n".join(missing_tests))
    return selected, component_map, roots


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


def wait_for_kernel(python: Path, url: str) -> None:
    code = r'''
import json
import sys
import time
import websocket

url = sys.argv[1]
deadline = time.time() + 20
last = None
while time.time() < deadline:
    try:
        ws = websocket.create_connection(url, timeout=1)
        ws.send(json.dumps({"type": "connect", "name": "testbed-isolated-ready", "sends": [], "receives": [], "version": 1}))
        msg = json.loads(ws.recv())
        ws.close()
        if msg.get("type") == "connected":
            sys.exit(0)
        last = RuntimeError(str(msg))
    except Exception as exc:
        last = exc
        time.sleep(0.5)
print(f"kernel did not become ready at {url}: {last}", file=sys.stderr)
sys.exit(1)
'''
    run([str(python), "-c", code, url])


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

    home = Path(args.home).resolve() if args.home else Path(tempfile.mkdtemp(prefix="tabula-testbed."))
    keep = args.keep or bool(args.home)
    kernel_port = free_port()
    observer_port = free_port()
    kernel_url = f"ws://127.0.0.1:{kernel_port}/ws"
    observer_url = f"http://127.0.0.1:{observer_port}/metrics"
    venv = home / ".venv"
    bin_dir = home / "bin"
    logs_dir = home / "logs"
    kernel: subprocess.Popen | None = None
    success = False

    log(f"==> Testbed home: {home}")
    log(f"==> Kernel URL: {kernel_url}")
    log(f"==> Observer URL: {observer_url}")
    log(f"==> Suites: {','.join(suites)}")

    try:
        bin_dir.mkdir(parents=True, exist_ok=True)
        logs_dir.mkdir(parents=True, exist_ok=True)
        (home / "config").mkdir(parents=True, exist_ok=True)

        log("==> Installing isolated Python environment")
        run([sys.executable, "-m", "venv", str(venv)])
        python = venv / "bin" / "python3"
        pip = venv / "bin" / "pip"
        run([str(pip), "install", "-q", "--upgrade", "pip"])
        run([str(pip), "install", "-q", "-r", str(repo_root / "scripts" / "requirements-dev.txt")])
        run([str(pip), "install", "-q", "-e", str(repo_root / "tools" / "tabula-distro")])

        log("==> Building isolated kernel binary")
        version = (repo_root / "VERSION").read_text(encoding="utf-8").strip()
        try:
            commit = output(["git", "rev-parse", "--short", "HEAD"], cwd=repo_root)
        except Exception:
            commit = "unknown"
        date = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        ldflags = f"-X main.version={version} -X main.commit={commit} -X main.date={date}"
        run(["go", "build", "-ldflags", ldflags, "-o", str(bin_dir / "tabula"), "./cmd/tabula/"], cwd=repo_root)
        (home / "VERSION").write_text(version + "\n", encoding="utf-8")

        # Snapshot the kernel's plugin protocol range so the distro installer's
        # compat check has the same source of truth as install-dev.sh.
        protocol_blob = output([str(bin_dir / "tabula"), "--protocol"])
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
        for value in bundles:
            generate_args.extend(["--bundle", value])
        for value in without:
            generate_args.extend(["--without", value])
        for value in components:
            generate_args.extend(["--component", value])

        log("==> Generating concrete testbed distro")
        run(generate_args)

        log(f"==> Installing generated testbed distro from {generated}")
        run([str(venv / "bin" / "tabula-distro"), "--home", str(home), "install", str(generated)])

        env = os.environ.copy()
        env.update({
            "TABULA_HOME": str(home),
            "TABULA_URL": kernel_url,
            "TABULA_OBSERVER_PORT": str(observer_port),
            "TABULA_CRON_DISABLE_OS_CRONTAB": "1",
            "TABULA_CRON_POLL_INTERVAL": "1",
            "TABULA_BOOT": f'"{python}" "{home / "boot.py"}"',
            "TABULA_PATH": f"{venv / 'bin'}:{bin_dir}:{env.get('PATH', '')}",
        })
        log("==> Starting isolated kernel")
        out = (logs_dir / "kernel.out.log").open("w", encoding="utf-8")
        err = (logs_dir / "kernel.err.log").open("w", encoding="utf-8")
        kernel = subprocess.Popen([str(bin_dir / "tabula"), "serve"], env=env, stdout=out, stderr=err)

        log("==> Waiting for isolated kernel")
        wait_for_kernel(python, kernel_url)

        log("==> Running testbed smoke tests")
        runner_lib = Path(__file__).resolve().parents[1]
        smoke_env = env.copy()
        smoke_env["PYTHONPATH"] = f"{runner_lib}:{home / '_lib' / 'python' / 'src'}:{testbed_dir / 'tests'}"
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
        if kernel is not None and kernel.poll() is None:
            kernel.send_signal(signal.SIGTERM)
            try:
                kernel.wait(timeout=5)
            except subprocess.TimeoutExpired:
                kernel.kill()
                kernel.wait(timeout=5)
        if not success:
            keep = True
            diagnostics = {
                "home": str(home),
                "generated_distro": str(home / "generated-testbed" / "distro.toml"),
                "kernel_stdout": str(logs_dir / "kernel.out.log"),
                "kernel_stderr": str(logs_dir / "kernel.err.log"),
            }
            log("==> Testbed failed; keeping diagnostics")
            log(f"    home: {diagnostics['home']}")
            log(f"    generated distro: {diagnostics['generated_distro']}")
            log(f"    kernel stdout: {diagnostics['kernel_stdout']}")
            log(f"    kernel stderr: {diagnostics['kernel_stderr']}")
            if JSON_MODE:
                print(json.dumps({"ok": False, "mode": "run", "suites": suites, "diagnostics": diagnostics}, sort_keys=True))
        if keep:
            log(f"==> Testbed home kept at {home}")
        else:
            shutil.rmtree(home, ignore_errors=True)


if __name__ == "__main__":
    raise SystemExit(main())

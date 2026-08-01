"""Generic installer and managed-stack entrypoint for ``tabula-agent``."""
from __future__ import annotations

import argparse
import json
import os
import secrets
import signal
import subprocess
import sys
import time
import tomllib
from pathlib import Path

from . import __version__
from . import agent_manifest
from . import service_runtime
from . import config as cfg
from . import install as installmod
from . import lock as lockmod
from . import paths
from . import requirements as reqmod
from . import sources as srcmod
from . import tenant_bindings
from . import tenant_materializer


class AgentError(RuntimeError):
    pass


def _source_arg(value: str) -> str:
    if value.startswith("git+") or value.startswith("local:"):
        return value
    path = Path(value).expanduser()
    if not path.exists():
        raise argparse.ArgumentTypeError(f"distro source not found: {value}")
    if not path.is_dir():
        raise argparse.ArgumentTypeError(f"distro source is not a directory: {value}")
    return str(path.resolve())


def _new_tenant_id(home: Path) -> str:
    for _ in range(100):
        tenant_id = f"agent-{secrets.token_hex(6)}"
        if not (home / "tenants" / tenant_id).exists():
            return tenant_id
    raise AgentError("could not generate an unused local tenant id")


def _bound_tenant(registry: tenant_bindings.Registry, root: Path) -> str | None:
    canonical_root = str(root.expanduser().resolve())
    for binding in registry.directories:
        if binding.root == canonical_root:
            return binding.tenant
    return None


def _install_lock(home: Path, tenant_id: str) -> dict[str, object] | None:
    path = home / "tenants" / tenant_id / "install.lock.json"
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None
    if not isinstance(payload, dict) or payload.get("version") != 2:
        return None
    return payload


def _installed_source(home: Path, tenant_id: str) -> str | None:
    payload = _install_lock(home, tenant_id)
    distro = payload.get("distro") if payload is not None else None
    if not isinstance(distro, dict):
        return None
    source = distro.get("distro_source")
    return source if isinstance(source, str) and source else None


def _cmd_install(args: argparse.Namespace, home: Path) -> int:
    project_root = Path(args.bind or Path.cwd()).expanduser().resolve()
    if not project_root.is_dir():
        raise AgentError(f"binding root is not a directory: {project_root}")

    registry = tenant_bindings.load(home)
    existing_tenant = None
    if args.tenant:
        if tenant_bindings.tenant_exists(home, args.tenant):
            existing_tenant = args.tenant
    elif args.default and registry.default is not None:
        existing_tenant = registry.default.tenant
    else:
        existing_tenant = _bound_tenant(registry, project_root)
    if existing_tenant is not None:
        installed_source = _installed_source(home, existing_tenant)
        if installed_source == args.distro:
            registry = _bind_install_target(
                registry,
                project_root,
                existing_tenant,
                default=bool(args.default),
                replace=bool(args.replace_binding),
            )
            tenant_bindings.save(home, registry)
            if args.update:
                result = tenant_materializer.refresh(
                    args.distro,
                    home,
                    existing_tenant,
                    project_root,
                    Path(args.values) if args.values else None,
                    offline=bool(args.frozen),
                    update=True,
                )
                print(f"updated existing tenant {existing_tenant}")
                print(f"  distro: {result.distro.lock.distro}")
            elif args.values:
                tenant_materializer.rematerialize(
                    home, existing_tenant, project_root, Path(args.values)
                )
                print(f"using existing tenant {existing_tenant}")
            else:
                print(f"using existing tenant {existing_tenant}")
            print(f"  binding: {'default' if args.default else project_root}")
            print(f"  source:  {installed_source}")
            if not args.no_start:
                _start_installed_tenant(home, existing_tenant, args.timeout)
            return 0
        detail = (
            f"installed from {installed_source!r}"
            if installed_source is not None
            else "has no valid tenant install lock"
        )
        if args.tenant:
            raise AgentError(
                f"tenant {existing_tenant!r} already exists and {detail}; choose another --tenant id"
            )
        if not args.replace_binding:
            target = "default binding" if args.default else f"directory {project_root}"
            raise AgentError(
                f"{target} selects tenant {existing_tenant!r}, which {detail}; "
                "pass --replace-binding to install and select a new tenant"
            )

    tenant_id = args.tenant or _new_tenant_id(home)
    result = tenant_materializer.install(
        args.distro,
        home,
        tenant_id=tenant_id,
        project_root=project_root,
        values_path=Path(args.values) if args.values else None,
        offline=bool(args.frozen),
        update=bool(args.update),
        replace_binding=bool(args.replace_binding),
        bind_project=not bool(args.default),
    )
    if args.default:
        registry = tenant_bindings.bind_default(
            tenant_bindings.load(home), tenant_id, replace=bool(args.replace_binding)
        )
        tenant_bindings.save(home, registry)
    print(f"installed tenant {tenant_id}")
    print(f"  distro: {result.distro.lock.distro}")
    print(f"  binding: {'default' if args.default else project_root}")
    print(f"  lock:    {result.tenant_dir / 'install.lock.json'}")
    if not args.no_start:
        _start_installed_tenant(home, tenant_id, args.timeout)
    return 0


def _bind_install_target(
    registry: tenant_bindings.Registry,
    project_root: Path,
    tenant_id: str,
    *,
    default: bool,
    replace: bool,
) -> tenant_bindings.Registry:
    if default:
        return tenant_bindings.bind_default(registry, tenant_id, replace=replace)
    return tenant_bindings.bind_directory(registry, project_root, tenant_id, replace=replace)


def _start_installed_tenant(home: Path, tenant_id: str, timeout_seconds: float) -> None:
    _ensure_ready(home, tenant_id, _kernel_url(home), timeout_seconds)
    print(f"tenant {tenant_id} ready")


def _agent_pid_file(home: Path) -> Path:
    return home / "run" / "agent-kernel.pid"


def _write_agent_pid(home: Path, pid: int) -> None:
    path = _agent_pid_file(home)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(f"{pid}\n", encoding="utf-8")


def _read_agent_pid(home: Path) -> int | None:
    try:
        raw = _agent_pid_file(home).read_text(encoding="utf-8").strip()
    except OSError:
        return None
    try:
        pid = int(raw)
    except ValueError:
        _agent_pid_file(home).unlink(missing_ok=True)
        return None
    return pid if pid > 0 else None


def _process_exists(pid: int) -> bool:
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return False
    except PermissionError:
        return True
    return True


def _process_command(pid: int) -> str:
    try:
        result = subprocess.run(
            ["ps", "-p", str(pid), "-o", "command="],
            text=True,
            capture_output=True,
            check=False,
            timeout=2.0,
        )
    except (OSError, subprocess.TimeoutExpired):
        return ""
    if result.returncode != 0:
        return ""
    return result.stdout.strip()


def _agent_pid_matches(home: Path, pid: int) -> bool:
    command = _process_command(pid)
    return _agent_command_matches(home, command)


def _agent_command_matches(home: Path, command: str) -> bool:
    if not command:
        return False
    tabula_bin = home / "bin" / "tabula"
    parts = command.split()
    executable = parts[0] if parts else ""
    executable_candidates = {executable}
    if executable:
        executable_candidates.add(str(Path(executable).expanduser().resolve()))
    candidates = {str(tabula_bin), str(tabula_bin.resolve())}
    return bool(candidates & executable_candidates) and " serve" in f" {command}" and "--runtime-mode" in command and "managed" in command


def _scan_agent_pid(home: Path) -> int | None:
    try:
        result = subprocess.run(
            ["ps", "ax", "-o", "pid=", "-o", "command="],
            text=True,
            capture_output=True,
            check=False,
            timeout=2.0,
        )
    except (OSError, subprocess.TimeoutExpired):
        return None
    if result.returncode != 0:
        return None
    matches: list[int] = []
    for line in result.stdout.splitlines():
        raw = line.strip()
        if not raw:
            continue
        pid_text, _, command = raw.partition(" ")
        try:
            pid = int(pid_text)
        except ValueError:
            continue
        if pid > 0 and _agent_command_matches(home, command):
            matches.append(pid)
    if not matches:
        return None
    return max(matches)


def _status_kernel_pid(home: Path) -> int | None:
    tabula = home / "bin" / "tabula"
    if not tabula.is_file():
        return None
    env = os.environ.copy()
    env["TABULA_HOME"] = str(home)
    try:
        result = subprocess.run(
            [str(tabula), "status", "--json"],
            env=env,
            timeout=3.0,
            text=True,
            capture_output=True,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        return None
    if result.returncode != 0:
        return None
    try:
        data = json.loads(result.stdout)
    except json.JSONDecodeError:
        return None
    kernel = data.get("kernel") if isinstance(data, dict) else None
    if not isinstance(kernel, dict) or kernel.get("running") is not True:
        return None
    pid = kernel.get("pid")
    return pid if isinstance(pid, int) and pid > 0 else None


def _agent_pid(home: Path) -> int | None:
    pid = _read_agent_pid(home)
    if pid is not None:
        if _process_exists(pid) and _agent_pid_matches(home, pid):
            return pid
        _agent_pid_file(home).unlink(missing_ok=True)
    pid = _status_kernel_pid(home)
    if pid is not None and _process_exists(pid) and _agent_pid_matches(home, pid):
        _write_agent_pid(home, pid)
        return pid
    pid = _scan_agent_pid(home)
    if pid is not None and _process_exists(pid):
        _write_agent_pid(home, pid)
        return pid
    return None


def _stop_agent(home: Path, timeout_seconds: float) -> bool:
    pid = _agent_pid(home)
    if pid is None:
        return False
    os.kill(pid, signal.SIGTERM)
    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        if not _process_exists(pid):
            _agent_pid_file(home).unlink(missing_ok=True)
            return True
        time.sleep(0.2)
    raise AgentError(f"managed service did not stop within {timeout_seconds:g}s (pid {pid})")


def _cmd_init(args: argparse.Namespace, _home: Path) -> int:
    root = Path(args.root or Path.cwd()).expanduser().resolve()
    if not root.is_dir():
        raise AgentError(f"project root is not a directory: {root}")
    path = agent_manifest.create(root / agent_manifest.FILENAME, args.distro)
    print(f"created {path}")
    return 0


def _manifest_source(manifest: agent_manifest.AgentManifest) -> str:
    source = manifest.source
    if source.startswith("git+") or source.startswith("local:"):
        return source
    path = Path(source).expanduser()
    if not path.is_absolute():
        path = manifest.path.parent / path
    return str(path.resolve())


def _cmd_apply(args: argparse.Namespace, home: Path) -> int:
    root = Path(args.root or Path.cwd()).expanduser().resolve()
    manifest = agent_manifest.load(root / agent_manifest.FILENAME)
    values_path = home / "run" / f"agent-values-{os.getpid()}.toml"
    try:
        agent_manifest.write_values(values_path, manifest.values)
        install_args = argparse.Namespace(
            bind=str(root),
            default=False,
            tenant=None,
            distro=_manifest_source(manifest),
            values=str(values_path),
            frozen=bool(args.frozen),
            update=bool(args.update),
                replace_binding=bool(args.replace_binding),
            no_start=bool(args.no_start),
            non_interactive=True,
            timeout=args.timeout,
        )
        return _cmd_install(install_args, home)
    finally:
        values_path.unlink(missing_ok=True)


def _select_tenant(home: Path, explicit: str | None, cwd: Path) -> str:
    if explicit:
        tenant_bindings.require_tenant_exists(home, explicit)
        return explicit
    binding = tenant_bindings.resolve(tenant_bindings.load(home), cwd)
    if binding is None:
        raise AgentError(
            f"no tenant binding matches {cwd.resolve()}; run "
            f"tabula-agent install --distro <source> --bind {cwd.resolve()} or pass --tenant <id>"
        )
    tenant_bindings.require_tenant_exists(home, binding.tenant)
    return binding.tenant


def _kernel_url(home: Path) -> str:
    path = home / "config" / "kernel.toml"
    try:
        with path.open("rb") as handle:
            data = tomllib.load(handle)
    except (OSError, tomllib.TOMLDecodeError) as exc:
        raise AgentError(f"read kernel config {path}: {exc}") from exc
    kernel = data.get("kernel")
    url = kernel.get("url") if isinstance(kernel, dict) else None
    if not isinstance(url, str) or not url.strip():
        raise AgentError(f"kernel.url is missing from {path}")
    return url.strip()


def _ensure_ready(home: Path, tenant_id: str, kernel_url: str, timeout_seconds: float) -> None:
    if timeout_seconds <= 0:
        raise AgentError("service readiness timeout must be greater than zero")
    if service_runtime.kernel_healthy(kernel_url, timeout_seconds=0.5):
        ready, reload_error = _wait_for_ready_runtime(home, tenant_id, kernel_url, timeout_seconds)
        if ready:
            return
        detail = f"; runtime reload failed: {reload_error}" if reload_error else ""
        raise AgentError(f"kernel is reachable, but runtime is not ready for tenant {tenant_id!r}{detail}")

    tabula_bin = home / "bin" / "tabula"
    argv = service_runtime.launch_argv(str(tabula_bin))
    service_runtime.require_launch_binary(argv[0])
    env = os.environ.copy()
    env["TABULA_HOME"] = str(home)
    env["TABULA_PRESERVE_RUNTIME_CONFIG"] = "1"
    logs = home / "logs"
    logs.mkdir(parents=True, exist_ok=True)
    out = (logs / "agent-kernel.out.log").open("ab")
    err = (logs / "agent-kernel.err.log").open("ab")
    try:
        proc = subprocess.Popen(argv, env=env, stdout=out, stderr=err, start_new_session=True)
        _write_agent_pid(home, proc.pid)
    finally:
        out.close()
        err.close()

    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        if proc.poll() is not None:
            _agent_pid_file(home).unlink(missing_ok=True)
            raise AgentError(
                f"managed service exited during startup with code {proc.returncode}; "
                f"see {logs / 'agent-kernel.err.log'}"
            )
        remaining = max(0.1, deadline - time.monotonic())
        if service_runtime.kernel_healthy(kernel_url, timeout_seconds=min(0.5, remaining)):
            ready, _reload_error = _wait_for_ready_runtime(home, tenant_id, kernel_url, min(1.0, remaining))
            if ready:
                return
        time.sleep(0.2)
    raise AgentError(
        f"managed service did not become ready for tenant {tenant_id!r}; "
        f"see {logs / 'agent-kernel.err.log'}"
    )


def _wait_for_ready_runtime(home: Path, tenant_id: str, kernel_url: str, timeout_seconds: float) -> tuple[bool, str]:
    deadline = time.monotonic() + timeout_seconds
    reload_error = ""
    next_reload = 0.0
    while time.monotonic() < deadline:
        remaining = max(0.1, deadline - time.monotonic())
        if not service_runtime.kernel_healthy(kernel_url, timeout_seconds=min(0.5, remaining)):
            time.sleep(0.2)
            continue
        if service_runtime.wait_for_runtime_ready(
            kernel_url,
            tenant_id,
            home=home,
            timeout_seconds=min(1.0, remaining),
        ):
            return True, reload_error
        now = time.monotonic()
        if now >= next_reload:
            current = service_runtime.request_runtime_reload(
                kernel_url,
                home=home,
                timeout_seconds=min(1.0, remaining),
            )
            if current:
                reload_error = current
            next_reload = now + 1.0
        time.sleep(0.2)
    return False, reload_error


def _start(args: argparse.Namespace, home: Path) -> int:
    if args.timeout <= 0:
        raise AgentError("--timeout must be greater than zero")
    tenant_id = _select_tenant(home, args.tenant, Path.cwd())
    _ensure_ready(home, tenant_id, _kernel_url(home), args.timeout)
    print(f"tenant {tenant_id} ready")
    return 0


def _cmd_start(args: argparse.Namespace, home: Path) -> int:
    return _start(args, home)


def _cmd_stop(args: argparse.Namespace, home: Path) -> int:
    if args.timeout <= 0:
        raise AgentError("--timeout must be greater than zero")
    stopped = _stop_agent(home, args.timeout)
    if stopped:
        print("managed service stopped")
    else:
        print("managed service is not running")
    return 0


def _cmd_restart(args: argparse.Namespace, home: Path) -> int:
    if args.timeout <= 0:
        raise AgentError("--timeout must be greater than zero")
    stopped = _stop_agent(home, args.timeout)
    if stopped:
        print("managed service stopped")
    tenant_id = _select_tenant(home, args.tenant, Path.cwd())
    _ensure_ready(home, tenant_id, _kernel_url(home), args.timeout)
    print(f"tenant {tenant_id} ready")
    return 0


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="tabula-agent", description="Install and start project-scoped Tabula agents")
    parser.add_argument("--version", action="version", version=f"%(prog)s {__version__}")
    parser.add_argument("--home", default=None, help="override $TABULA_HOME")
    parser.add_argument("--tenant", default=None, help="start an explicit installed tenant")
    parser.add_argument("--timeout", type=float, default=30.0, help="service readiness timeout in seconds")
    sub = parser.add_subparsers(dest="command")

    install = sub.add_parser("install", help="install an agent tenant for a directory")
    install.add_argument("--distro", required=True, type=_source_arg, help="full Git URI or local distro path")
    install.add_argument("--bind", default=None, help="project directory to bind (default: current directory)")
    install.add_argument("--default", action="store_true", help="select tenant as fallback instead of binding a directory")
    install.add_argument("--tenant", default=None, help="explicit new tenant id")
    install.add_argument("--values", default="", help="distro-owned values TOML")
    install.add_argument("--frozen", action="store_true", help="require cached sources; no network access")
    install.add_argument("--update", action="store_true", help="resolve latest revisions")
    install.add_argument("--replace-binding", action="store_true", help="replace a binding owned by another installation")
    install.add_argument("--no-start", action="store_true", help="install without starting or checking the user service")
    install.add_argument("--non-interactive", action="store_true", help="disable prompts; fail on missing required input")
    install.add_argument("--timeout", type=float, default=30.0, help="service readiness timeout in seconds")
    install.set_defaults(func=_cmd_install)

    init = sub.add_parser("init", help="create optional tabula.agent.toml")
    init.add_argument("--distro", required=True, help="full Git URI or local distro path")
    init.add_argument("--root", default=None, help="project directory (default: current directory)")
    init.set_defaults(func=_cmd_init)

    apply = sub.add_parser("apply", help="apply tabula.agent.toml to selected project tenant")
    apply.add_argument("--root", default=None, help="project directory (default: current directory)")
    apply.add_argument("--frozen", action="store_true", help="require cached sources; no network access")
    apply.add_argument("--update", action="store_true", help="resolve latest revisions")
    apply.add_argument("--replace-binding", action="store_true")
    apply.add_argument("--no-start", action="store_true")
    apply.add_argument("--timeout", type=float, default=30.0)
    apply.set_defaults(func=_cmd_apply)

    start = sub.add_parser("start", help="start the managed local agent service")
    start.add_argument("--tenant", default=None, help="explicit installed tenant")
    start.add_argument("--timeout", type=float, default=30.0, help="service readiness timeout in seconds")
    start.set_defaults(func=_cmd_start)

    stop = sub.add_parser("stop", help="stop the managed local agent service")
    stop.add_argument("--timeout", type=float, default=10.0, help="graceful stop timeout in seconds")
    stop.set_defaults(func=_cmd_stop)

    restart = sub.add_parser("restart", help="restart the managed local agent service")
    restart.add_argument("--tenant", default=None, help="explicit installed tenant")
    restart.add_argument("--timeout", type=float, default=30.0, help="stop and readiness timeout in seconds")
    restart.set_defaults(func=_cmd_restart)
    return parser


def main(argv: list[str] | None = None) -> int:
    parser = _parser()
    parsed_argv = list(sys.argv[1:] if argv is None else argv)
    args = parser.parse_args(parsed_argv)
    home = Path(args.home).expanduser().resolve() if args.home else paths.tabula_home()
    home.mkdir(parents=True, exist_ok=True)
    try:
        if args.command in {"install", "init", "apply", "start", "stop", "restart"}:
            return args.func(args, home)
        return _start(args, home)
    except (
        AgentError,
        agent_manifest.AgentManifestError,
        service_runtime.ServiceRuntimeError,
        cfg.ConfigError,
        installmod.InstallError,
        lockmod.LockError,
        reqmod.RequirementsError,
        srcmod.SourceError,
        tenant_bindings.BindingError,
        tenant_materializer.TenantMaterializationError,
    ) as exc:
        print(f"tabula-agent: {exc}", file=sys.stderr)
        return 1

"""Launch orchestration for runnable app manifests."""
from __future__ import annotations

import os
import hashlib
import json
import shlex
import subprocess
import sys
import time
import tomllib
from dataclasses import dataclass
from pathlib import Path
from tempfile import gettempdir
from urllib.error import URLError
from urllib.parse import urlparse, urlunparse
from urllib.request import urlopen

import tomlkit

from tabula_distro import toml_io

from .app_manifest import AppManifest, RuntimeTopology


class AppRunError(RuntimeError):
    pass


@dataclass(frozen=True)
class RunPlan:
    app_id: str
    kernel_id: str
    kernel_mode: str
    kernel_url: str
    runtime_mode: str
    runtime_ids: tuple[str, ...]
    execution_backends: tuple[str, ...]


@dataclass(frozen=True)
class RunResult:
    plan: RunPlan
    started_kernel: bool = False
    reused_kernel: bool = False
    runtime_ready: bool = False


def plan(manifest: AppManifest) -> RunPlan:
    runtimes = manifest.runtimes
    runtime_mode = _serve_runtime_mode(runtimes)
    return RunPlan(
        app_id=manifest.application.id,
        kernel_id=manifest.kernel.id,
        kernel_mode=manifest.kernel.mode,
        kernel_url=manifest.kernel.url,
        runtime_mode=runtime_mode,
        runtime_ids=tuple(runtime.id for runtime in runtimes),
        execution_backends=tuple(str(runtime.exec.get("backend", "")) for runtime in runtimes),
    )


def execute(manifest: AppManifest, home: Path, *, tabula_bin: str = "tabula", timeout_seconds: float = 30.0, foreground: bool = False, boot_path: Path | None = None) -> RunResult:
    run_plan = plan(manifest)
    _check_runtime_execution_support(manifest.runtimes)
    if manifest.kernel.mode == "external":
        if not kernel_healthy(manifest.kernel.url, timeout_seconds=timeout_seconds):
            raise AppRunError(f"external kernel is not reachable: {manifest.kernel.url}")
        return RunResult(plan=run_plan, reused_kernel=True, runtime_ready=True)

    if manifest.kernel.mode != "managed":
        raise AppRunError(f"unsupported kernel mode: {manifest.kernel.mode}")

    if kernel_healthy(manifest.kernel.url, timeout_seconds=0.5):
        if foreground:
            raise AppRunError(f"kernel at {manifest.kernel.url} is already reachable; stop it before using --foreground")
        if wait_for_runtime_ready(manifest.kernel.url, manifest.application.id, timeout_seconds=timeout_seconds):
            return RunResult(plan=run_plan, reused_kernel=True, runtime_ready=True)
        return RunResult(plan=run_plan, reused_kernel=True, runtime_ready=False)

    boot_cmd = os.environ.get("TABULA_BOOT")
    if not boot_cmd:
        if boot_path is None:
            raise AppRunError("TABULA_BOOT is not set and no app boot path was provided")
        if not boot_path.is_file():
            raise AppRunError(f"app boot script does not exist at {boot_path}")
        boot_cmd = f"{shlex.quote(sys.executable)} {shlex.quote(str(boot_path))}"

    env = os.environ.copy()
    env["TABULA_HOME"] = str(home)
    env["TABULA_APP_ID"] = manifest.application.id
    env["TABULA_TENANT_ID"] = manifest.application.id
    env["TABULA_TENANT_DIR"] = str(home / "tenants" / manifest.application.id)
    if boot_path is not None:
        env["TABULA_BOOT_PATH"] = str(boot_path)
    env["TABULA_BOOT"] = boot_cmd
    env["TABULA_URL"] = manifest.kernel.url
    env.setdefault("TABULA_PATH", os.environ.get("PATH", ""))
    env["TABULA_PRESERVE_RUNTIME_CONFIG"] = "1"
    argv = _kernel_launch_argv(tabula_bin, run_plan.runtime_mode)
    _require_launch_binary(argv[0], tabula_bin=tabula_bin, runtime_mode=run_plan.runtime_mode)
    if foreground:
        os.execvpe(argv[0], argv, env)
        return RunResult(plan=run_plan, started_kernel=True)
    logs_dir = home / "logs"
    logs_dir.mkdir(parents=True, exist_ok=True)
    out = (logs_dir / "app-run-kernel.out.log").open("ab")
    err = (logs_dir / "app-run-kernel.err.log").open("ab")
    proc = subprocess.Popen(
        argv,
        env=env,
        stdout=out,
        stderr=err,
        start_new_session=True,
    )
    out.close()
    err.close()
    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        if proc.poll() is not None:
            raise AppRunError(f"managed kernel exited during startup with code {proc.returncode}; see {logs_dir / 'app-run-kernel.err.log'}")
        if kernel_healthy(manifest.kernel.url, timeout_seconds=0.5):
            return RunResult(plan=run_plan, started_kernel=True)
        time.sleep(0.2)
    raise AppRunError(f"managed kernel did not become ready: {manifest.kernel.url}; see {logs_dir / 'app-run-kernel.err.log'}")


def kernel_healthy(kernel_url: str, *, timeout_seconds: float = 1.0) -> bool:
    if _kernel_websocket_ready(kernel_url, timeout_seconds=timeout_seconds):
        return True
    try:
        health_url = _health_url(kernel_url)
    except ValueError:
        return False
    try:
        with urlopen(health_url, timeout=max(timeout_seconds, 0.1)) as resp:
            return 200 <= resp.status < 300
    except (OSError, URLError, ValueError):
        return False


def wait_for_runtime_ready(kernel_url: str, tenant_id: str, *, timeout_seconds: float = 10.0) -> bool:
    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        if _runtime_ready(kernel_url, tenant_id, timeout_seconds=min(1.0, max(0.1, deadline - time.monotonic()))):
            return True
        time.sleep(0.2)
    return False


def _runtime_ready(kernel_url: str, tenant_id: str, *, timeout_seconds: float) -> bool:
    try:
        snapshot_url = _internal_url(kernel_url, "/internal/snapshot/runtimes")
    except ValueError:
        return False
    try:
        with urlopen(snapshot_url, timeout=max(timeout_seconds, 0.1)) as resp:
            if resp.status < 200 or resp.status >= 300:
                return False
            data = json.loads(resp.read().decode("utf-8"))
    except (OSError, URLError, ValueError, json.JSONDecodeError):
        return False
    runtimes = data.get("runtimes") if isinstance(data, dict) else None
    if not isinstance(runtimes, list):
        return False
    for runtime in runtimes:
        if not isinstance(runtime, dict) or not runtime.get("attached"):
            continue
        served = runtime.get("tenants_served") or []
        if served and "*" not in served and tenant_id not in served:
            continue
        targets = runtime.get("targets") or []
        if not isinstance(targets, list):
            return True
        if not targets:
            return True
        for target in targets:
            if not isinstance(target, dict):
                continue
            target_tenants = target.get("tenants") or []
            if target_tenants and "*" not in target_tenants and tenant_id not in target_tenants:
                continue
            if target.get("state") in {"ready", "manifest_loaded", "initializing"}:
                return True
    return False


def _kernel_websocket_ready(kernel_url: str, *, timeout_seconds: float) -> bool:
    try:
        import websocket
    except ImportError:
        return False
    try:
        ws = websocket.create_connection(kernel_url, timeout=max(timeout_seconds, 0.1))
        try:
            token = os.environ.get("TABULA_KERNEL_TOKEN", "").strip()
            if not token:
                home = os.environ.get("TABULA_HOME", "").strip()
                if home:
                    try:
                        token = (Path(home) / "run" / "kernel-client-token").read_text(encoding="utf-8").strip()
                    except OSError:
                        token = ""
            ws.send(json.dumps({"v": 3, "type": "hello", "data": {"name": "tabula-install-ready", "send_topics": [], "receive_topics": [], "auth_token": token}}))
            msg = json.loads(ws.recv())
            return msg.get("type") == "hello_ack"
        finally:
            ws.close()
    except Exception:
        return False


def _serve_runtime_mode(runtimes: tuple[RuntimeTopology, ...]) -> str:
    modes = {runtime.mode for runtime in runtimes}
    if "managed" in modes:
        if sum(1 for runtime in runtimes if runtime.mode == "managed") > 1:
            raise AppRunError("multiple managed runtimes are not implemented yet")
        return "managed"
    return "external"


def _check_runtime_execution_support(runtimes: tuple[RuntimeTopology, ...]) -> None:
    for runtime in runtimes:
        backend = str(runtime.exec.get("backend", "")).strip()
        if runtime.mode == "managed" and backend not in {"bare"}:
            raise AppRunError(f"managed runtime execution backend {backend!r} is not implemented yet")


def _kernel_launch_argv(tabula_bin: str, runtime_mode: str) -> list[str]:
    if runtime_mode == "managed":
        return [_tabula_server_bin(tabula_bin)]
    return [tabula_bin, "serve", "--foreground", "--runtime-mode", "external"]


def _tabula_server_bin(tabula_bin: str) -> str:
    if any(sep in tabula_bin for sep in ("/", "\\")):
        return str(Path(tabula_bin).with_name("tabula-runner"))
    return "tabula-runner"


def _require_launch_binary(path: str, *, tabula_bin: str, runtime_mode: str) -> None:
    if any(sep in path for sep in ("/", "\\")):
        if Path(path).is_file():
            return
        if runtime_mode == "managed" and Path(path).name == "tabula-runner":
            raise AppRunError(
                f"tabula-runner not found at {path}; reinstall Tabula or run install-dev.sh for this TABULA_HOME"
            )
        raise AppRunError(f"launch binary not found at {path}")
    import shutil
    if shutil.which(path):
        return
    if runtime_mode == "managed":
        raise AppRunError(
            f"tabula-runner not found on PATH; reinstall Tabula or pass --tabula-bin pointing to an installed tabula binary"
        )
    raise AppRunError(f"launch binary {path!r} not found on PATH")


def _health_url(kernel_url: str) -> str:
    return _internal_url(kernel_url, "/health")


def _internal_url(kernel_url: str, path: str) -> str:
    parsed = urlparse(kernel_url)
    scheme = {"ws": "http", "wss": "https"}.get(parsed.scheme, parsed.scheme)
    if not scheme or not parsed.netloc:
        raise ValueError(f"invalid kernel url: {kernel_url}")
    return urlunparse((scheme, parsed.netloc, path, "", "", ""))


def write_runtime_config(manifest: AppManifest, home: Path) -> Path:
    path = home / "config" / "runtime.toml"
    tenant = manifest.application.id
    tenant_dir = home / "tenants" / tenant
    tenants = _merged_runtime_tenants(path, tenant, tenant_dir)
    tenant_ids = list(tenants)
    runtime_sock = _runtime_socket_path(home)

    doc = toml_io.load(path)
    doc["plugin_dirs"] = _string_array([])
    doc["skill_dirs"] = _string_array([])

    tenant_aot = tomlkit.aot()
    for tenant_id, dirs in tenants.items():
        entry = tomlkit.table()
        entry["id"] = tenant_id
        entry["plugin_dirs"] = _string_array(dirs["plugin_dirs"])
        entry["skill_dirs"] = _string_array(dirs["skill_dirs"])
        tenant_aot.append(entry)
    doc["tenant"] = tenant_aot

    kernel_aot = tomlkit.aot()
    kernel = tomlkit.table()
    kernel["id"] = "main"
    kernel["url"] = "unix://" + str(runtime_sock)
    kernel["token_file"] = str(home / "run" / "runtime-token")
    kernel["tenants"] = _string_array(tenant_ids)
    kernel_aot.append(kernel)
    doc["kernel"] = kernel_aot

    toml_io.dump(path, doc)
    return path


def _string_array(values: list[str]) -> tomlkit.items.Array:
    array = tomlkit.array()
    for value in values:
        array.append(value)
    return array


def _runtime_socket_path(home: Path) -> Path:
    default = home / "run" / "runtime.sock"
    if len(str(default)) <= 100:
        return default
    safe = "tabula-rt-" + hashlib.sha256(str(home).encode("utf-8")).hexdigest()[:16]
    return Path(gettempdir()) / safe / "runtime.sock"


def _merged_runtime_tenants(path: Path, tenant: str, tenant_dir: Path) -> dict[str, dict[str, list[str]]]:
    tenants: dict[str, dict[str, list[str]]] = {}
    if path.is_file():
        try:
            data = tomllib.loads(path.read_text(encoding="utf-8"))
        except (OSError, tomllib.TOMLDecodeError):
            data = {}
        for item in data.get("tenant") or []:
            if not isinstance(item, dict):
                continue
            tenant_id = str(item.get("id") or "").strip()
            if not tenant_id:
                continue
            tenants[tenant_id] = {
                "plugin_dirs": [str(value) for value in item.get("plugin_dirs") or [] if str(value).strip()],
                "skill_dirs": [str(value) for value in item.get("skill_dirs") or [] if str(value).strip()],
            }
    tenants[tenant] = {
        "plugin_dirs": [str(tenant_dir / "plugins")],
        "skill_dirs": [str(tenant_dir / "skills")],
    }
    return tenants

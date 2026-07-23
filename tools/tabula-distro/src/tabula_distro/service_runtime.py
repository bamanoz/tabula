"""Managed service readiness and launch helpers."""
from __future__ import annotations

import json
import os
import shutil
import subprocess
import time
from pathlib import Path
from urllib.error import URLError
from urllib.parse import urlparse, urlunparse
from urllib.request import Request, urlopen


class ServiceRuntimeError(RuntimeError):
    pass


def kernel_healthy(kernel_url: str, *, timeout_seconds: float = 1.0) -> bool:
    if _kernel_websocket_ready(kernel_url, timeout_seconds=timeout_seconds):
        return True
    try:
        health_url = _internal_url(kernel_url, "/health")
    except ValueError:
        return False
    try:
        with urlopen(health_url, timeout=max(timeout_seconds, 0.1)) as resp:
            return 200 <= resp.status < 300
    except (OSError, URLError, ValueError):
        return False


def wait_for_runtime_ready(
    kernel_url: str,
    tenant_id: str,
    *,
    home: Path | None = None,
    tabula_bin: str = "tabula",
    timeout_seconds: float = 10.0,
) -> bool:
    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        probe_timeout = min(1.0, max(0.1, deadline - time.monotonic()))
        if _runtime_ready(kernel_url, tenant_id, home=home, timeout_seconds=probe_timeout):
            return True
        if home is not None and _runtime_ready_from_status(
            home, tenant_id, tabula_bin=tabula_bin, timeout_seconds=probe_timeout
        ):
            return True
        time.sleep(0.2)
    return False


def request_runtime_reload(
    kernel_url: str, *, home: Path | None = None, timeout_seconds: float = 5.0
) -> str:
    try:
        reload_url = _internal_url(kernel_url, "/internal/reload/runtime", home=home)
    except ValueError as exc:
        return str(exc)
    request = Request(
        reload_url,
        data=b"{}",
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urlopen(request, timeout=max(timeout_seconds, 0.1)) as resp:
            if 200 <= resp.status < 300:
                return ""
            return f"POST {reload_url} returned HTTP {resp.status}"
    except (OSError, URLError, ValueError) as exc:
        return f"POST {reload_url}: {exc}"


def launch_argv(tabula_bin: str) -> list[str]:
    return [tabula_bin, "serve", "--foreground", "--runtime-mode", "managed"]


def require_launch_binary(path: str) -> None:
    if any(sep in path for sep in ("/", "\\")):
        if Path(path).is_file():
            return
        raise ServiceRuntimeError(f"tabula binary not found at {path}; reinstall Tabula")
    if shutil.which(path):
        return
    raise ServiceRuntimeError(f"tabula binary {path!r} not found on PATH")


def _runtime_ready(
    kernel_url: str,
    tenant_id: str,
    *,
    home: Path | None = None,
    timeout_seconds: float,
) -> bool:
    try:
        snapshot_url = _internal_url(kernel_url, "/internal/snapshot/runtimes", home=home)
    except ValueError:
        return False
    try:
        with urlopen(snapshot_url, timeout=max(timeout_seconds, 0.1)) as resp:
            if resp.status < 200 or resp.status >= 300:
                return False
            data = json.loads(resp.read().decode("utf-8"))
    except (OSError, URLError, ValueError, json.JSONDecodeError):
        return False
    return _runtime_ready_from_snapshot(data, tenant_id)


def _runtime_ready_from_status(
    home: Path,
    tenant_id: str,
    *,
    tabula_bin: str = "tabula",
    timeout_seconds: float,
) -> bool:
    tabula = Path(tabula_bin)
    if not tabula.is_absolute():
        candidate = home / "bin" / tabula_bin
        if candidate.is_file():
            tabula = candidate
    env = os.environ.copy()
    env["TABULA_HOME"] = str(home)
    try:
        result = subprocess.run(
            [str(tabula), "status", "--json"],
            env=env,
            timeout=max(timeout_seconds, 0.1),
            text=True,
            capture_output=True,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        return False
    if result.returncode != 0:
        return False
    try:
        data = json.loads(result.stdout)
    except json.JSONDecodeError:
        return False
    return _runtime_ready_from_snapshot(data, tenant_id)


def _runtime_ready_from_snapshot(data: object, tenant_id: str) -> bool:
    runtimes = data.get("runtimes") if isinstance(data, dict) else None
    if not isinstance(runtimes, list):
        return False
    for runtime in runtimes:
        if not isinstance(runtime, dict) or not runtime.get("attached"):
            continue
        served = runtime.get("tenants_served") or []
        if served and "*" not in served and tenant_id not in served:
            continue
        capabilities_by_tenant = runtime.get("capabilities_by_tenant")
        if isinstance(capabilities_by_tenant, dict):
            tenant_capabilities = capabilities_by_tenant.get(tenant_id)
            if isinstance(tenant_capabilities, list) and tenant_capabilities:
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
                        token = (
                            Path(home) / "run" / "kernel-client-token"
                        ).read_text(encoding="utf-8").strip()
                    except OSError:
                        token = ""
            ws.send(json.dumps({
                "v": 3,
                "type": "hello",
                "data": {
                    "name": "tabula-install-ready",
                    "send_topics": [],
                    "receive_topics": [],
                    "auth_token": token,
                },
            }))
            msg = json.loads(ws.recv())
            return msg.get("type") == "hello_ack"
        finally:
            ws.close()
    except Exception:
        return False


def _internal_url(kernel_url: str, path: str, *, home: Path | None = None) -> str:
    if home is not None:
        status_endpoint = _kernel_status_ws_endpoint(home)
        if status_endpoint:
            kernel_url = status_endpoint
    parsed = urlparse(kernel_url)
    scheme = {"ws": "http", "wss": "https"}.get(parsed.scheme, parsed.scheme)
    if not scheme or not parsed.netloc:
        raise ValueError(f"invalid kernel url: {kernel_url}")
    return urlunparse((scheme, parsed.netloc, path, "", "", ""))


def _kernel_status_ws_endpoint(home: Path) -> str:
    try:
        data = json.loads((home / "run" / "kernel-status.json").read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return ""
    endpoint = str(data.get("ws_endpoint") or "").strip() if isinstance(data, dict) else ""
    parsed = urlparse(endpoint)
    if parsed.scheme not in {"ws", "wss"} or not parsed.netloc:
        return ""
    return endpoint

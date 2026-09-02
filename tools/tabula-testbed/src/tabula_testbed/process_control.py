from __future__ import annotations

import json
import os
import signal
import socket
import subprocess
import time
from pathlib import Path
from urllib.parse import urlparse


def restart_kernel(*, timeout: float = 20) -> int:
    """Restart the runner-owned isolated kernel and return its new PID."""
    pid_file = _required_path("TABULA_TESTBED_KERNEL_PID_FILE")
    old_pid = _read_pid(pid_file)
    _stop_process(old_pid, timeout=5)

    raw_command = os.environ.get("TABULA_TESTBED_KERNEL_COMMAND", "")
    try:
        command = json.loads(raw_command)
    except json.JSONDecodeError as exc:
        raise RuntimeError("TABULA_TESTBED_KERNEL_COMMAND must be JSON argv") from exc
    if not isinstance(command, list) or not command or not all(isinstance(item, str) and item for item in command):
        raise RuntimeError("TABULA_TESTBED_KERNEL_COMMAND must be a non-empty JSON string array")

    out_path = _required_path("TABULA_TESTBED_KERNEL_OUT")
    err_path = _required_path("TABULA_TESTBED_KERNEL_ERR")
    out = out_path.open("a", encoding="utf-8")
    err = err_path.open("a", encoding="utf-8")
    creationflags = subprocess.CREATE_NEW_PROCESS_GROUP if os.name == "nt" else 0
    try:
        process = subprocess.Popen(command, env=os.environ.copy(), stdout=out, stderr=err, creationflags=creationflags)
    finally:
        out.close()
        err.close()
    pid_file.write_text(f"{process.pid}\n", encoding="utf-8")
    _wait_for_kernel(timeout)
    _wait_for_runtime(timeout)
    return process.pid


def restart_runtime(*, timeout: float = 20) -> int:
    """Kill the managed runtime and wait for the kernel to attach a replacement."""
    old_pid = runtime_pid()
    if old_pid <= 0:
        raise RuntimeError("managed runtime PID is unavailable")
    _stop_process(old_pid, timeout=5)

    deadline = time.time() + timeout
    last = ""
    while time.time() < deadline:
        try:
            pid = runtime_pid()
            if pid > 0 and pid != old_pid:
                return pid
        except (OSError, RuntimeError, subprocess.SubprocessError, json.JSONDecodeError) as exc:
            last = str(exc)
        time.sleep(0.2)
    raise RuntimeError(f"managed runtime did not restart: old_pid={old_pid} last={last}")


def runtime_pid() -> int:
    tabula_bin = _required_path("TABULA_TESTBED_TABULA_BIN")
    raw = subprocess.check_output([str(tabula_bin), "status", "--json"], env=os.environ.copy(), text=True)
    body = json.loads(raw)
    for runtime in body.get("runtimes", []):
        if isinstance(runtime, dict) and runtime.get("id") == "local" and runtime.get("attached"):
            pid = int(runtime.get("pid") or 0)
            if pid > 0:
                return pid
    if os.name != "nt":
        home = _required_path("TABULA_HOME")
        try:
            raw = subprocess.check_output(["pgrep", "-f", str(home / "config" / "runtime.toml")], text=True)
            for line in raw.splitlines():
                pid = int(line.strip())
                if pid > 0:
                    return pid
        except (OSError, ValueError, subprocess.SubprocessError):
            pass
    return 0


def kernel_pid() -> int:
    return _read_pid(_required_path("TABULA_TESTBED_KERNEL_PID_FILE"))


def _wait_for_kernel(timeout: float) -> None:
    parsed = urlparse(os.environ.get("TABULA_URL", ""))
    if not parsed.hostname or not parsed.port:
        raise RuntimeError("TABULA_URL must contain a host and port")
    deadline = time.time() + timeout
    last: OSError | None = None
    while time.time() < deadline:
        try:
            with socket.create_connection((parsed.hostname, parsed.port), timeout=1):
                return
        except OSError as exc:
            last = exc
            time.sleep(0.1)
    raise RuntimeError(f"kernel did not listen at {parsed.hostname}:{parsed.port}: {last}")


def _wait_for_runtime(timeout: float) -> None:
    deadline = time.time() + timeout
    last = ""
    while time.time() < deadline:
        try:
            pid = runtime_pid()
            if pid > 0:
                return
        except (OSError, RuntimeError, subprocess.SubprocessError, json.JSONDecodeError) as exc:
            last = str(exc)
        time.sleep(0.2)
    raise RuntimeError(f"managed runtime did not attach after kernel restart: {last}")


def _required_path(name: str) -> Path:
    value = os.environ.get(name, "").strip()
    if not value:
        raise RuntimeError(f"{name} is required for installed process control")
    return Path(value)


def _read_pid(path: Path) -> int:
    try:
        pid = int(path.read_text(encoding="utf-8").strip())
    except (OSError, ValueError) as exc:
        raise RuntimeError(f"invalid process PID file: {path}") from exc
    if pid <= 0:
        raise RuntimeError(f"invalid process PID in {path}: {pid}")
    return pid


def _stop_process(pid: int, *, timeout: float) -> None:
    if pid <= 0:
        return
    try:
        os.kill(pid, signal.CTRL_BREAK_EVENT if os.name == "nt" else signal.SIGTERM)
    except ProcessLookupError:
        return
    deadline = time.time() + timeout
    while time.time() < deadline:
        if not _process_alive(pid):
            return
        time.sleep(0.1)
    try:
        os.kill(pid, signal.SIGTERM if os.name == "nt" else signal.SIGKILL)
    except ProcessLookupError:
        return
    deadline = time.time() + 5
    while time.time() < deadline:
        if not _process_alive(pid):
            return
        time.sleep(0.1)
    raise RuntimeError(f"process did not stop: pid={pid}")


def _process_alive(pid: int) -> bool:
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return False
    except PermissionError:
        return True
    if os.name != "nt":
        try:
            state = subprocess.check_output(["ps", "-o", "stat=", "-p", str(pid)], text=True).strip()
            if state.startswith("Z"):
                return False
        except subprocess.SubprocessError:
            return False
    return True


__all__ = ["kernel_pid", "restart_kernel", "restart_runtime", "runtime_pid"]

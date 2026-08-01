"""Generic host-service manifests and external process lifecycle."""
from __future__ import annotations

import hashlib
import json
import os
import plistlib
import re
import shlex
import shutil
import signal
import subprocess
import sys
import time
import tomllib
from dataclasses import dataclass
from pathlib import Path

from . import links


class HostServiceError(RuntimeError):
    pass


@dataclass(frozen=True)
class Readiness:
    kind: str = "process"
    path: str = ""
    timeout_seconds: float = 10.0


@dataclass(frozen=True)
class HostServiceManifest:
    service_id: str
    entry: str
    args: tuple[str, ...]
    environment: dict[str, str]
    platforms: tuple[str, ...]
    readiness: Readiness
    shutdown_timeout_seconds: float


def load_manifest(root: Path) -> HostServiceManifest:
    path = root / "service.toml"
    try:
        with path.open("rb") as handle:
            data = tomllib.load(handle)
    except (OSError, tomllib.TOMLDecodeError) as exc:
        raise HostServiceError(f"read host-service manifest {path}: {exc}") from exc
    unknown = set(data) - {"service", "readiness", "shutdown"}
    if unknown:
        raise HostServiceError(f"{path}: unsupported section {sorted(unknown)[0]!r}")
    service = data.get("service")
    if not isinstance(service, dict):
        raise HostServiceError(f"{path}: [service] is required")
    unknown_service = set(service) - {"id", "entry", "args", "environment", "platforms"}
    if unknown_service:
        raise HostServiceError(f"{path}: unsupported [service] field {sorted(unknown_service)[0]!r}")
    service_id = _simple_name(path, "service.id", service.get("id"))
    entry = _relative_file(path, "service.entry", service.get("entry"))
    entry_path = root / entry
    if not entry_path.is_file():
        raise HostServiceError(f"{path}: service.entry does not exist: {entry}")
    if not os.access(entry_path, os.X_OK):
        raise HostServiceError(f"{path}: service.entry is not executable: {entry}")
    args = _strings(path, "service.args", service.get("args", []))
    environment_raw = service.get("environment", {})
    if not isinstance(environment_raw, dict) or not all(
        isinstance(key, str) and key and isinstance(value, str)
        for key, value in environment_raw.items()
    ):
        raise HostServiceError(f"{path}: service.environment must be a string table")
    platforms = _strings(path, "service.platforms", service.get("platforms", ["darwin", "linux"]))
    if not platforms or any(item not in {"darwin", "linux"} for item in platforms):
        raise HostServiceError(f"{path}: service.platforms entries must be darwin or linux")

    readiness_raw = data.get("readiness", {})
    if not isinstance(readiness_raw, dict):
        raise HostServiceError(f"{path}: [readiness] must be a table")
    unknown_readiness = set(readiness_raw) - {"kind", "path", "timeout_seconds"}
    if unknown_readiness:
        raise HostServiceError(f"{path}: unsupported [readiness] field {sorted(unknown_readiness)[0]!r}")
    readiness_kind = str(readiness_raw.get("kind") or "process").strip()
    if readiness_kind not in {"process", "file"}:
        raise HostServiceError(f"{path}: readiness.kind must be process or file")
    readiness_path = str(readiness_raw.get("path") or "").strip()
    if readiness_kind == "file":
        readiness_path = _relative_path(path, "readiness.path", readiness_path)
    timeout = _positive_number(path, "readiness.timeout_seconds", readiness_raw.get("timeout_seconds", 10))

    shutdown_raw = data.get("shutdown", {})
    if not isinstance(shutdown_raw, dict):
        raise HostServiceError(f"{path}: [shutdown] must be a table")
    unknown_shutdown = set(shutdown_raw) - {"timeout_seconds"}
    if unknown_shutdown:
        raise HostServiceError(f"{path}: unsupported [shutdown] field {sorted(unknown_shutdown)[0]!r}")
    shutdown_timeout = _positive_number(path, "shutdown.timeout_seconds", shutdown_raw.get("timeout_seconds", 10))
    return HostServiceManifest(
        service_id=service_id,
        entry=entry,
        args=args,
        environment=dict(environment_raw),
        platforms=platforms,
        readiness=Readiness(readiness_kind, readiness_path, timeout),
        shutdown_timeout_seconds=shutdown_timeout,
    )


def artifact_digest(root: Path) -> str:
    digest = hashlib.sha256()
    for path in sorted(item for item in root.rglob("*") if item.is_file()):
        rel = path.relative_to(root).as_posix()
        digest.update(rel.encode("utf-8"))
        digest.update(b"\0")
        digest.update(b"x" if os.access(path, os.X_OK) else b"-")
        with path.open("rb") as handle:
            while chunk := handle.read(65536):
                digest.update(chunk)
        digest.update(b"\n")
    return digest.hexdigest()


def reconcile(
    home: Path,
    distro_path: Path,
    *,
    service_id: str | None = None,
    start: bool = True,
    adapter: str = "auto",
) -> list[dict[str, object]]:
    roots = _service_roots(distro_path)
    if service_id is not None:
        roots = [root for root in roots if root.name == service_id]
        if not roots:
            raise HostServiceError(f"host service {service_id!r} is not installed by {distro_path}")
    receipts: list[dict[str, object]] = []
    declared: set[str] = set()
    for source_root in roots:
        manifest = load_manifest(source_root)
        declared.add(manifest.service_id)
        if manifest.service_id != source_root.name:
            raise HostServiceError(
                f"{source_root / 'service.toml'}: service.id {manifest.service_id!r} must match directory name {source_root.name!r}"
            )
        _require_platform(manifest)
        release = _materialize_release(home, source_root, manifest)
        receipts.append(
            _activate(home, release, manifest, owner=distro_path.name, start=start, adapter=adapter)
        )
    if service_id is None:
        for installed in list_services(home):
            installed_id = str(installed["id"])
            owner = _read_text(_service_root(home, installed_id) / "owner")
            if owner == distro_path.name and installed_id not in declared:
                receipts.append(remove(home, installed_id))
    return receipts


def status(home: Path, service_id: str) -> dict[str, object]:
    root = _service_root(home, service_id)
    current = links.resolve_reference(root / "current")
    pid = _read_pid(root)
    selected = _read_text(root / "adapter")
    ownership_mismatch = False
    if selected == "launchd":
        running = _launchd_running(service_id)
    else:
        exists = pid is not None and _process_exists(pid)
        running = exists and pid is not None and _pid_matches_release(root, pid)
        ownership_mismatch = exists and not running
        if pid is not None and not exists:
            _pid_path(root).unlink(missing_ok=True)
            pid = None
    return {
        "id": service_id,
        "installed": current is not None and current.is_dir(),
        "release": current.name if current is not None else "",
        "running": running,
        "pid": pid,
        "adapter": selected,
        "ownership_mismatch": ownership_mismatch,
        "last_receipt": _read_json(root / "last-receipt.json"),
    }


def list_services(home: Path) -> list[dict[str, object]]:
    root = home / "host-services"
    if not root.is_dir():
        return []
    return [status(home, entry.name) for entry in sorted(root.iterdir()) if entry.is_dir()]


def start(home: Path, service_id: str, *, adapter: str = "auto") -> dict[str, object]:
    root = _service_root(home, service_id)
    release = links.resolve_reference(root / "current")
    if release is None or not release.is_dir():
        raise HostServiceError(f"host service {service_id!r} has no active release")
    manifest = load_manifest(release)
    _require_platform(manifest)
    return _start_release(home, release, manifest, adapter=adapter)


def stop(home: Path, service_id: str) -> dict[str, object]:
    root = _service_root(home, service_id)
    release = links.resolve_reference(root / "current")
    manifest = load_manifest(release) if release is not None and release.is_dir() else None
    selected = _read_text(root / "adapter") or "process"
    if selected == "launchd":
        _launchd_stop(service_id)
    else:
        _process_stop(root, manifest.shutdown_timeout_seconds if manifest else 10.0)
    return _write_receipt(root, "stopped", release=release.name if release else "", adapter=selected)


def restart(home: Path, service_id: str, *, adapter: str = "auto") -> dict[str, object]:
    stop(home, service_id)
    return start(home, service_id, adapter=adapter)


def remove(home: Path, service_id: str, *, purge: bool = False) -> dict[str, object]:
    root = _service_root(home, service_id)
    selected = _read_text(root / "adapter")
    if root.exists():
        stop(home, service_id)
    if selected == "launchd":
        _launchd_remove(service_id)
    receipt = _write_receipt(
        root,
        "purged" if purge else "removed",
        release="",
        adapter=selected or "",
    )
    links.remove_path(root / "current")
    links.remove_path(root / "previous")
    if purge:
        shutil.rmtree(root, ignore_errors=True)
    else:
        shutil.rmtree(root / "releases", ignore_errors=True)
        for path in (root / "adapter", root / "owner", root / "last-receipt.json", root / "transaction.json"):
            path.unlink(missing_ok=True)
    return receipt


def _activate(
    home: Path,
    release: Path,
    manifest: HostServiceManifest,
    *,
    owner: str,
    start: bool,
    adapter: str,
) -> dict[str, object]:
    root = _service_root(home, manifest.service_id)
    _recover_transaction(home, root, adapter=adapter)
    owner_path = root / "owner"
    existing_owner = _read_text(owner_path)
    if existing_owner and existing_owner != owner:
        raise HostServiceError(
            f"host service {manifest.service_id!r} is owned by distro {existing_owner!r}, not {owner!r}"
        )
    owner_path.parent.mkdir(parents=True, exist_ok=True)
    owner_path.write_text(owner + "\n", encoding="utf-8")
    current = links.resolve_reference(root / "current")
    persisted_adapter = _read_text(root / "adapter")
    if current == release:
        if start and not bool(status(home, manifest.service_id)["running"]):
            return _start_release(home, release, manifest, adapter=persisted_adapter or adapter)
        return _write_receipt(
            root,
            "unchanged",
            release=release.name,
            adapter=persisted_adapter or (adapter if adapter != "auto" else ""),
        )

    transaction = root / "transaction.json"
    was_running = bool(status(home, manifest.service_id)["running"])
    previous_adapter = persisted_adapter or (adapter if adapter != "auto" else "")
    _write_json(transaction, {
        "version": 1,
        "phase": "prepared",
        "candidate": release.name,
        "previous": current.name if current is not None else "",
        "was_running": was_running,
        "previous_adapter": previous_adapter,
    })
    if was_running:
        stop(home, manifest.service_id)
    if current is not None:
        links.replace_directory_reference(root / "previous", current)
    links.replace_directory_reference(root / "current", release)
    _write_json(transaction, {
        "version": 1,
        "phase": "switched",
        "candidate": release.name,
        "previous": current.name if current is not None else "",
        "was_running": was_running,
        "previous_adapter": previous_adapter,
    })
    try:
        if start or was_running:
            receipt = _start_release(home, release, manifest, adapter=adapter)
        else:
            receipt = _write_receipt(
                root,
                "installed",
                release=release.name,
                adapter=previous_adapter or (adapter if adapter != "auto" else ""),
            )
    except Exception as activation_error:
        links.remove_path(root / "current")
        rollback_error: Exception | None = None
        if current is not None:
            links.replace_directory_reference(root / "current", current)
            if was_running:
                try:
                    previous_manifest = load_manifest(current)
                    _start_release(
                        home,
                        current,
                        previous_manifest,
                        adapter=previous_adapter or adapter,
                    )
                except Exception as exc:
                    rollback_error = exc
        receipt_adapter = previous_adapter or (adapter if adapter != "auto" else "")
        _write_receipt(
            root,
            "rollback_failed" if rollback_error is not None else "rolled_back",
            release=current.name if current else "",
            adapter=receipt_adapter,
            error=str(rollback_error or activation_error),
        )
        transaction.unlink(missing_ok=True)
        if rollback_error is not None:
            raise HostServiceError(
                f"host service {manifest.service_id!r} activation failed: {activation_error}; "
                f"rollback restart failed: {rollback_error}"
            ) from activation_error
        raise
    transaction.unlink(missing_ok=True)
    return receipt


def _recover_transaction(home: Path, root: Path, *, adapter: str) -> None:
    transaction = root / "transaction.json"
    payload = _read_json(transaction)
    if payload is None:
        return
    if not isinstance(payload, dict) or payload.get("version") != 1 or payload.get("phase") not in {"prepared", "switched"}:
        raise HostServiceError(f"invalid host-service transaction: {transaction}")
    candidate_name = str(payload.get("candidate") or "")
    previous_name = str(payload.get("previous") or "")
    was_running = payload.get("was_running") is True
    previous_adapter = str(payload.get("previous_adapter") or "")
    releases = root / "releases"
    current = links.resolve_reference(root / "current")
    candidate = releases / candidate_name if candidate_name else None
    previous = releases / previous_name if previous_name else None
    if was_running and current is not None and candidate is not None and current == candidate:
        selected = _read_text(root / "adapter") or previous_adapter or _select_adapter(adapter)
        if selected == "launchd":
            _launchd_stop(root.name)
        else:
            _process_stop(root, 10.0)
    links.remove_path(root / "current")
    if previous is not None and previous.is_dir():
        links.replace_directory_reference(root / "current", previous)
        if was_running:
            try:
                _start_release(
                    home,
                    previous,
                    load_manifest(previous),
                    adapter=previous_adapter or adapter,
                )
            except Exception as exc:
                transaction.unlink(missing_ok=True)
                _write_receipt(
                    root,
                    "recovery_failed",
                    release=previous.name,
                    adapter=previous_adapter or (adapter if adapter != "auto" else ""),
                    error=str(exc),
                )
                raise HostServiceError(
                    f"host service {root.name!r} recovered previous release but failed to restart it: {exc}"
                ) from exc
    transaction.unlink(missing_ok=True)
    _write_receipt(
        root,
        "recovered",
        release=previous.name if previous is not None and previous.is_dir() else "",
        adapter=_read_text(root / "adapter") or previous_adapter or (adapter if adapter != "auto" else ""),
    )


def _materialize_release(home: Path, source_root: Path, manifest: HostServiceManifest) -> Path:
    digest = artifact_digest(source_root)
    root = _service_root(home, manifest.service_id)
    release = root / "releases" / digest
    if release.is_dir():
        if artifact_digest(release) != digest:
            raise HostServiceError(f"host service {manifest.service_id!r} retained release hash mismatch: {release}")
        return release
    staging = root / "staging" / digest
    shutil.rmtree(staging, ignore_errors=True)
    staging.parent.mkdir(parents=True, exist_ok=True)
    shutil.copytree(source_root, staging, symlinks=False)
    if artifact_digest(staging) != digest:
        shutil.rmtree(staging, ignore_errors=True)
        raise HostServiceError(f"host service {manifest.service_id!r} changed while materializing")
    release.parent.mkdir(parents=True, exist_ok=True)
    os.replace(staging, release)
    try:
        staging.parent.rmdir()
    except OSError:
        pass
    return release


def _start_release(home: Path, release: Path, manifest: HostServiceManifest, *, adapter: str) -> dict[str, object]:
    root = _service_root(home, manifest.service_id)
    if manifest.readiness.kind == "file":
        readiness_path = home / manifest.readiness.path
        readiness_path.parent.mkdir(parents=True, exist_ok=True)
        readiness_path.unlink(missing_ok=True)
    selected = _select_adapter(adapter)
    if selected == "launchd":
        _launchd_start(home, release, manifest)
        pid = None
    else:
        pid = _process_start(home, release, manifest)
    (root / "adapter").write_text(selected + "\n", encoding="utf-8")
    if not _wait_ready(home, root, manifest, pid, adapter=selected):
        if selected == "launchd":
            _launchd_stop(manifest.service_id)
        else:
            _process_stop(root, manifest.shutdown_timeout_seconds)
        raise HostServiceError(
            f"host service {manifest.service_id!r} did not become ready within {manifest.readiness.timeout_seconds:g}s"
        )
    return _write_receipt(root, "ready", release=release.name, adapter=selected, pid=pid)


def _process_start(home: Path, release: Path, manifest: HostServiceManifest) -> int:
    root = _service_root(home, manifest.service_id)
    existing = _read_pid(root)
    if existing is not None and _process_exists(existing):
        if _pid_matches_release(root, existing):
            return existing
        _pid_path(root).unlink(missing_ok=True)
        raise HostServiceError(
            f"refusing to reuse pid {existing}: it is not owned by host service {manifest.service_id!r}"
        )
    env = _service_env(home, root, manifest)
    logs = home / "logs" / "host-services"
    logs.mkdir(parents=True, exist_ok=True)
    stdout_fd = os.open(logs / f"{manifest.service_id}.out.log", os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
    stderr_fd = os.open(logs / f"{manifest.service_id}.err.log", os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
    argv = [str(release / manifest.entry), *manifest.args]
    command = f"cd {shlex.quote(str(release))} && exec {shlex.join(argv)}"
    try:
        pid = os.posix_spawn(
            "/bin/sh",
            ["/bin/sh", "-c", command],
            env,
            file_actions=[
                (os.POSIX_SPAWN_DUP2, stdout_fd, 1),
                (os.POSIX_SPAWN_DUP2, stderr_fd, 2),
                (os.POSIX_SPAWN_CLOSE, stdout_fd),
                (os.POSIX_SPAWN_CLOSE, stderr_fd),
            ],
            setsid=True,
        )
    finally:
        os.close(stdout_fd)
        os.close(stderr_fd)
    _pid_path(root).parent.mkdir(parents=True, exist_ok=True)
    _pid_path(root).write_text(f"{pid}\n", encoding="utf-8")
    return pid


def _process_stop(root: Path, timeout_seconds: float) -> None:
    pid = _read_pid(root)
    if pid is None:
        return
    if _process_exists(pid) and not _pid_matches_release(root, pid):
        _pid_path(root).unlink(missing_ok=True)
        raise HostServiceError(f"refusing to stop pid {pid}: it is not owned by host service {root.name!r}")
    if _process_group_exists(pid):
        try:
            os.killpg(pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        deadline = time.monotonic() + timeout_seconds
        while time.monotonic() < deadline and _process_group_exists(pid):
            _process_exists(pid)
            time.sleep(0.05)
        if _process_group_exists(pid):
            os.killpg(pid, signal.SIGKILL)
            deadline = time.monotonic() + 1.0
            while time.monotonic() < deadline and _process_group_exists(pid):
                _process_exists(pid)
                time.sleep(0.05)
    _process_exists(pid)
    _pid_path(root).unlink(missing_ok=True)


def _launchd_start(home: Path, release: Path, manifest: HostServiceManifest) -> None:
    if sys.platform != "darwin":
        raise HostServiceError("launchd host-service adapter is supported only on macOS")
    plist = _launchd_plist_path(manifest.service_id)
    plist.parent.mkdir(parents=True, exist_ok=True)
    env = _service_env(home, _service_root(home, manifest.service_id), manifest)
    logs = home / "logs" / "host-services"
    logs.mkdir(parents=True, exist_ok=True)
    payload = {
        "Label": _launchd_label(manifest.service_id),
        "ProgramArguments": [str(release / manifest.entry), *manifest.args],
        "WorkingDirectory": str(release),
        "EnvironmentVariables": env,
        "RunAtLoad": True,
        "KeepAlive": {"SuccessfulExit": False},
        "StandardOutPath": str(logs / f"{manifest.service_id}.out.log"),
        "StandardErrorPath": str(logs / f"{manifest.service_id}.err.log"),
    }
    tmp = plist.with_suffix(".tmp")
    with tmp.open("wb") as handle:
        plistlib.dump(payload, handle)
    os.replace(tmp, plist)
    subprocess.run(["launchctl", "bootout", f"gui/{os.getuid()}", str(plist)], check=False, capture_output=True)
    result = subprocess.run(
        ["launchctl", "bootstrap", f"gui/{os.getuid()}", str(plist)],
        text=True,
        capture_output=True,
        check=False,
    )
    if result.returncode != 0:
        raise HostServiceError(f"launchctl bootstrap {manifest.service_id}: {result.stderr.strip() or result.stdout.strip()}")


def _launchd_stop(service_id: str) -> None:
    if sys.platform != "darwin":
        return
    plist = _launchd_plist_path(service_id)
    subprocess.run(["launchctl", "bootout", f"gui/{os.getuid()}", str(plist)], check=False, capture_output=True)


def _launchd_remove(service_id: str) -> None:
    _launchd_stop(service_id)
    _launchd_plist_path(service_id).unlink(missing_ok=True)


def _launchd_running(service_id: str) -> bool:
    if sys.platform != "darwin":
        return False
    result = subprocess.run(
        ["launchctl", "print", f"gui/{os.getuid()}/{_launchd_label(service_id)}"],
        text=True,
        check=False,
        capture_output=True,
    )
    if result.returncode != 0:
        return False
    state = re.search(r"^\s*state\s*=\s*(\S+)", result.stdout, re.MULTILINE)
    pid = re.search(r"^\s*pid\s*=\s*(\d+)", result.stdout, re.MULTILINE)
    return state is not None and state.group(1) == "running" and pid is not None and int(pid.group(1)) > 0


def _wait_ready(
    home: Path,
    root: Path,
    manifest: HostServiceManifest,
    pid: int | None,
    *,
    adapter: str,
) -> bool:
    deadline = time.monotonic() + manifest.readiness.timeout_seconds
    while time.monotonic() < deadline:
        if manifest.readiness.kind == "file":
            if (home / manifest.readiness.path).is_file():
                return True
        elif adapter == "launchd":
            if _launchd_running(manifest.service_id):
                return True
        elif pid is not None and _process_exists(pid):
            return True
        time.sleep(0.05)
    return False


def _service_env(home: Path, root: Path, manifest: HostServiceManifest) -> dict[str, str]:
    env = os.environ.copy()
    env.update(manifest.environment)
    state = root / "state"
    state.mkdir(parents=True, exist_ok=True)
    env.update({
        "TABULA_HOME": str(home),
        "TABULA_HOST_SERVICE_ID": manifest.service_id,
        "TABULA_HOST_SERVICE_STATE_DIR": str(state),
    })
    return env


def _select_adapter(adapter: str) -> str:
    requested = (adapter or "auto").strip()
    if requested == "auto":
        if sys.platform == "darwin":
            return "launchd"
        raise HostServiceError(
            f"no persistent host-service adapter for platform {sys.platform}; use --adapter process only for foreground/testbed execution"
        )
    if requested not in {"launchd", "process"}:
        raise HostServiceError(f"unsupported host-service adapter: {requested}")
    return requested


def _require_platform(manifest: HostServiceManifest) -> None:
    platform = "darwin" if sys.platform == "darwin" else "linux" if sys.platform.startswith("linux") else sys.platform
    if platform not in manifest.platforms:
        raise HostServiceError(
            f"host service {manifest.service_id!r} does not support platform {platform}; supports {', '.join(manifest.platforms)}"
        )


def _service_roots(distro_path: Path) -> list[Path]:
    root = distro_path / "host-services"
    if not root.is_dir():
        return []
    return [entry for entry in sorted(root.iterdir()) if entry.is_dir()]


def _service_root(home: Path, service_id: str) -> Path:
    if not service_id or Path(service_id).name != service_id or service_id in {".", ".."}:
        raise HostServiceError(f"invalid host-service id: {service_id!r}")
    return home / "host-services" / service_id


def _pid_path(root: Path) -> Path:
    return root / "run" / "pid"


def _read_pid(root: Path) -> int | None:
    try:
        pid = int(_pid_path(root).read_text(encoding="utf-8").strip())
    except (OSError, ValueError):
        return None
    return pid if pid > 0 else None


def _process_exists(pid: int) -> bool:
    try:
        reaped, _status = os.waitpid(pid, os.WNOHANG)
        if reaped == pid:
            return False
    except ChildProcessError:
        pass
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return False
    except PermissionError:
        return True
    return True


def _process_group_exists(pid: int) -> bool:
    try:
        os.killpg(pid, 0)
    except ProcessLookupError:
        return False
    except PermissionError:
        return True
    return True


def _pid_matches_release(root: Path, pid: int) -> bool:
    current = links.resolve_reference(root / "current")
    if current is None:
        return False
    try:
        manifest = load_manifest(current)
        result = subprocess.run(
            ["ps", "-p", str(pid), "-o", "command="],
            text=True,
            capture_output=True,
            check=False,
            timeout=2.0,
        )
    except (HostServiceError, OSError, subprocess.TimeoutExpired):
        return False
    if result.returncode != 0:
        return False
    executable = str((current / manifest.entry).resolve())
    try:
        command = shlex.split(result.stdout.strip())
    except ValueError:
        return False
    return any(str(Path(token).expanduser().resolve()) == executable for token in command if "/" in token)


def _launchd_label(service_id: str) -> str:
    return f"ai.tabula.host-service.{service_id}"


def _launchd_plist_path(service_id: str) -> Path:
    return Path.home() / "Library" / "LaunchAgents" / f"{_launchd_label(service_id)}.plist"


def _write_receipt(root: Path, status_value: str, **values: object) -> dict[str, object]:
    receipt = {"version": 1, "status": status_value, "time": _now_iso(), **values}
    _write_json(root / "last-receipt.json", receipt)
    history = root / "receipts.jsonl"
    history.parent.mkdir(parents=True, exist_ok=True)
    with history.open("a", encoding="utf-8") as handle:
        handle.write(json.dumps(receipt, sort_keys=True) + "\n")
    return receipt


def _write_json(path: Path, value: object) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    os.replace(tmp, path)


def _read_json(path: Path) -> object | None:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None


def _read_text(path: Path) -> str:
    try:
        return path.read_text(encoding="utf-8").strip()
    except OSError:
        return ""


def _simple_name(path: Path, field: str, value: object) -> str:
    text = str(value or "").strip()
    if not text or Path(text).name != text or text in {".", ".."}:
        raise HostServiceError(f"{path}: {field} must be a simple name")
    return text


def _relative_file(path: Path, field: str, value: object) -> str:
    return _relative_path(path, field, str(value or "").strip())


def _relative_path(path: Path, field: str, value: str) -> str:
    rel = Path(value)
    if not value or rel.is_absolute() or ".." in rel.parts:
        raise HostServiceError(f"{path}: {field} must be relative and stay inside its root")
    return rel.as_posix()


def _strings(path: Path, field: str, value: object) -> tuple[str, ...]:
    if not isinstance(value, list) or not all(isinstance(item, str) and item.strip() for item in value):
        raise HostServiceError(f"{path}: {field} must be an array of non-empty strings")
    return tuple(item.strip() for item in value)


def _positive_number(path: Path, field: str, value: object) -> float:
    if isinstance(value, bool) or not isinstance(value, (int, float)) or value <= 0:
        raise HostServiceError(f"{path}: {field} must be greater than zero")
    return float(value)


def _now_iso() -> str:
    import datetime as dt

    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")

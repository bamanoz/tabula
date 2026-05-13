from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path


class RuntimeConfigError(RuntimeError):
    pass


def sync_for_distro(home: Path, boot_path: Path) -> Path:
    boot_path = boot_path.expanduser().resolve()
    if not boot_path.is_file():
        raise RuntimeConfigError(f"boot script does not exist at {boot_path}")
    env = os.environ.copy()
    env["TABULA_HOME"] = str(home)
    env.setdefault("TABULA_URL", "ws://localhost:8089/ws")
    env["PATH"] = env.get("TABULA_PATH") or _default_path(home, env.get("PATH", ""))
    proc = subprocess.run(
        [sys.executable, str(boot_path)],
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    if proc.returncode != 0:
        detail = (proc.stderr or proc.stdout).strip()
        if detail:
            raise RuntimeConfigError(f"boot script failed: {detail}")
        raise RuntimeConfigError(f"boot script failed with exit code {proc.returncode}")
    try:
        payload = json.loads(proc.stdout)
    except json.JSONDecodeError as exc:
        raise RuntimeConfigError(f"cannot parse boot output: {exc}") from exc
    plugin_dirs = _plugin_dirs(home, payload)
    return write(home, plugin_dirs)


def write(home: Path, plugin_dirs: list[str]) -> Path:
    path = home / "config" / "runtime.toml"
    path.parent.mkdir(parents=True, exist_ok=True)
    runtime_sock = _runtime_socket_path(home)
    payload = "\n".join([
        f"plugin_dirs = [{', '.join(json.dumps(item) for item in plugin_dirs)}]",
        f"skill_dirs = [{json.dumps(str(home / 'skills'))}]",
        "",
        "[[kernel]]",
        'id = "main"',
        f"url = {json.dumps('unix://' + str(runtime_sock))}",
        f"token_file = {json.dumps(str(home / 'run' / 'runtime-token'))}",
        'tenants = ["*"]',
        "",
        "[pool]",
        "cold_workers_per_tenant_max = 16",
        "",
    ])
    path.write_text(payload, encoding="utf-8")
    return path


def _plugin_dirs(home: Path, payload: object) -> list[str]:
    plugins = payload.get("plugins") if isinstance(payload, dict) else None
    dirs: list[str] = []
    if isinstance(plugins, list):
        seen: set[str] = set()
        for item in plugins:
            if not isinstance(item, dict):
                continue
            manifest_path = str(item.get("manifest_path") or "").strip()
            if not manifest_path:
                continue
            resolved = str(Path(manifest_path).expanduser().resolve())
            if resolved in seen:
                continue
            seen.add(resolved)
            dirs.append(resolved)
    if dirs:
        return dirs
    return [str(home / "plugins")]


def _default_path(home: Path, current: str) -> str:
    extras = [str(home / ".venv" / "bin"), str(home / "bin")]
    if current:
        extras.append(current)
    return os.pathsep.join(extras)


def _runtime_socket_path(home: Path) -> Path:
    default = home / "run" / "runtime.sock"
    if len(str(default)) <= 100:
        return default
    import hashlib
    from tempfile import gettempdir

    safe = "tabula-rt-" + hashlib.sha256(str(home).encode("utf-8")).hexdigest()[:16]
    return Path(gettempdir()) / safe / "runtime.sock"

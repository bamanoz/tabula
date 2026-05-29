from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path

import tomlkit

from tabula_distro import toml_io


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
    distro = distro_metadata(home, boot_path)
    return write(home, plugin_dirs, distro=distro)


def write(home: Path, plugin_dirs: list[str], *, distro: dict[str, str] | None = None) -> Path:
    """Write runtime.toml for the active distro.

    Re-runs of this function preserve any user comments and unknown keys the
    user added to the file. Only the keys we own (``plugin_dirs``,
    ``skill_dirs``, the ``[[kernel]]`` entry, ``[pool]``, and ``[distro]``)
    are authoritative — they are overwritten to match the install state.
    """
    path = home / "config" / "runtime.toml"
    runtime_sock = _runtime_socket_path(home)
    doc = toml_io.load(path)

    doc["plugin_dirs"] = _string_array(plugin_dirs)
    doc["skill_dirs"] = _string_array([str(home / "skills")])

    kernels = tomlkit.aot()
    kernel = tomlkit.table()
    kernel["id"] = "main"
    kernel["url"] = "unix://" + str(runtime_sock)
    kernel["token_file"] = str(home / "run" / "runtime-token")
    kernel["tenants"] = _string_array(["*"])
    kernels.append(kernel)
    doc["kernel"] = kernels

    toml_io.merge_defaults(doc, {"pool": {"cold_workers_per_tenant_max": 16}})

    if distro is not None:
        # The trust check (kernel-side) reads these two keys. Active is a
        # human-readable id ("code-immune") and dir points at the distro
        # source tree the SHA is computed over. Both come from the
        # installer because only it knows which generation is active.
        distro_table = tomlkit.table()
        distro_table["active"] = distro["active"]
        distro_table["dir"] = distro["dir"]
        doc["distro"] = distro_table

    toml_io.dump(path, doc)
    return path


def distro_metadata(home: Path, boot_path: Path) -> dict[str, str]:
    """Resolve distro id and source directory from a boot.py path.

    The installer lays out boot scripts at
    ``$TABULA_HOME/distrib/<distro>/generations/<gen>/boot.py``. We trust the
    parent directory of ``boot.py`` as the distro source tree (it contains
    ``boot.py``, ``distro.toml``, ``application/``, etc) and lift the distro
    id from two levels up.
    """
    distro_dir = boot_path.parent
    parent = distro_dir.parent
    if parent.name == "generations":
        active = parent.parent.name
    else:
        active = parent.name
    return {"active": active, "dir": str(distro_dir)}


def _string_array(values: list[str]) -> tomlkit.items.Array:
    array = tomlkit.array()
    for value in values:
        array.append(value)
    return array


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

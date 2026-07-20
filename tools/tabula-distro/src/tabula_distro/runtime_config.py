from __future__ import annotations

import os
import sys
from pathlib import Path

from tabula_distro import toml_io


class RuntimeConfigError(RuntimeError):
    pass


def sync_for_distro(home: Path, distro_path: Path) -> Path:
    distro_dir = _distro_dir(distro_path)
    if not distro_dir.is_dir():
        raise RuntimeConfigError(f"distro generation does not exist at {distro_dir}")
    plugin_dirs = [str(distro_dir / "plugins")]
    distro = distro_metadata(home, distro_dir)
    write_kernel_config(home, url=os.environ.get("TABULA_URL", "ws://localhost:8089/ws"))
    return write(home, plugin_dirs, distro=distro)


def write_kernel_config(home: Path, *, url: str = "ws://localhost:8089/ws") -> Path:
    path = home / "config" / "kernel.toml"
    doc = toml_io.load(path)
    tomlkit = toml_io.require_tomlkit()
    kernel = doc.get("kernel")
    if not hasattr(kernel, "__setitem__"):
        kernel = tomlkit.table()
    kernel["url"] = str(url).strip() or "ws://localhost:8089/ws"
    doc["kernel"] = kernel
    runtime_wss = doc.get("runtime_wss")
    if not hasattr(runtime_wss, "__setitem__"):
        runtime_wss = tomlkit.table()
        runtime_wss["enabled"] = False
        doc["runtime_wss"] = runtime_wss
    toml_io.dump(path, doc)
    return path


def write(home: Path, plugin_dirs: list[str], *, distro: dict[str, str] | None = None) -> Path:
    """Write runtime.toml for the active distro.

    Re-runs of this function preserve any user comments and unknown keys the
    user added to the file. Only the keys we own (``plugin_dirs``,
    ``skill_dirs``, the ``[[kernel]]`` entry, ``[runtimes.python]``,
    ``[pool]``, and ``[distro]``)
    are authoritative — they are overwritten to match the install state.
    """
    path = home / "config" / "runtime.toml"
    runtime_sock = runtime_socket_path(home)
    doc = toml_io.load(path)
    tomlkit = toml_io.require_tomlkit()

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

    write_python_runtime(doc)

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


def write_python_runtime(doc, *, executable: str | Path | None = None) -> None:
    """Record the concrete interpreter behind the platform-neutral `python` runtime."""
    tomlkit = toml_io.require_tomlkit()
    runtimes = doc.get("runtimes")
    if not hasattr(runtimes, "__setitem__"):
        runtimes = tomlkit.table()
    python = tomlkit.table()
    # A POSIX virtualenv's python executable is commonly a symlink to the base
    # interpreter. Resolving it drops the virtualenv and its site-packages.
    interpreter = Path(os.path.abspath(Path(executable or sys.executable).expanduser()))
    python["command"] = _string_array([str(interpreter)])
    runtimes["python"] = python
    doc["runtimes"] = runtimes


def distro_metadata(home: Path, distro_dir: Path) -> dict[str, str]:
    """Resolve distro id and source directory from a generation path.

    The installer lays out generations at
    ``$TABULA_HOME/distrib/<distro>/generations/<gen>``. We trust the
    generation directory as the active distro tree and lift the distro id from
    two levels up.
    """
    distro_dir = distro_dir.expanduser().resolve()
    parent = distro_dir.parent
    if parent.name == "generations":
        active = parent.parent.name
    else:
        active = parent.name
    return {"active": active, "dir": str(distro_dir)}


def _string_array(values: list[str]):
    array = toml_io.require_tomlkit().array()
    for value in values:
        array.append(value)
    return array


def _distro_dir(path: Path) -> Path:
    return path.expanduser().resolve()


def runtime_socket_path(home: Path) -> Path:
    override = os.environ.get("TABULA_RUNTIME_SOCKET_PATH", "").strip()
    if override:
        return Path(override).expanduser()
    default = home / "run" / "runtime.sock"
    if len(str(default)) <= 100:
        return default
    import hashlib
    from tempfile import gettempdir

    safe = "tabula-rt-" + hashlib.sha256(str(home).encode("utf-8")).hexdigest()[:16]
    return Path(gettempdir()) / safe / "runtime.sock"

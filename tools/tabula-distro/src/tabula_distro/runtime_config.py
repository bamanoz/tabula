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
        raise RuntimeConfigError(f"installed distro does not exist at {distro_dir}")
    plugin_dirs = [str(distro_dir / "plugins")]
    distro = distro_metadata(home, distro_dir)
    write_kernel_config(home, url=os.environ.get("TABULA_URL", "ws://localhost:8089/ws"))
    return write(home, plugin_dirs, distro=distro)


def sync_tenant(home: Path, tenant_id: str, tenant_dir: Path, *, distro_dir: Path | None = None) -> Path:
    """Add or replace one tenant runtime surface without changing other tenants."""
    path = home / "config" / "runtime.toml"
    doc = toml_io.load(path)
    tomlkit = toml_io.require_tomlkit()
    global_dirs = (
        [str(value) for value in doc.get("plugin_dirs") or []],
        [str(value) for value in doc.get("skill_dirs") or []],
    )
    configured_tenants: dict[str, tuple[list[str], list[str]]] = {}
    for item in doc.get("tenant") or []:
        if not hasattr(item, "get"):
            continue
        item_id = str(item.get("id") or "").strip()
        if not item_id:
            continue
        configured_tenants[item_id] = (
            [str(value) for value in item.get("plugin_dirs") or []],
            [str(value) for value in item.get("skill_dirs") or []],
        )
    doc["plugin_dirs"] = _string_array([])
    doc["skill_dirs"] = _string_array([])

    tenants: dict[str, tuple[list[str], list[str]]] = {}
    tenants_dir = home / "tenants"
    if tenants_dir.is_dir():
        for installed_dir in sorted(tenants_dir.iterdir()):
            if not installed_dir.is_dir() or installed_dir.name.startswith("."):
                continue
            plugin_dir = installed_dir / "plugins"
            skill_dir = installed_dir / "skills"
            if (installed_dir / "install.lock.json").is_file() or plugin_dir.is_dir() or skill_dir.is_dir():
                dirs = ([str(plugin_dir)], [str(skill_dir)])
            else:
                dirs = configured_tenants.get(installed_dir.name, global_dirs)
            tenants[installed_dir.name] = dirs
    tenants[tenant_id] = ([str(tenant_dir / "plugins")], [str(tenant_dir / "skills")])

    tenant_aot = tomlkit.aot()
    for item_id in sorted(tenants):
        plugin_dirs, skill_dirs = tenants[item_id]
        item = tomlkit.table()
        item["id"] = item_id
        item["plugin_dirs"] = _string_array(plugin_dirs)
        item["skill_dirs"] = _string_array(skill_dirs)
        tenant_aot.append(item)
    doc["tenant"] = tenant_aot

    kernels = tomlkit.aot()
    kernel = tomlkit.table()
    kernel["id"] = "main"
    kernel["url"] = "unix://" + str(runtime_socket_path(home))
    kernel["token_file"] = str(home / "run" / "runtime-token")
    kernel["tenants"] = _string_array(sorted(tenants))
    kernels.append(kernel)
    doc["kernel"] = kernels
    write_python_runtime(doc)
    toml_io.merge_defaults(doc, {"pool": {"cold_workers_per_tenant_max": 16}})
    if distro_dir is not None:
        distro = distro_metadata(home, distro_dir)
        distro_table = tomlkit.table()
        distro_table["active"] = distro["active"]
        distro_table["dir"] = distro["dir"]
        doc["distro"] = distro_table
    toml_io.dump(path, doc)
    write_kernel_config(home, url=os.environ.get("TABULA_URL", "ws://localhost:8089/ws"))
    return path


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
        # installer because only it knows which distro is active.
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
    """Resolve distro id and source directory from an installed distro path."""
    distro_dir = distro_dir.expanduser().resolve()
    return {"active": distro_dir.name, "dir": str(distro_dir)}


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

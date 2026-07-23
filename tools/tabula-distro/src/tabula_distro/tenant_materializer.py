"""Tenant installation and distro-owned materialization."""
from __future__ import annotations

import datetime as dt
import json
import os
import re
import shlex
import shutil
import subprocess
import sys
import tomllib
from dataclasses import dataclass
from pathlib import Path

from . import plugin_config
from . import config as cfg
from . import install as installmod
from . import runtime_config
from . import tenant_bindings
from . import toml_io


_TENANT_ID_RE = re.compile(r"^[a-z0-9][a-z0-9-]{0,62}$")
_RESERVED_TENANT_IDS = {"admin", "kernel", "runtime", "system"}


class TenantMaterializationError(RuntimeError):
    pass


@dataclass(frozen=True)
class TenantInstallResult:
    tenant_dir: Path
    distro: installmod.InstallResult
    materialized: bool
    compiled_plugins: tuple[str, ...]


def install(
    source: str | Path,
    home: Path,
    *,
    tenant_id: str,
    project_root: Path,
    values_path: Path | None = None,
    display_name: str = "",
    offline: bool = False,
    update: bool = False,
    keep_generations: int = 5,
    replace_binding: bool = False,
    bind_project: bool = True,
) -> TenantInstallResult:
    _validate_tenant_id(tenant_id)
    project_root = project_root.expanduser().resolve()
    if not project_root.is_dir():
        raise TenantMaterializationError(f"project root is not a directory: {project_root}")
    values = _load_values(values_path)

    tenant_dir = home / "tenants" / tenant_id
    if tenant_dir.exists() or tenant_dir.is_symlink():
        raise TenantMaterializationError(f"tenant already exists: {tenant_id}")
    registry = tenant_bindings.load(home)
    if bind_project:
        registry = tenant_bindings.bind_directory(
            registry, project_root, tenant_id, replace=replace_binding
        )
    tenant_dir.mkdir(parents=True)
    try:
        result = installmod.install(
            source,
            home,
            offline=offline,
            update=update,
            tenant=tenant_id,
            expose_global_boot=False,
            keep_generations=keep_generations,
        )
        _write_tenant_metadata(tenant_dir, tenant_id, display_name or tenant_id)
        _write_values(tenant_dir / "values.toml", values)
        materialized = _run_materializer(
            result.generation.path,
            result.lock.distro,
            home,
            tenant_id,
            project_root,
        )
        compiled = plugin_config.compile_plugin_configs(home, tenant_dir)
        runtime_config.sync_tenant(home, tenant_id, tenant_dir, distro_dir=result.generation.path)
        tenant_bindings.save(home, registry)
    except Exception:
        shutil.rmtree(tenant_dir, ignore_errors=True)
        raise
    installmod.touch_reload_trigger(home, tenant=tenant_id)
    return TenantInstallResult(
        tenant_dir=tenant_dir,
        distro=result,
        materialized=materialized,
        compiled_plugins=compiled,
    )


def refresh(
    source: str,
    home: Path,
    tenant_id: str,
    project_root: Path,
    values_path: Path | None,
    *,
    offline: bool,
    update: bool,
    keep_generations: int,
) -> TenantInstallResult:
    tenant_dir = home / "tenants" / tenant_id
    if not tenant_dir.is_dir():
        raise TenantMaterializationError(f"tenant does not exist: {tenant_id}")
    result = installmod.install(
        source,
        home,
        offline=offline,
        update=update,
        tenant=tenant_id,
        expose_global_boot=False,
        keep_generations=keep_generations,
    )
    values = _load_values(values_path) if values_path is not None else _load_values(tenant_dir / "values.toml")
    _write_values(tenant_dir / "values.toml", values)
    materialized = _run_materializer(
        result.generation.path,
        result.lock.distro,
        home,
        tenant_id,
        project_root.expanduser().resolve(),
    )
    compiled = plugin_config.compile_plugin_configs(home, tenant_dir)
    runtime_config.sync_tenant(home, tenant_id, tenant_dir, distro_dir=result.generation.path)
    installmod.touch_reload_trigger(home, tenant=tenant_id)
    return TenantInstallResult(
        tenant_dir=tenant_dir,
        distro=result,
        materialized=materialized,
        compiled_plugins=compiled,
    )


def rematerialize(
    home: Path,
    tenant_id: str,
    project_root: Path,
    values_path: Path,
) -> None:
    tenant_dir = home / "tenants" / tenant_id
    lock_path = tenant_dir / "install.lock.json"
    try:
        payload = json.loads(lock_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise TenantMaterializationError(f"read tenant install lock {lock_path}: {exc}") from exc
    generation = payload.get("generation")
    distro = payload.get("distro")
    if not isinstance(generation, dict) or not isinstance(distro, dict):
        raise TenantMaterializationError(f"tenant install lock is incomplete: {lock_path}")
    generation_path = generation.get("path")
    distro_name = distro.get("distro")
    if not isinstance(generation_path, str) or not isinstance(distro_name, str):
        raise TenantMaterializationError(f"tenant install lock is incomplete: {lock_path}")
    generation_root = Path(generation_path)
    if not generation_root.is_absolute():
        generation_root = home / generation_root
    values = _load_values(values_path)
    _write_values(tenant_dir / "values.toml", values)
    _run_materializer(
        generation_root, distro_name, home, tenant_id, project_root.expanduser().resolve()
    )
    plugin_config.compile_plugin_configs(home, tenant_dir)
    runtime_config.sync_tenant(home, tenant_id, tenant_dir, distro_dir=generation_root)
    installmod.touch_reload_trigger(home, tenant=tenant_id)


def _validate_tenant_id(tenant_id: str) -> None:
    if not _TENANT_ID_RE.fullmatch(tenant_id) or tenant_id in _RESERVED_TENANT_IDS:
        raise TenantMaterializationError(
            f"invalid tenant id {tenant_id!r}; expected lowercase letters, digits, and hyphens, max 63 characters"
        )


def _load_values(path: Path | None) -> dict[str, object]:
    if path is None:
        return {}
    path = path.expanduser().resolve()
    if not path.is_file():
        raise TenantMaterializationError(f"values file does not exist: {path}")
    try:
        with path.open("rb") as handle:
            data = tomllib.load(handle)
    except tomllib.TOMLDecodeError as exc:
        raise TenantMaterializationError(f"invalid values file {path}: {exc}") from exc
    return data


def _write_tenant_metadata(tenant_dir: Path, tenant_id: str, display_name: str) -> None:
    path = tenant_dir / "tenant.toml"
    if path.is_file():
        return
    doc = toml_io.require_tomlkit().document()
    tenant = toml_io.require_tomlkit().table()
    tenant["id"] = tenant_id
    tenant["display_name"] = display_name
    tenant["created_at"] = dt.datetime.now(dt.timezone.utc).isoformat().replace("+00:00", "Z")
    doc["tenant"] = tenant
    toml_io.dump(path, doc)


def _write_values(path: Path, values: dict[str, object]) -> None:
    doc = toml_io.require_tomlkit().document()
    for key, value in values.items():
        doc[key] = toml_io.to_tomlkit(value)
    toml_io.dump(path, doc)


def _run_materializer(
    generation_path: Path,
    distro_name: str,
    home: Path,
    tenant_id: str,
    project_root: Path,
) -> bool:
    distro = cfg.load(generation_path, override_name=distro_name)
    _validate_contract_files(distro)
    raw = distro.tenant_materializer
    if raw is None:
        return False
    command = shlex.split(raw)
    if not command:
        raise TenantMaterializationError("tenant materializer command is empty")
    command = _normalize_python_command(command)
    tenant_dir = home / "tenants" / tenant_id
    _reset_materializer_output(tenant_dir)
    env = os.environ.copy()
    env.update({
        "TABULA_HOME": str(home),
        "TABULA_TENANT_ID": tenant_id,
        "TABULA_TENANT_DIR": str(tenant_dir),
        "TABULA_TENANT_DISTRO_DIR": str(generation_path),
        "TABULA_TENANT_PROJECT_ROOT": str(project_root),
        "TABULA_TENANT_VALUES": str(tenant_dir / "values.toml"),
        "TABULA_TENANT_INSTALL_LOCK": str(tenant_dir / "install.lock.json"),
    })
    _prepend_pythonpath(env, tenant_dir / "packages" / "python" / "src")
    process = subprocess.run(
        command,
        cwd=generation_path,
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    if process.returncode != 0:
        detail = (process.stderr or process.stdout or "").strip()
        raise TenantMaterializationError(
            f"tenant materializer failed ({raw!r})" + (f": {detail}" if detail else "")
        )
    return True


def _validate_contract_files(distro: cfg.DistroConfig) -> None:
    for field, relative in (
        ("values_schema", distro.tenant_values_schema),
        ("values_defaults", distro.tenant_values_defaults),
    ):
        if relative is None:
            continue
        path = Path(relative)
        if path.is_absolute() or ".." in path.parts or not (distro.path / path).is_file():
            raise TenantMaterializationError(
                f"tenant_contract.{field} must name a file inside the distro generation: {relative}"
            )


def _prepend_pythonpath(env: dict[str, str], root: Path) -> None:
    if not root.is_dir():
        return
    current = env.get("PYTHONPATH", "")
    env["PYTHONPATH"] = str(root) + (os.pathsep + current if current else "")


def _normalize_python_command(command: list[str]) -> list[str]:
    if os.name != "nt" or not command:
        return command
    launcher = Path(command[0]).name.lower()
    if launcher in {"python", "python.exe", "python3", "python3.exe"}:
        return [sys.executable, *command[1:]]
    if launcher in {"py", "py.exe"}:
        rest = list(command[1:])
        if rest and re.fullmatch(r"-\d+(\.\d+)?", rest[0]):
            rest = rest[1:]
        return [sys.executable, *rest]
    return command


def _reset_materializer_output(tenant_dir: Path) -> None:
    config_dir = tenant_dir / "config"
    if config_dir.is_dir() and not config_dir.is_symlink():
        shutil.rmtree(config_dir)
        return
    if config_dir.exists() or config_dir.is_symlink():
        config_dir.unlink(missing_ok=True)

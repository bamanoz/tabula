"""Runnable agent application manifest parsing and lock support."""
from __future__ import annotations

import datetime as _dt
import json
import os
import re
import shlex
import shutil
import subprocess
import sys
import tomllib
from dataclasses import dataclass, field, replace
from pathlib import Path
from typing import Any

from . import config as cfg
from . import lock as distrolock
from . import sources as srcmod
from . import toml_io
from .cache import GitCache


APP_LOCK_VERSION = 1
_APP_ID_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$")


class AppManifestError(ValueError):
    pass


@dataclass(frozen=True)
class Application:
    id: str
    name: str = ""


@dataclass(frozen=True)
class DistroRef:
    source: str


@dataclass(frozen=True)
class KernelTopology:
    id: str
    mode: str
    url: str


@dataclass(frozen=True)
class RuntimeTopology:
    id: str
    mode: str
    tenants: tuple[str, ...]
    exec: dict[str, Any] = field(default_factory=dict)


@dataclass(frozen=True)
class DirectoryBinding:
    root: str
    app: str
    kernel: str


@dataclass(frozen=True)
class DefaultBinding:
    app: str
    kernel: str


@dataclass(frozen=True)
class Bindings:
    directories: tuple[DirectoryBinding, ...] = ()
    default: DefaultBinding | None = None


@dataclass(frozen=True)
class AppManifest:
    path: Path
    application: Application
    distro: DistroRef
    kernel: KernelTopology
    runtimes: tuple[RuntimeTopology, ...]
    bindings: Bindings
    values: dict[str, Any] = field(default_factory=dict)


@dataclass(frozen=True)
class AppLock:
    application_id: str
    application_name: str
    manifest_path: str
    distro_id: str
    distro_name: str
    distro_source: str
    distro_lock: dict[str, Any]
    kernel: dict[str, Any]
    runtimes: tuple[dict[str, Any], ...]
    bindings: dict[str, Any]
    generated_at: str | None = None

    def to_json(self) -> dict[str, Any]:
        return {
            "version": APP_LOCK_VERSION,
            "generated_at": self.generated_at or now_iso(),
            "application": {
                "id": self.application_id,
                "name": self.application_name,
            },
            "manifest_path": self.manifest_path,
            "distro": {
                "id": self.distro_id,
                "name": self.distro_name,
                "source": self.distro_source,
                "lock": self.distro_lock,
            },
            "kernel": self.kernel,
            "runtimes": list(self.runtimes),
            "bindings": self.bindings,
        }

    @classmethod
    def from_json(cls, data: dict[str, Any]) -> "AppLock":
        version = data.get("version")
        if version != APP_LOCK_VERSION:
            raise AppManifestError(f"unsupported app lock version: {version}")
        app = _section(data, "application")
        distro = _section(data, "distro")
        distro_lock = _section(distro, "lock")
        return cls(
            application_id=_string(app, "id", "application"),
            application_name=str(app.get("name") or ""),
            manifest_path=str(data.get("manifest_path") or ""),
            distro_id=str(distro.get("id") or "").strip(),
            distro_name=_string(distro, "name", "distro"),
            distro_source=_string(distro, "source", "distro"),
            distro_lock=distro_lock,
            kernel=_section(data, "kernel"),
            runtimes=tuple(item for item in data.get("runtimes", []) if isinstance(item, dict)),
            bindings=_section(data, "bindings"),
            generated_at=data.get("generated_at"),
        )


def now_iso() -> str:
    return _dt.datetime.now(_dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def load(path: Path, *, tabula_home: Path | None = None) -> AppManifest:
    path = path.expanduser().resolve()
    try:
        with path.open("rb") as f:
            data = tomllib.load(f)
    except OSError as exc:
        raise AppManifestError(f"read app manifest {path}: {exc}") from exc
    data = _expand_manifest_data(data, path=path, tabula_home=tabula_home)
    return parse(data, path=path)


def parse(data: dict[str, Any], *, path: Path) -> AppManifest:
    application_raw = _section(data, "application")
    app_id = _string(application_raw, "id", "application")
    _validate_id(app_id)
    application = Application(id=app_id, name=str(application_raw.get("name") or ""))

    distro_raw = _section(data, "distro")
    _validate_distro_ref(distro_raw)
    distro = DistroRef(source=_string(distro_raw, "source", "distro"))

    kernel_raw = _optional_section(data, "kernel")
    kernel = KernelTopology(
        id=_optional_string(kernel_raw, "id") or app_id,
        mode=_choice(kernel_raw, "mode", "kernel", {"managed", "external"}, default="managed"),
        url=_optional_string(kernel_raw, "url") or "ws://127.0.0.1:8089/ws",
    )

    runtimes_raw = data.get("runtimes")
    if runtimes_raw is None:
        runtimes = (RuntimeTopology(id="local", mode="managed", tenants=(app_id,), exec={"backend": "bare"}),)
    elif not isinstance(runtimes_raw, list) or not runtimes_raw:
        raise AppManifestError("runtimes must be a non-empty array of tables")
    else:
        runtimes = tuple(_parse_runtime(item, index, app_id=app_id) for index, item in enumerate(runtimes_raw))

    bindings = _parse_bindings(data.get("bindings"), app_id=app_id, kernel_id=kernel.id, project_root=str(_project_root(path)))
    values_raw = data.get("values")
    values = _clone_toml(values_raw) if isinstance(values_raw, dict) else {}

    return AppManifest(
        path=path,
        application=application,
        distro=distro,
        kernel=kernel,
        runtimes=runtimes,
        bindings=bindings,
        values=values,
    )


def create_lock(manifest: AppManifest, home: Path, *, offline: bool = False, update: bool = False) -> AppLock:
    # `update` is accepted for CLI shape parity; distro resolution currently
    # follows the app manifest source exactly and does not consult a prior app lock.
    _ = update
    distro_path, distro_source_uri = _resolve_distro_source(manifest.distro.source, manifest.path.parent, home, offline=offline)
    distro_cfg = cfg.load(distro_path)
    distro_lock = distrolock.Lock(
        distro=distro_cfg.name,
        distro_source=distro_source_uri or manifest.distro.source,
        distro_version=str(distro_cfg.version) if distro_cfg.version is not None else None,
    )
    return AppLock(
        application_id=manifest.application.id,
        application_name=manifest.application.name,
        manifest_path=str(manifest.path),
        distro_id=distro_cfg.id,
        distro_name=distro_cfg.name,
        distro_source=manifest.distro.source,
        distro_lock=distro_lock.to_json(),
        kernel={"id": manifest.kernel.id, "mode": manifest.kernel.mode, "url": manifest.kernel.url},
        runtimes=tuple(
            {"id": runtime.id, "mode": runtime.mode, "tenants": list(runtime.tenants), "exec": _clone_toml(runtime.exec)}
            for runtime in manifest.runtimes
        ),
        bindings=_bindings_json(manifest.bindings),
    )


def resolve_distro_path(manifest: AppManifest, home: Path, *, offline: bool = False) -> Path:
    path, _source = _resolve_distro_source(manifest.distro.source, manifest.path.parent, home, offline=offline)
    return path


def materializer_declared(manifest: AppManifest, home: Path, *, offline: bool = False) -> bool:
    contract = _application_contract(resolve_distro_path(manifest, home, offline=offline))
    raw = contract.get("materializer")
    return isinstance(raw, str) and bool(raw.strip())


def save_lock(path: Path, lock: AppLock) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    payload = json.dumps(lock.to_json(), indent=2, sort_keys=True) + "\n"
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_text(payload, encoding="utf-8")
    tmp.replace(path)


def load_lock(path: Path) -> AppLock | None:
    if not path.is_file():
        return None
    return AppLock.from_json(json.loads(path.read_text(encoding="utf-8")))


def default_lock_path(manifest_path: Path) -> Path:
    if manifest_path.name.endswith(".toml"):
        return manifest_path.with_suffix(".lock")
    return manifest_path.with_name(manifest_path.name + ".lock")


def with_bindings(manifest: AppManifest, bindings: Bindings) -> AppManifest:
    return replace(manifest, bindings=bindings)


def with_lock_bindings(lock: AppLock, bindings: Bindings) -> AppLock:
    return replace(lock, bindings=_bindings_json(bindings))


def materialize_metadata(manifest: AppManifest, lock: AppLock, home: Path) -> Path:
    _migrate_global_driver_config(home / "config" / "global.toml")
    tenant_dir = home / "tenants" / manifest.application.id
    tenant_dir.mkdir(parents=True, exist_ok=True)
    tenant_file = tenant_dir / "tenant.toml"
    if not tenant_file.is_file():
        tenant_file.write_text(
            "[tenant]\n"
            f"id = {json.dumps(manifest.application.id)}\n"
            f"display_name = {json.dumps(manifest.application.name or manifest.application.id)}\n"
            f"created_at = {json.dumps(now_iso())}\n",
            encoding="utf-8",
        )
    save_lock(tenant_dir / "app.lock.json", lock)
    (tenant_dir / "app.toml").write_text(manifest.path.read_text(encoding="utf-8"), encoding="utf-8")
    values_payload = _tomlish(manifest.values)
    (tenant_dir / "values.toml").write_text(values_payload, encoding="utf-8")
    return tenant_dir


def run_materializer(manifest: AppManifest, home: Path, *, lock_path: Path | None = None, phase: str = "apply", dry_run: bool = False) -> bool:
    distro_path, _source = _resolve_distro_source(manifest.distro.source, manifest.path.parent, home, offline=False)
    contract = _application_contract(distro_path)
    tenant_dir = home / "tenants" / manifest.application.id
    _reset_materializer_output(tenant_dir)
    raw = contract.get("materializer")
    if not isinstance(raw, str) or not raw.strip():
        return False
    cmd = shlex.split(raw)
    if not cmd:
        return False
    cmd = _normalize_materializer_command(cmd)
    env = os.environ.copy()
    env.update({
        "TABULA_HOME": str(home),
        "TABULA_APP_ID": manifest.application.id,
        "TABULA_TENANT_ID": manifest.application.id,
        "TABULA_TENANT_DIR": str(tenant_dir),
        "TABULA_APP_MANIFEST": str(manifest.path),
        "TABULA_APP_LOCK": str(lock_path or default_lock_path(manifest.path)),
        "TABULA_APP_LOCK_JSON": str(tenant_dir / "app.lock.json"),
        "TABULA_APP_VALUES": str(tenant_dir / "values.toml"),
        "TABULA_APP_PHASE": phase,
        "TABULA_APP_DRY_RUN": "1" if dry_run else "0",
    })
    _prepend_pythonpath(env, _materializer_python_roots(home))
    proc = subprocess.run(cmd, cwd=distro_path, env=env, capture_output=True, text=True)
    if proc.returncode != 0:
        detail = (proc.stderr or proc.stdout or "").strip()
        raise AppManifestError(f"app materializer failed ({raw!r})" + (f": {detail}" if detail else ""))
    return True


def _normalize_materializer_command(cmd: list[str]) -> list[str]:
    if os.name != "nt" or not cmd:
        return cmd
    launcher = Path(cmd[0]).name.lower()
    if launcher in {"python", "python.exe", "python3", "python3.exe"}:
        return [sys.executable, *cmd[1:]]
    if launcher in {"py", "py.exe"}:
        rest = list(cmd[1:])
        if rest and re.fullmatch(r"-\d+(\.\d+)?", rest[0]):
            rest = rest[1:]
        return [sys.executable, *rest]
    return cmd


def _materializer_python_roots(home: Path) -> list[Path]:
    roots = [home / "packages" / "python" / "src"]
    bundle_root = _local_bundles_root()
    if bundle_root is not None:
        roots.extend([
            bundle_root / "base" / "plugin-sdk" / "sdk" / "python" / "src",
            bundle_root / "base" / "skills" / "sdk" / "python" / "src",
            bundle_root / "base" / "tool-result-store" / "sdk" / "python" / "src",
            bundle_root / "base" / "sessions" / "sdk" / "python" / "src",
            bundle_root / "base" / "deferred-tools" / "sdk" / "python" / "src",
            bundle_root / "drivers" / "driver" / "sdk" / "python" / "src",
            bundle_root / "mempalace" / "mempalace-common" / "sdk" / "python" / "src",
        ])
    return [path for path in roots if path.is_dir()]


def _local_bundles_root() -> Path | None:
    direct = os.environ.get("TABULA_BUNDLES_ROOT")
    if direct:
        path = Path(direct).expanduser().resolve()
        if path.is_dir():
            return path
    alias = os.environ.get("TABULA_SOURCE_ALIAS_TABULA_BUNDLES", "")
    if alias.startswith("local:"):
        raw = alias.removeprefix("local:").split("#", 1)[0]
        path = Path(raw).expanduser().resolve()
        if path.is_dir():
            return path
    return None


def _prepend_pythonpath(env: dict[str, str], roots: list[Path]) -> None:
    if not roots:
        return
    parts = [str(path) for path in roots]
    current = env.get("PYTHONPATH")
    if current:
        parts.append(current)
    env["PYTHONPATH"] = os.pathsep.join(parts)


def compile_plugin_configs(home: Path, tenant_dir: Path) -> tuple[str, ...]:
    migrate_driver_plugin_config(home, tenant_dir)
    plugin_root = tenant_dir / "config" / "plugins"
    if not plugin_root.is_dir():
        return ()
    compiled: list[str] = []
    for plugin_dir in sorted(entry for entry in plugin_root.iterdir() if entry.is_dir()):
        defaults_path = plugin_dir / "defaults.toml"
        if not defaults_path.is_file():
            continue
        schema = _plugin_config_schema(home, plugin_dir.name)
        merged = _merge_component_toml(
            _read_toml(defaults_path),
            _read_toml(home / "config" / "plugins" / plugin_dir.name / "config.toml"),
            schema,
        )
        config_path = plugin_dir / "config.toml"
        if merged:
            config_path.write_text(_tomlish(merged), encoding="utf-8")
            compiled.append(plugin_dir.name)
            continue
        config_path.unlink(missing_ok=True)
    return tuple(compiled)


def migrate_driver_plugin_config(home: Path, tenant_dir: Path) -> None:
    """Move the driver config from the removed app/client surface to plugin config."""

    _migrate_global_driver_config(home / "config" / "global.toml")
    _migrate_tenant_driver_config(tenant_dir)


def _migrate_global_driver_config(path: Path) -> None:
    if not path.is_file():
        return
    doc = toml_io.load(path)
    clients = doc.get("clients")
    if not isinstance(clients, dict):
        return
    driver = clients.get("driver")
    if not isinstance(driver, dict):
        return
    plugins = doc.get("plugins")
    if not isinstance(plugins, dict):
        plugins = toml_io.require_tomlkit().table()
        doc["plugins"] = plugins
    existing = plugins.get("driver")
    if isinstance(existing, dict):
        merged = _merge_toml(_plain_toml(driver), _plain_toml(existing))
        plugins["driver"] = toml_io.to_tomlkit(merged)
    else:
        plugins["driver"] = driver
    del clients["driver"]
    if not clients:
        del doc["clients"]
    toml_io.dump(path, doc)


def _migrate_tenant_driver_config(tenant_dir: Path) -> None:
    old_dir = tenant_dir / "config" / "apps" / "driver"
    if not old_dir.is_dir():
        return
    new_dir = tenant_dir / "config" / "plugins" / "driver"
    for filename in ("defaults.toml", "config.toml"):
        old_path = old_dir / filename
        if not old_path.is_file():
            continue
        new_path = new_dir / filename
        if new_path.is_file():
            merged = _merge_toml(_read_toml(old_path), _read_toml(new_path))
            new_path.write_text(_tomlish(merged), encoding="utf-8")
            old_path.unlink()
            continue
        new_path.parent.mkdir(parents=True, exist_ok=True)
        old_path.replace(new_path)
    for path in (old_dir, old_dir.parent):
        try:
            path.rmdir()
        except OSError:
            pass


def _plain_toml(value: Any) -> Any:
    if isinstance(value, dict):
        return {str(key): _plain_toml(item) for key, item in value.items()}
    if isinstance(value, list):
        return [_plain_toml(item) for item in value]
    if hasattr(value, "unwrap"):
        return value.unwrap()
    return value


def _reset_materializer_output(tenant_dir: Path) -> None:
    config_dir = tenant_dir / "config"
    if config_dir.is_dir() and not config_dir.is_symlink():
        shutil.rmtree(config_dir)
        return
    if config_dir.exists() or config_dir.is_symlink():
        config_dir.unlink(missing_ok=True)


def _application_contract(distro_path: Path) -> dict[str, Any]:
    path = distro_path / "distro.toml"
    if not path.is_file():
        return {}
    with path.open("rb") as f:
        data = tomllib.load(f)
    contract = data.get("application_contract")
    return contract if isinstance(contract, dict) else {}


def _read_toml(path: Path) -> dict[str, Any]:
    if not path.is_file():
        return {}
    with path.open("rb") as f:
        data = tomllib.load(f)
    return data if isinstance(data, dict) else {}


def _merge_toml(base: dict[str, Any], overlay: dict[str, Any]) -> dict[str, Any]:
    merged = _clone_toml(base)
    for key, value in overlay.items():
        existing = merged.get(key)
        if isinstance(existing, dict) and isinstance(value, dict):
            merged[str(key)] = _merge_toml(existing, value)
            continue
        merged[str(key)] = _clone_toml(value)
    return merged


def _merge_component_toml(base: dict[str, Any], overlay: dict[str, Any], schema: dict[str, str]) -> dict[str, Any]:
    merged = _clone_toml(base)
    replace_keys = _replace_keys(overlay.get("__replace"))
    for key, value in overlay.items():
        if key == "__replace":
            continue
        existing = merged.get(key)
        if key not in replace_keys and schema.get(str(key)) in {"string_list", "int_list"} and isinstance(existing, list) and isinstance(value, list):
            merged[str(key)] = _prepend_unique(value, existing)
            continue
        if key not in replace_keys and isinstance(existing, dict) and isinstance(value, dict):
            merged[str(key)] = _merge_toml(existing, value)
            continue
        merged[str(key)] = _clone_toml(value)
    return merged


def _plugin_config_schema(home: Path, plugin_id: str) -> dict[str, str]:
    data = _read_toml(home / "plugins" / plugin_id / "plugin.schema.toml")
    entries: dict[str, str] = {}
    for key, value in data.items():
        if not isinstance(key, str) or not key.startswith("entry.") or not isinstance(value, dict):
            continue
        field = key.split(".", 1)[1]
        field_type = value.get("type")
        if isinstance(field_type, str):
            entries[field] = field_type
    entry = data.get("entry")
    if isinstance(entry, dict):
        for field, value in entry.items():
            if isinstance(field, str) and isinstance(value, dict) and isinstance(value.get("type"), str):
                entries[field] = value["type"]
    return entries


def _replace_keys(value: Any) -> set[str]:
    if isinstance(value, str):
        return {value} if value else set()
    if isinstance(value, list):
        return {item for item in value if isinstance(item, str) and item}
    return set()


def _prepend_unique(incoming: list[Any], existing: list[Any]) -> list[Any]:
    result = [_clone_toml(item) for item in incoming]
    for item in existing:
        if item not in result:
            result.append(_clone_toml(item))
    return result


def _resolve_distro_source(value: str, base_dir: Path, home: Path, *, offline: bool) -> tuple[Path, str]:
    if value.startswith("local:") or value.startswith("git+"):
        src = srcmod.parse(value, base_dir=base_dir)
        if isinstance(src, srcmod.LocalSource):
            return src.path, value
        cache = GitCache(home / "cache")
        checkout = cache.fetch(src, offline=offline)
        root = checkout.worktree if not src.subpath else checkout.worktree / src.subpath
        if not root.is_dir():
            raise AppManifestError(f"git source subpath not found: {src.subpath} in {src.url}")
        return root, value
    path = Path(value).expanduser()
    if not path.is_absolute():
        path = base_dir / path
    return path.resolve(), value


def _parse_runtime(item: Any, index: int, *, app_id: str) -> RuntimeTopology:
    if not isinstance(item, dict):
        raise AppManifestError(f"runtimes[{index}] must be a table")
    runtime_id = _optional_string(item, "id") or ("local" if index == 0 else "")
    if not runtime_id:
        raise AppManifestError(f"runtimes[{index}].id is required")
    tenants_raw = item.get("tenants")
    if tenants_raw is None:
        tenants_raw = [app_id]
    if not isinstance(tenants_raw, list) or not tenants_raw or not all(isinstance(value, str) and value.strip() for value in tenants_raw):
        raise AppManifestError(f"runtimes[{index}].tenants must be a non-empty list of strings")
    exec_raw = item.get("exec")
    exec_cfg = _clone_toml(exec_raw) if isinstance(exec_raw, dict) else {}
    exec_cfg.setdefault("backend", "bare")
    return RuntimeTopology(
        id=runtime_id,
        mode=_choice(item, "mode", f"runtimes[{index}]", {"managed", "external"}, default="managed"),
        tenants=tuple(str(value).strip() for value in tenants_raw),
        exec=exec_cfg,
    )


def _parse_bindings(raw: Any, *, app_id: str, kernel_id: str, project_root: str) -> Bindings:
    if raw is None:
        return Bindings(directories=(DirectoryBinding(root=project_root, app=app_id, kernel=kernel_id),))
    if not isinstance(raw, dict):
        raise AppManifestError("bindings must be a table")
    data = raw
    default = None
    if "default" in data:
        default_raw = data["default"]
        if not isinstance(default_raw, dict):
            raise AppManifestError("bindings.default must be a table")
        default = DefaultBinding(
            app=_optional_string(default_raw, "app") or app_id,
            kernel=_optional_string(default_raw, "kernel") or kernel_id,
        )
    directories_raw = data.get("directory", [])
    if directories_raw is None:
        directories_raw = []
    if not isinstance(directories_raw, list):
        raise AppManifestError("bindings.directory must be an array of tables")
    directories = []
    for index, item in enumerate(directories_raw):
        if not isinstance(item, dict):
            raise AppManifestError(f"bindings.directory[{index}] must be a table")
        directories.append(DirectoryBinding(
            root=_string(item, "root", f"bindings.directory[{index}]"),
            app=_optional_string(item, "app") or app_id,
            kernel=_optional_string(item, "kernel") or kernel_id,
        ))
    return Bindings(directories=tuple(directories), default=default)


def _expand_manifest_data(data: dict[str, Any], *, path: Path, tabula_home: Path | None) -> dict[str, Any]:
    application_raw = data.get("application")
    if not isinstance(application_raw, dict):
        raise AppManifestError("application section is required")
    app_id = application_raw.get("id")
    if not isinstance(app_id, str) or not app_id.strip():
        raise AppManifestError("application.id is required")
    project_root = _project_root(path)
    local = _read_local(project_root / ".tabula" / "local.toml")
    variables: dict[str, str] = {
        "project_root": str(project_root),
        "application_id": app_id.strip(),
        "tabula_home": str(tabula_home) if tabula_home is not None else "",
    }
    return _expand_value(data, variables=variables, local=local)


def _project_root(path: Path) -> Path:
    if path.parent.name == ".tabula":
        return path.parent.parent
    return path.parent


def _read_local(path: Path) -> dict[str, Any]:
    if not path.is_file():
        return {}
    try:
        with path.open("rb") as f:
            return tomllib.load(f)
    except OSError as exc:
        raise AppManifestError(f"read local overrides {path}: {exc}") from exc


def _expand_value(value: Any, *, variables: dict[str, str], local: dict[str, Any]) -> Any:
    if isinstance(value, str):
        return _expand_string(value, variables=variables, local=local)
    if isinstance(value, list):
        return [_expand_value(item, variables=variables, local=local) for item in value]
    if isinstance(value, dict):
        return {str(key): _expand_value(val, variables=variables, local=local) for key, val in value.items()}
    return value


_VAR_RE = re.compile(r"\$\{([^}]+)\}")


def _expand_string(value: str, *, variables: dict[str, str], local: dict[str, Any]) -> str:
    def replace(match: re.Match[str]) -> str:
        key = match.group(1).strip()
        if key in variables:
            return variables[key]
        if key.startswith("local."):
            local_value = _lookup_local(local, key[len("local."):])
            if local_value is None:
                raise AppManifestError(f"missing local override for ${{{key}}}")
            return str(local_value)
        raise AppManifestError(f"unknown app manifest variable ${{{key}}}")

    return _VAR_RE.sub(replace, value)


def _lookup_local(local: dict[str, Any], dotted: str) -> Any:
    current: Any = local
    for part in dotted.split("."):
        if not isinstance(current, dict) or part not in current:
            return None
        current = current[part]
    if isinstance(current, dict | list):
        raise AppManifestError(f"local override local.{dotted} must be a scalar")
    return current


def _bindings_json(bindings: Bindings) -> dict[str, Any]:
    out: dict[str, Any] = {}
    if bindings.default is not None:
        out["default"] = {"app": bindings.default.app, "kernel": bindings.default.kernel}
    if bindings.directories:
        out["directory"] = [
            {"root": binding.root, "app": binding.app, "kernel": binding.kernel}
            for binding in bindings.directories
        ]
    return out


def _section(data: dict[str, Any], key: str) -> dict[str, Any]:
    value = data.get(key)
    if not isinstance(value, dict):
        raise AppManifestError(f"{key} section is required")
    return value


def _optional_section(data: dict[str, Any], key: str) -> dict[str, Any]:
    value = data.get(key)
    if value is None:
        return {}
    if not isinstance(value, dict):
        raise AppManifestError(f"{key} section must be a table")
    return value


def _string(data: dict[str, Any], key: str, section: str) -> str:
    value = data.get(key)
    if not isinstance(value, str) or not value.strip():
        raise AppManifestError(f"{section}.{key} is required")
    return value.strip()


def _optional_string(data: dict[str, Any], key: str) -> str | None:
    value = data.get(key)
    if value is None:
        return None
    if not isinstance(value, str) or not value.strip():
        return None
    return value.strip()


def _choice(data: dict[str, Any], key: str, section: str, choices: set[str], *, default: str | None = None) -> str:
    value = _optional_string(data, key) or default
    if value is None:
        raise AppManifestError(f"{section}.{key} is required")
    if value not in choices:
        allowed = ", ".join(sorted(choices))
        raise AppManifestError(f"{section}.{key} must be one of: {allowed}")
    return value


def _validate_id(value: str) -> None:
    if not _APP_ID_RE.fullmatch(value):
        raise AppManifestError(f"application.id is invalid: {value!r}")


def _validate_distro_ref(data: dict[str, Any]) -> None:
    unsupported = [key for key in ("id", "name") if key in data]
    if unsupported:
        joined = ", ".join(f"distro.{key}" for key in unsupported)
        raise AppManifestError(f"{joined} is not supported; app manifests use distro.source as the single distro selector")


def _clone_toml(value: Any) -> Any:
    if isinstance(value, dict):
        return {str(key): _clone_toml(val) for key, val in value.items()}
    if isinstance(value, list):
        return [_clone_toml(item) for item in value]
    return value


def _tomlish(data: dict[str, Any]) -> str:
    lines: list[str] = []

    def write_table(prefix: str, value: dict[str, Any]) -> None:
        scalars = {key: val for key, val in value.items() if not isinstance(val, dict)}
        children = {key: val for key, val in value.items() if isinstance(val, dict)}
        if prefix:
            lines.append(f"[{prefix}]")
        for key, val in sorted(scalars.items()):
            lines.append(f"{key} = {_toml_value(val)}")
        if scalars and children:
            lines.append("")
        for key, child in sorted(children.items()):
            write_table(f"{prefix}.{key}" if prefix else key, child)

    write_table("", data)
    return "\n".join(lines).rstrip() + "\n"


def _toml_value(value: Any) -> str:
    if isinstance(value, str):
        return json.dumps(value)
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, int | float):
        return str(value)
    if isinstance(value, list):
        return "[" + ", ".join(_toml_value(item) for item in value) + "]"
    if isinstance(value, dict):
        return "{ " + ", ".join(f"{key} = {_toml_value(val)}" for key, val in sorted(value.items())) + " }"
    if value is None:
        return '""'
    return json.dumps(str(value))

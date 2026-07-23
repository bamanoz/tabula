"""Host-local directory and default bindings to installed tenants."""
from __future__ import annotations

import json
import tomllib
from dataclasses import dataclass
from pathlib import Path
from typing import Any


class BindingError(ValueError):
    pass


@dataclass(frozen=True)
class DirectoryBinding:
    root: str
    tenant: str


@dataclass(frozen=True)
class DefaultBinding:
    tenant: str


@dataclass(frozen=True)
class Registry:
    directories: tuple[DirectoryBinding, ...] = ()
    default: DefaultBinding | None = None

    def to_json(self) -> dict[str, Any]:
        out: dict[str, Any] = {
            "directory": [
                {"root": binding.root, "tenant": binding.tenant}
                for binding in self.directories
            ]
        }
        if self.default is not None:
            out["default"] = {"tenant": self.default.tenant}
        return out


def registry_path(home: Path) -> Path:
    return home / "bindings.toml"


def load(home: Path) -> Registry:
    path = registry_path(home)
    if not path.is_file():
        return Registry()
    try:
        with path.open("rb") as handle:
            data = tomllib.load(handle)
    except (OSError, tomllib.TOMLDecodeError) as exc:
        raise BindingError(f"read tenant bindings {path}: {exc}") from exc
    return parse(data)


def parse(data: dict[str, Any]) -> Registry:
    default = None
    raw_default = data.get("default")
    if raw_default is not None:
        if not isinstance(raw_default, dict):
            raise BindingError("default binding must be a table")
        default = DefaultBinding(tenant=_string(raw_default, "tenant", "default"))

    raw_directories = data.get("directory", [])
    if not isinstance(raw_directories, list):
        raise BindingError("directory bindings must be an array of tables")
    directories: list[DirectoryBinding] = []
    for index, item in enumerate(raw_directories):
        if not isinstance(item, dict):
            raise BindingError(f"directory binding {index} must be a table")
        directories.append(DirectoryBinding(
            root=_clean_root(_string(item, "root", f"directory[{index}]")),
            tenant=_string(item, "tenant", f"directory[{index}]"),
        ))
    return Registry(directories=tuple(_dedupe(directories)), default=default)


def save(home: Path, registry: Registry) -> None:
    path = registry_path(home)
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(".toml.tmp")
    tmp.write_text(_format(registry), encoding="utf-8")
    tmp.replace(path)


def bind_directory(registry: Registry, root: str | Path, tenant: str, *, replace: bool = False) -> Registry:
    binding = DirectoryBinding(root=_clean_root(str(root)), tenant=_clean(tenant, "tenant"))
    existing = next((item for item in registry.directories if item.root == binding.root), None)
    if existing is not None:
        if existing.tenant == binding.tenant:
            return registry
        if not replace:
            raise BindingError(
                f"directory {binding.root} is bound to tenant {existing.tenant!r}; pass --replace-binding to replace it"
            )
    kept = [item for item in registry.directories if item.root != binding.root]
    kept.append(binding)
    return Registry(directories=tuple(sorted(kept, key=lambda item: item.root)), default=registry.default)


def bind_default(registry: Registry, tenant: str, *, replace: bool = False) -> Registry:
    tenant = _clean(tenant, "tenant")
    if registry.default is not None and registry.default.tenant != tenant and not replace:
        raise BindingError(
            f"default is bound to tenant {registry.default.tenant!r}; pass --replace-binding to replace it"
        )
    return Registry(directories=registry.directories, default=DefaultBinding(tenant=tenant))


def unbind_directory(registry: Registry, root: str | Path) -> Registry:
    clean = _clean_root(str(root))
    return Registry(
        directories=tuple(item for item in registry.directories if item.root != clean),
        default=registry.default,
    )


def resolve(registry: Registry, cwd: str | Path) -> DirectoryBinding | DefaultBinding | None:
    current = Path(cwd).expanduser().resolve()
    matches: list[tuple[int, DirectoryBinding]] = []
    for binding in registry.directories:
        root = Path(binding.root)
        try:
            current.relative_to(root)
        except ValueError:
            continue
        matches.append((len(root.parts), binding))
    if matches:
        return max(matches, key=lambda item: item[0])[1]
    return registry.default


def tenant_exists(home: Path, tenant: str) -> bool:
    return (home / "tenants" / tenant / "install.lock.json").is_file()


def require_tenant_exists(home: Path, tenant: str) -> None:
    if not tenant_exists(home, tenant):
        raise BindingError(f"unknown tenant {tenant!r}; install the tenant first")


def _format(registry: Registry) -> str:
    lines: list[str] = []
    if registry.default is not None:
        lines.extend(("[default]", f"tenant = {json.dumps(registry.default.tenant)}", ""))
    for binding in registry.directories:
        lines.extend((
            "[[directory]]",
            f"root = {json.dumps(binding.root)}",
            f"tenant = {json.dumps(binding.tenant)}",
            "",
        ))
    return "\n".join(lines).rstrip() + "\n"


def _dedupe(bindings: list[DirectoryBinding]) -> list[DirectoryBinding]:
    by_root = {binding.root: binding for binding in bindings}
    return [by_root[root] for root in sorted(by_root)]


def _string(data: dict[str, Any], key: str, section: str) -> str:
    value = data.get(key)
    if not isinstance(value, str) or not value.strip():
        raise BindingError(f"{section}.{key} is required")
    return value.strip()


def _clean(value: str, name: str) -> str:
    value = str(value).strip()
    if not value:
        raise BindingError(f"{name} is required")
    return value


def _clean_root(root: str) -> str:
    return str(Path(_clean(root, "root")).expanduser().resolve())

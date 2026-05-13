"""Application binding registry for runnable agent manifests."""
from __future__ import annotations

import json
import tomllib
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from .app_manifest import Bindings, DefaultBinding, DirectoryBinding


class BindingError(ValueError):
    pass


@dataclass(frozen=True)
class Registry:
    directories: tuple[DirectoryBinding, ...] = ()
    default: DefaultBinding | None = None

    def to_json(self) -> dict[str, Any]:
        out: dict[str, Any] = {}
        if self.default is not None:
            out["default"] = {"app": self.default.app, "kernel": self.default.kernel}
        out["directory"] = [
            {"root": binding.root, "app": binding.app, "kernel": binding.kernel}
            for binding in self.directories
        ]
        return out


def registry_path(home: Path) -> Path:
    return home / "app-bindings.toml"


def load(home: Path) -> Registry:
    path = registry_path(home)
    if not path.is_file():
        return Registry()
    try:
        with path.open("rb") as f:
            data = tomllib.load(f)
    except OSError as exc:
        raise BindingError(f"read app bindings {path}: {exc}") from exc
    return parse(data)


def parse(data: dict[str, Any]) -> Registry:
    default = None
    default_raw = data.get("default")
    if default_raw is not None:
        if not isinstance(default_raw, dict):
            raise BindingError("default binding must be a table")
        default = DefaultBinding(
            app=_string(default_raw, "app", "default"),
            kernel=_string(default_raw, "kernel", "default"),
        )
    directories_raw = data.get("directory", [])
    if directories_raw is None:
        directories_raw = []
    if not isinstance(directories_raw, list):
        raise BindingError("directory bindings must be an array of tables")
    directories = []
    for index, item in enumerate(directories_raw):
        if not isinstance(item, dict):
            raise BindingError(f"directory binding {index} must be a table")
        directories.append(DirectoryBinding(
            root=_clean_root(_string(item, "root", f"directory[{index}]")),
            app=_string(item, "app", f"directory[{index}]"),
            kernel=_string(item, "kernel", f"directory[{index}]"),
        ))
    return Registry(directories=tuple(_dedupe_directories(directories)), default=default)


def save(home: Path, registry: Registry) -> None:
    path = registry_path(home)
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_text(_format(registry), encoding="utf-8")
    tmp.replace(path)


def apply_manifest_bindings(home: Path, bindings: Bindings) -> Registry:
    registry = load(home)
    for binding in bindings.directories:
        registry = bind_directory(registry, binding.root, binding.app, binding.kernel)
    if bindings.default is not None:
        registry = bind_default(registry, bindings.default.app, bindings.default.kernel)
    save(home, registry)
    return registry


def bind_directory(registry: Registry, root: str, app: str, kernel: str) -> Registry:
    binding = DirectoryBinding(root=_clean_root(root), app=_clean(app, "app"), kernel=_clean(kernel, "kernel"))
    kept = [item for item in registry.directories if item.root != binding.root]
    kept.append(binding)
    return Registry(directories=tuple(sorted(kept, key=lambda item: item.root)), default=registry.default)


def bind_default(registry: Registry, app: str, kernel: str) -> Registry:
    return Registry(
        directories=registry.directories,
        default=DefaultBinding(app=_clean(app, "app"), kernel=_clean(kernel, "kernel")),
    )


def unbind_directory(registry: Registry, root: str) -> Registry:
    clean = _clean_root(root)
    return Registry(
        directories=tuple(item for item in registry.directories if item.root != clean),
        default=registry.default,
    )


def resolve(registry: Registry, cwd: str | Path) -> DirectoryBinding | DefaultBinding | None:
    current = Path(cwd).expanduser().resolve()
    matches = []
    for binding in registry.directories:
        root = Path(binding.root)
        try:
            current.relative_to(root)
        except ValueError:
            continue
        matches.append((len(root.parts), binding))
    if matches:
        matches.sort(key=lambda item: item[0], reverse=True)
        return matches[0][1]
    return registry.default


def app_exists(home: Path, app: str) -> bool:
    return (home / "tenants" / app / "app.lock.json").is_file()


def require_app_exists(home: Path, app: str) -> None:
    if not app_exists(home, app):
        raise BindingError(f"unknown app {app!r}; apply the app manifest first")


def _format(registry: Registry) -> str:
    lines: list[str] = []
    if registry.default is not None:
        lines.append("[default]")
        lines.append(f"app = {json.dumps(registry.default.app)}")
        lines.append(f"kernel = {json.dumps(registry.default.kernel)}")
        lines.append("")
    for binding in registry.directories:
        lines.append("[[directory]]")
        lines.append(f"root = {json.dumps(binding.root)}")
        lines.append(f"app = {json.dumps(binding.app)}")
        lines.append(f"kernel = {json.dumps(binding.kernel)}")
        lines.append("")
    return "\n".join(lines).rstrip() + "\n"


def _dedupe_directories(bindings: list[DirectoryBinding]) -> list[DirectoryBinding]:
    by_root: dict[str, DirectoryBinding] = {}
    for binding in bindings:
        by_root[binding.root] = binding
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

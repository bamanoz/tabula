"""Tenant plugin configuration compilation."""
from __future__ import annotations

import json
import tomllib
from pathlib import Path
from typing import Any

from . import toml_io


def compile_plugin_configs(home: Path, tenant_dir: Path) -> tuple[str, ...]:
    plugin_root = tenant_dir / "config" / "plugins"
    if not plugin_root.is_dir():
        return ()
    compiled: list[str] = []
    for plugin_dir in sorted(entry for entry in plugin_root.iterdir() if entry.is_dir()):
        defaults_path = plugin_dir / "defaults.toml"
        if not defaults_path.is_file():
            continue
        schema = _plugin_config_schema(tenant_dir, plugin_dir.name)
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


def _plugin_config_schema(tenant_dir: Path, plugin_id: str) -> dict[str, str]:
    data = _read_toml(tenant_dir / "plugins" / plugin_id / "plugin.schema.toml")
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

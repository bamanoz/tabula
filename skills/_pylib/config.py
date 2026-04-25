#!/usr/bin/env python3
"""Shared typed config helpers for Tabula skills."""

from __future__ import annotations

import json
import os
import tomllib
from pathlib import Path

from skills._pylib.paths import global_config_file, secrets_file, skill_config_toml, tabula_home


class SkillConfigError(RuntimeError):
    """Raised when a skill configuration is missing or invalid."""


def get_tabula_home() -> Path:
    return tabula_home()


def load_global_config(tabula_home_override: str | Path | None = None) -> dict:
    tabula_root = Path(tabula_home_override) if tabula_home_override is not None else get_tabula_home()
    config = _read_toml(tabula_root / "config" / "global.toml")
    if config and not isinstance(config, dict):
        raise SkillConfigError("global config must be a TOML object")
    return config or {}


def _read_json(path: Path) -> dict:
    if not path.is_file():
        return {}
    with path.open("r", encoding="utf-8") as f:
        data = json.load(f)
    if isinstance(data, dict):
        return data
    raise SkillConfigError(f"{path}: expected JSON object")


def _read_toml(path: Path) -> dict:
    if not path.is_file():
        return {}
    with path.open("rb") as f:
        data = tomllib.load(f)
    if isinstance(data, dict):
        return data
    raise SkillConfigError(f"{path}: expected TOML object")


def _get_nested(mapping: dict, key_path: str):
    current = mapping
    for part in key_path.split("."):
        if not isinstance(current, dict) or part not in current:
            return None
        current = current[part]
    return current


def _coerce_value(field: dict, value):
    value_type = field.get("type", "string")
    if value is None:
        return None
    if value_type == "float":
        if isinstance(value, bool):
            raise SkillConfigError(f"{field['key']}: expected float")
        if isinstance(value, (int, float)):
            return float(value)
        if isinstance(value, str):
            try:
                return float(value.strip())
            except ValueError as exc:
                raise SkillConfigError(f"{field['key']}: expected float") from exc
        raise SkillConfigError(f"{field['key']}: expected float")
    if value_type == "int":
        if isinstance(value, bool):
            raise SkillConfigError(f"{field['key']}: expected integer")
        if isinstance(value, int):
            return value
        if isinstance(value, str):
            try:
                return int(value.strip())
            except ValueError as exc:
                raise SkillConfigError(f"{field['key']}: expected integer") from exc
        raise SkillConfigError(f"{field['key']}: expected integer")
    if value_type == "string":
        if not isinstance(value, str):
            raise SkillConfigError(f"{field['key']}: expected string")
        return value
    if value_type == "int_list":
        if isinstance(value, str):
            result: list[int] = []
            for item in value.split(","):
                item = item.strip()
                if not item:
                    continue
                try:
                    result.append(int(item))
                except ValueError as exc:
                    raise SkillConfigError(f"{field['key']}: expected integer list") from exc
            return result
        if isinstance(value, list) and all(isinstance(item, int) and not isinstance(item, bool) for item in value):
            return value
        raise SkillConfigError(f"{field['key']}: expected integer list")
    if value_type == "string_list":
        if isinstance(value, str):
            return [item.strip() for item in value.split(",") if item.strip()]
        if isinstance(value, list) and all(isinstance(item, str) for item in value):
            return value
        raise SkillConfigError(f"{field['key']}: expected string list")
    raise SkillConfigError(f"{field['key']}: unsupported field type {value_type!r}")


def _resolve_secret_ref(ref: dict, *, field: dict, secrets: dict):
    source = ref.get("source")
    ref_id = ref.get("id")
    if not isinstance(source, str) or not isinstance(ref_id, str) or not ref_id.strip():
        raise SkillConfigError(f"{field['key']}: invalid secret ref")
    if source == "store":
        value = secrets.get(ref_id)
        return value
    if source == "env":
        return os.environ.get(ref_id)
    if source == "file":
        path = Path(ref_id).expanduser()
        if not path.is_file():
            return None
        return path.read_text(encoding="utf-8").strip()
    raise SkillConfigError(f"{field['key']}: unsupported secret source {source!r}")


def _resolve_field(field: dict, *, global_cfg: dict, file_cfg: dict, secrets: dict):
    key = field["key"]
    uses_global = isinstance(field.get("global_key"), str) and field.get("global_key", "").strip() != ""
    for env_name in [field.get("env"), *field.get("env_aliases", [])]:
        if not env_name:
            continue
        env_value = os.environ.get(env_name)
        if env_value is not None and env_value != "":
            return _coerce_value(field, env_value)

    global_key = field.get("global_key")
    if isinstance(global_key, str) and global_key.strip():
        global_value = _get_nested(global_cfg, global_key)
        if isinstance(global_value, dict) and field.get("secret", False):
            value = _resolve_secret_ref(global_value, field=field, secrets=secrets)
            if value is not None:
                return _coerce_value(field, value)
        elif global_value is not None:
            return _coerce_value(field, global_value)

    if not uses_global:
        file_value = _get_nested(file_cfg, key)
        if isinstance(file_value, dict) and field.get("secret", False):
            value = _resolve_secret_ref(file_value, field=field, secrets=secrets)
            if value is not None:
                return _coerce_value(field, value)
        elif file_value is not None:
            return _coerce_value(field, file_value)

    store_ids = field.get("store_ids")
    if field.get("secret", False):
        candidates: list[str] = []
        if isinstance(store_ids, list):
            candidates.extend(store_id for store_id in store_ids if isinstance(store_id, str))
        store_id = field.get("store_id")
        if isinstance(store_id, str):
            candidates.append(store_id)
        for candidate in candidates:
            value = secrets.get(candidate)
            if value is not None:
                return _coerce_value(field, value)

    default = field.get("default")
    if default is not None:
        return _coerce_value(field, default)

    if field.get("required", False):
        names = [name for name in [field.get("env"), *field.get("env_aliases", [])] if name]
        if uses_global:
            config_source = str(global_config_file())
        else:
            config_source = str(get_tabula_home() / 'config' / 'skills' / (field['skill_id'] + '.toml'))
        raise SkillConfigError(
            f"missing required config {key!r}; checked env {names}, "
            f"{config_source} and secrets store"
        )
    return None


def _load_schema(skill_dir: Path) -> dict:
    path = skill_dir / "SKILL.config.json"
    data = _read_json(path)
    if not data:
        raise SkillConfigError(f"{path}: missing skill config schema")
    if not isinstance(data.get("id"), str) or not data["id"].strip():
        raise SkillConfigError(f"{path}: schema must define non-empty id")
    entries = data.get("config", {}).get("entries")
    if not isinstance(entries, list):
        raise SkillConfigError(f"{path}: schema must define config.entries list")
    for field in entries:
        if not isinstance(field, dict) or not isinstance(field.get("key"), str):
            raise SkillConfigError(f"{path}: invalid config entry")
        field.setdefault("type", "string")
        field.setdefault("env_aliases", [])
        field["skill_id"] = data["id"]
    return data


def load_skill_config(skill_dir: str | Path, tabula_home_override: str | Path | None = None) -> dict:
    """Load a skill config from env, skill TOML, and secrets.json.

    Precedence for this pilot implementation:
    1. canonical env / env aliases
    2. skill config file
    3. secret store fallback for secret fields
    4. schema defaults
    """

    resolved_skill_dir = Path(skill_dir)
    schema = _load_schema(resolved_skill_dir)
    tabula_home = Path(tabula_home_override) if tabula_home_override is not None else get_tabula_home()
    global_cfg = load_global_config(tabula_home_override=tabula_home)
    file_cfg = _read_toml(tabula_home / "config" / "skills" / f"{schema['id']}.toml")
    secrets = _read_json(tabula_home / "secrets.json")

    resolved: dict[str, object] = {}
    for field in schema["config"]["entries"]:
        resolved[field["key"]] = _resolve_field(field, global_cfg=global_cfg, file_cfg=file_cfg, secrets=secrets)
    return resolved

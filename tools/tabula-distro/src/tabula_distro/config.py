"""distro.toml parsing.

Schema (all sections optional; absent file = empty config, legacy behavior):

    [distro]
    name = "ouroboros"
    # kernel = ">=0.3"   # reserved, not enforced yet

    [[bundles]]
    name     = "memory"
    source   = "local:../../bundles/memory"
    skills   = ["memory-save", "memory-search"]   # optional allowlist
    override = false                              # explicit conflict override

    [[skills]]
    name     = "weather"
    source   = "git+https://github.com/foo/weather-skill.git@main#path=skill"
    override = false

A sibling ``distro.override.toml`` (gitignored) is merged on top: any
``[[bundles]]``/``[[skills]]`` entry there replaces the entry with the same
``name`` in the base config. New entries are appended.
"""
from __future__ import annotations

import sys
import tomllib
from dataclasses import dataclass, field
from pathlib import Path


@dataclass(frozen=True)
class BundleEntry:
    name: str
    source: str
    skills: tuple[str, ...] = ()
    override: bool = False


@dataclass(frozen=True)
class SkillEntry:
    name: str
    source: str
    override: bool = False


@dataclass(frozen=True)
class DistroConfig:
    path: Path  # directory containing distro.toml (the distro root)
    name: str
    bundles: tuple[BundleEntry, ...] = ()
    skills: tuple[SkillEntry, ...] = ()


def load(distro_dir: Path, *, override_name: str | None = None) -> DistroConfig:
    """Load distro.toml from ``distro_dir``; return empty config if missing."""
    distro_dir = distro_dir.resolve()
    base = _read_toml(distro_dir / "distro.toml")
    override = _read_toml(distro_dir / "distro.override.toml")
    merged = _merge(base, override)

    name = override_name or _section(merged, "distro").get("name") or distro_dir.name
    return DistroConfig(
        path=distro_dir,
        name=name,
        bundles=tuple(_parse_bundle(entry) for entry in merged.get("bundles", [])),
        skills=tuple(_parse_skill(entry) for entry in merged.get("skills", [])),
    )


def _read_toml(path: Path) -> dict:
    if not path.is_file():
        return {}
    with path.open("rb") as f:
        return tomllib.load(f)


def _section(data: dict, key: str) -> dict:
    value = data.get(key)
    return value if isinstance(value, dict) else {}


def _merge(base: dict, override: dict) -> dict:
    if not override:
        return base
    result = dict(base)
    if "distro" in override:
        result["distro"] = {**_section(base, "distro"), **_section(override, "distro")}
    for list_key in ("bundles", "skills"):
        if list_key not in override:
            continue
        by_name: dict[str, dict] = {}
        order: list[str] = []
        for entry in list(base.get(list_key, [])) + list(override.get(list_key, [])):
            if not isinstance(entry, dict) or "name" not in entry:
                continue
            n = entry["name"]
            if n not in by_name:
                order.append(n)
            by_name[n] = entry  # later wins
        result[list_key] = [by_name[n] for n in order]
    return result


def _parse_bundle(entry: dict) -> BundleEntry:
    _require(entry, ("name", "source"), kind="bundle")
    skills = entry.get("skills") or ()
    if not all(isinstance(s, str) for s in skills):
        raise ConfigError(f"bundle {entry['name']!r}: 'skills' must be list[str]")
    return BundleEntry(
        name=entry["name"],
        source=entry["source"],
        skills=tuple(skills),
        override=bool(entry.get("override", False)),
    )


def _parse_skill(entry: dict) -> SkillEntry:
    _require(entry, ("name", "source"), kind="skill")
    return SkillEntry(
        name=entry["name"],
        source=entry["source"],
        override=bool(entry.get("override", False)),
    )


def _require(entry: dict, keys: tuple[str, ...], *, kind: str) -> None:
    missing = [k for k in keys if k not in entry]
    if missing:
        raise ConfigError(f"{kind} entry missing keys: {', '.join(missing)} (got {entry!r})")


class ConfigError(ValueError):
    """Raised for malformed distro.toml entries."""

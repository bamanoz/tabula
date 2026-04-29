"""distro.toml parsing.

Schema (all sections optional; absent file = empty config, legacy behavior):

    [distro]
    name    = "ouroboros"
    version = "0.1.0"           # optional but recommended

    [requires]
    kernel = ">=0.8.0,<1.0.0"   # enforced by tabula-distro install

    [sources.tabula-bundles]
    source = "git+https://github.com/bamanoz/tabula-bundles.git@main"

    [[bundles]]
    name       = "memory"
    source     = "source:tabula-bundles#path=memory"
    components = ["memory-save", "memory-search"] # optional allowlist
    override   = false                            # explicit conflict override

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

from .semver import Constraint, Version, VersionError


@dataclass(frozen=True)
class BundleEntry:
    name: str
    source: str
    # None  = no allowlist, install every component in the bundle
    # ()    = explicit empty allowlist, install no components
    # (...) = install only the named components
    components: tuple[str, ...] | None = None
    override: bool = False

    @property
    def skills(self) -> tuple[str, ...] | None:
        """Backward-compatible alias for the legacy bundle skill allowlist."""
        return self.components


@dataclass(frozen=True)
class SkillEntry:
    name: str
    source: str
    override: bool = False


@dataclass(frozen=True)
class SourceAlias:
    name: str
    source: str


@dataclass(frozen=True)
class DistroConfig:
    path: Path  # directory containing distro.toml (the distro root)
    name: str
    version: Version | None = None
    requires_kernel: Constraint | None = None
    sources: dict[str, SourceAlias] = field(default_factory=dict)
    bundles: tuple[BundleEntry, ...] = ()
    skills: tuple[SkillEntry, ...] = ()
    plugins: tuple[SkillEntry, ...] = ()


def load(distro_dir: Path, *, override_name: str | None = None) -> DistroConfig:
    """Load distro.toml from ``distro_dir``; return empty config if missing."""
    distro_dir = distro_dir.resolve()
    base = _read_toml(distro_dir / "distro.toml")
    override = _read_toml(distro_dir / "distro.override.toml")
    merged = _merge(base, override)

    distro_section = _section(merged, "distro")
    requires_section = _section(merged, "requires")

    name = override_name or distro_section.get("name") or distro_dir.name

    version_raw = distro_section.get("version")
    try:
        version = Version.parse(version_raw) if version_raw else None
    except VersionError as exc:
        raise ConfigError(f"{distro_dir}/distro.toml: invalid distro.version: {exc}") from exc

    kernel_raw = requires_section.get("kernel")
    try:
        requires_kernel = Constraint.parse(kernel_raw) if kernel_raw else None
    except VersionError as exc:
        raise ConfigError(f"{distro_dir}/distro.toml: invalid requires.kernel: {exc}") from exc

    return DistroConfig(
        path=distro_dir,
        name=name,
        version=version,
        requires_kernel=requires_kernel,
        sources=_parse_sources(_section(merged, "sources")),
        bundles=tuple(_parse_bundle(entry) for entry in merged.get("bundles", [])),
        skills=tuple(_parse_skill(entry) for entry in merged.get("skills", [])),
        plugins=tuple(_parse_skill(entry) for entry in merged.get("plugins", [])),
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
    for dict_key in ("distro", "requires", "sources"):
        if dict_key in override:
            merged_section = {**_section(base, dict_key)}
            for key, value in _section(override, dict_key).items():
                if isinstance(value, dict) and isinstance(merged_section.get(key), dict):
                    merged_section[key] = {**merged_section[key], **value}
                else:
                    merged_section[key] = value
            result[dict_key] = merged_section
    for list_key in ("bundles", "skills", "plugins"):
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
    has_components = "components" in entry
    has_skills = "skills" in entry
    if has_components and has_skills:
        raise ConfigError(f"bundle {entry['name']!r}: use only one of 'components' or legacy 'skills'")
    if has_components or has_skills:
        key = "components" if has_components else "skills"
        components_raw = entry[key] or []
        if not isinstance(components_raw, list) or not all(isinstance(s, str) for s in components_raw):
            raise ConfigError(f"bundle {entry['name']!r}: '{key}' must be list[str]")
        components: tuple[str, ...] | None = tuple(components_raw)
    else:
        components = None
    return BundleEntry(
        name=entry["name"],
        source=entry["source"],
        components=components,
        override=bool(entry.get("override", False)),
    )


def _parse_skill(entry: dict) -> SkillEntry:
    _require(entry, ("name", "source"), kind="skill")
    return SkillEntry(
        name=entry["name"],
        source=entry["source"],
        override=bool(entry.get("override", False)),
    )


def _parse_sources(section: dict) -> dict[str, SourceAlias]:
    aliases: dict[str, SourceAlias] = {}
    for name, data in section.items():
        if not isinstance(data, dict):
            raise ConfigError(f"source alias {name!r}: expected TOML table")
        _require(data, ("source",), kind=f"source alias {name!r}")
        aliases[name] = SourceAlias(name=name, source=data["source"])
    return aliases


def _require(entry: dict, keys: tuple[str, ...], *, kind: str) -> None:
    missing = [k for k in keys if k not in entry]
    if missing:
        raise ConfigError(f"{kind} entry missing keys: {', '.join(missing)} (got {entry!r})")


class ConfigError(ValueError):
    """Raised for malformed distro.toml entries."""

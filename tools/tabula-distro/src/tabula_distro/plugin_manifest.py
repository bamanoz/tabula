"""Plugin manifest parsing (``plugin.toml``) — minimal install-time view.

The kernel owns the canonical schema (see ``internal/kernel/plugin/manifest.go``
and ``docs/PROTOCOL.md``). This module reads only the fields the distro
installer needs to enforce compatibility:

    [requires]
    kernel           = ">=0.9.0,<1.0.0"
    protocol_version = 1            # int or [int, ...]
    sdk              = "tabula-plugin-sdk>=0.1.0,<0.2.0"

A ``plugin.toml`` without a ``[requires]`` block is tolerated during the
rollout window: the installer logs a warning and skips compat checks for
that plugin. Once all upstream plugin.toml files declare the block we will
flip this to a hard error.
"""
from __future__ import annotations

import re
import tomllib
from dataclasses import dataclass
from pathlib import Path

from .semver import Constraint, VersionError


@dataclass(frozen=True)
class SDKRequirement:
    name: str
    constraint: Constraint


@dataclass(frozen=True)
class PluginRequires:
    kernel: Constraint | None
    protocol_versions: tuple[int, ...]
    sdk: SDKRequirement | None


@dataclass(frozen=True)
class PluginManifest:
    path: Path
    requires: PluginRequires | None  # None when [requires] absent (rollout)


class PluginManifestError(ValueError):
    """Raised when a plugin.toml is malformed."""


_SDK_RE = re.compile(r"^(@?[A-Za-z0-9._\-/]+?)\s*((?:[<>=!]=?|\s).*)$")


def load_plugin_manifest(plugin_dir: Path) -> PluginManifest:
    """Load ``plugin.toml`` from a plugin directory."""
    manifest_path = plugin_dir / "plugin.toml"
    if not manifest_path.is_file():
        raise PluginManifestError(f"missing plugin.toml: {manifest_path}")

    with manifest_path.open("rb") as f:
        try:
            data = tomllib.load(f)
        except tomllib.TOMLDecodeError as exc:
            raise PluginManifestError(f"{manifest_path}: invalid TOML: {exc}") from exc

    requires_raw = data.get("requires")
    if requires_raw is None:
        return PluginManifest(path=manifest_path, requires=None)
    if not isinstance(requires_raw, dict):
        raise PluginManifestError(f"{manifest_path}: [requires] must be a TOML table")

    requires = _parse_requires(manifest_path, requires_raw)
    return PluginManifest(path=manifest_path, requires=requires)


def _parse_requires(manifest_path: Path, raw: dict) -> PluginRequires:
    kernel_raw = raw.get("kernel")
    proto_raw = raw.get("protocol_version")
    sdk_raw = raw.get("sdk")

    try:
        kernel = Constraint.parse(kernel_raw) if kernel_raw else None
    except VersionError as exc:
        raise PluginManifestError(f"{manifest_path}: invalid requires.kernel: {exc}") from exc

    protos = _parse_protocol_versions(manifest_path, proto_raw)

    sdk: SDKRequirement | None = None
    if sdk_raw is not None:
        if not isinstance(sdk_raw, str) or not sdk_raw.strip():
            raise PluginManifestError(
                f"{manifest_path}: requires.sdk must be a non-empty string "
                "like 'tabula-plugin-sdk>=0.1.0,<0.2.0'"
            )
        sdk = _parse_sdk(manifest_path, sdk_raw)

    return PluginRequires(kernel=kernel, protocol_versions=protos, sdk=sdk)


def _parse_protocol_versions(manifest_path: Path, raw: object) -> tuple[int, ...]:
    if raw is None:
        return ()
    values: list[int] = []
    items: list[object]
    if isinstance(raw, int) and not isinstance(raw, bool):
        items = [raw]
    elif isinstance(raw, list):
        items = list(raw)
    else:
        raise PluginManifestError(
            f"{manifest_path}: requires.protocol_version must be an int or array of ints"
        )
    for item in items:
        if isinstance(item, bool) or not isinstance(item, int) or item < 1:
            raise PluginManifestError(
                f"{manifest_path}: requires.protocol_version entries must be positive ints"
            )
        values.append(item)
    if not values:
        raise PluginManifestError(
            f"{manifest_path}: requires.protocol_version must list at least one version"
        )
    return tuple(sorted(set(values)))


def _parse_sdk(manifest_path: Path, raw: str) -> SDKRequirement:
    text = raw.strip()
    m = _SDK_RE.match(text)
    if m is None:
        # Bare name with no constraint — treat as exact match against any
        # version is meaningless here; require a constraint.
        raise PluginManifestError(
            f"{manifest_path}: requires.sdk must include a version range, "
            f"got {raw!r} (e.g. 'tabula-plugin-sdk>=0.1.0,<0.2.0')"
        )
    name = m.group(1).strip()
    range_text = m.group(2).strip()
    if not name or not range_text:
        raise PluginManifestError(
            f"{manifest_path}: requires.sdk must look like '<name><range>', got {raw!r}"
        )
    try:
        constraint = Constraint.parse(range_text)
    except VersionError as exc:
        raise PluginManifestError(
            f"{manifest_path}: invalid requires.sdk range {range_text!r}: {exc}"
        ) from exc
    return SDKRequirement(name=name, constraint=constraint)

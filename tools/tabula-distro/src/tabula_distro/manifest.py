"""Bundle manifest parsing (``bundle.toml``).

Schema:

    [bundle]
    name    = "drivers"
    version = "0.1.0"
    components = ["driver", "mcp"]

    [requires]
    kernel = ">=0.8.0,<1.0.0"

A bundle without a ``bundle.toml`` is treated as an unversioned legacy bundle:
no compatibility check is performed and ``BundleManifest.version`` is ``None``.
We may tighten this later.
"""
from __future__ import annotations

import tomllib
from dataclasses import dataclass
from pathlib import Path

from .semver import Constraint, Version, VersionError


@dataclass(frozen=True)
class BundleManifest:
    name: str | None
    version: Version | None
    requires_kernel: Constraint | None
    path: Path  # bundle root
    # None means legacy walk-discovery; an empty tuple means explicit empty bundle.
    components: tuple[str, ...] | None = None

    @property
    def is_versioned(self) -> bool:
        return self.version is not None


class ManifestError(ValueError):
    """Raised when a bundle.toml is malformed."""


def load_bundle_manifest(bundle_root: Path) -> BundleManifest:
    """Load ``bundle.toml`` from ``bundle_root``; return placeholder if absent."""
    manifest_path = bundle_root / "bundle.toml"
    if not manifest_path.is_file():
        return BundleManifest(name=None, version=None, requires_kernel=None, path=bundle_root)

    with manifest_path.open("rb") as f:
        data = tomllib.load(f)

    bundle = data.get("bundle") or {}
    requires = data.get("requires") or {}

    name = bundle.get("name")
    version_raw = bundle.get("version")
    components = _parse_components(manifest_path, bundle.get("components"))
    kernel_raw = requires.get("kernel")

    try:
        version = Version.parse(version_raw) if version_raw else None
    except VersionError as exc:
        raise ManifestError(f"{manifest_path}: invalid bundle.version: {exc}") from exc

    try:
        kernel = Constraint.parse(kernel_raw) if kernel_raw else None
    except VersionError as exc:
        raise ManifestError(f"{manifest_path}: invalid requires.kernel: {exc}") from exc

    return BundleManifest(name=name, version=version, requires_kernel=kernel, path=bundle_root,
                          components=components)


def _parse_components(manifest_path: Path, raw: object) -> tuple[str, ...] | None:
    if raw is None:
        return None
    if not isinstance(raw, list):
        raise ManifestError(f"{manifest_path}: bundle.components must be list[str]")
    components: list[str] = []
    for value in raw:
        if not isinstance(value, str) or not value.strip():
            raise ManifestError(f"{manifest_path}: bundle.components entries must be non-empty strings")
        p = Path(value)
        if p.is_absolute() or ".." in p.parts:
            raise ManifestError(f"{manifest_path}: bundle.components entry must be relative and stay inside bundle: {value!r}")
        components.append(p.as_posix())
    return tuple(components)

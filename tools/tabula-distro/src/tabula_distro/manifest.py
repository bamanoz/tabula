"""Bundle manifest parsing (``bundle.toml``).

Schema:

    [bundle]
    name    = "drivers"
    version = "0.1.0"

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
    kernel_raw = requires.get("kernel")

    try:
        version = Version.parse(version_raw) if version_raw else None
    except VersionError as exc:
        raise ManifestError(f"{manifest_path}: invalid bundle.version: {exc}") from exc

    try:
        kernel = Constraint.parse(kernel_raw) if kernel_raw else None
    except VersionError as exc:
        raise ManifestError(f"{manifest_path}: invalid requires.kernel: {exc}") from exc

    return BundleManifest(name=name, version=version, requires_kernel=kernel, path=bundle_root)

"""Bundle manifest parsing (``bundle.toml``).

Schema:

    [bundle]
    name    = "drivers"
    version = "0.1.0"
    components = ["driver", "mcp"]

    [requires]
    kernel = ">=0.8.0,<1.0.0"

    [[exports.python_packages]]
    name = "tabula_session_sdk"
    path = "sessions/sdk/python/src/tabula_session_sdk"
    public = true
    owner = "sessions"

    [[exports.typescript_packages]]
    name = "@tabula/skill-sdk"
    path = "skills/sdk/typescript"
    public = true
    owner = "skills"

    [[dependencies]]
    bundle = "extensions"
    components = ["sessions"]
    python_packages = ["tabula_session_sdk"]
    typescript_packages = ["@tabula/skill-sdk"]

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
    exports_python_packages: tuple["PythonPackageExport", ...] = ()
    exports_typescript_packages: tuple["TypeScriptPackageExport", ...] = ()
    dependencies: tuple["BundleDependency", ...] = ()

    @property
    def is_versioned(self) -> bool:
        return self.version is not None


class ManifestError(ValueError):
    """Raised when a bundle.toml is malformed."""


@dataclass(frozen=True)
class PythonPackageExport:
    name: str
    path: str
    public: bool = True
    owner: str = ""


@dataclass(frozen=True)
class TypeScriptPackageExport:
    name: str
    path: str
    public: bool = True
    owner: str = ""


@dataclass(frozen=True)
class BundleDependency:
    bundle: str
    components: tuple[str, ...] = ()
    python_packages: tuple[str, ...] = ()
    typescript_packages: tuple[str, ...] = ()


def load_bundle_manifest(bundle_root: Path) -> BundleManifest:
    """Load ``bundle.toml`` from ``bundle_root``; return placeholder if absent."""
    manifest_path = bundle_root / "bundle.toml"
    if not manifest_path.is_file():
        return BundleManifest(name=None, version=None, requires_kernel=None, path=bundle_root)

    with manifest_path.open("rb") as f:
        data = tomllib.load(f)

    bundle = data.get("bundle") or {}
    requires = data.get("requires") or {}
    exports = data.get("exports") or {}

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

    return BundleManifest(
        name=name,
        version=version,
        requires_kernel=kernel,
        path=bundle_root,
        components=components,
        exports_python_packages=_parse_python_package_exports(manifest_path, exports.get("python_packages")),
        exports_typescript_packages=_parse_typescript_package_exports(manifest_path, exports.get("typescript_packages")),
        dependencies=_parse_dependencies(manifest_path, data.get("dependencies")),
    )


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


def _parse_python_package_exports(manifest_path: Path, raw: object) -> tuple[PythonPackageExport, ...]:
    if raw is None:
        return ()
    if not isinstance(raw, list):
        raise ManifestError(f"{manifest_path}: exports.python_packages must be an array of tables")
    exports: list[PythonPackageExport] = []
    for entry in raw:
        if not isinstance(entry, dict):
            raise ManifestError(f"{manifest_path}: exports.python_packages entries must be tables")
        name = str(entry.get("name") or "").strip()
        path = str(entry.get("path") or "").strip()
        if not name or not path:
            raise ManifestError(f"{manifest_path}: exports.python_packages entries require name and path")
        rel = Path(path)
        if rel.is_absolute() or ".." in rel.parts:
            raise ManifestError(f"{manifest_path}: exports.python_packages path must stay inside bundle: {path!r}")
        if not _valid_python_package_name(name):
            raise ManifestError(f"{manifest_path}: exports.python_packages name must be a single package name: {name!r}")
        exports.append(PythonPackageExport(name=name, path=rel.as_posix(), public=_parse_public(manifest_path, "exports.python_packages", entry), owner=str(entry.get("owner") or "").strip()))
    return tuple(exports)


def _parse_typescript_package_exports(manifest_path: Path, raw: object) -> tuple[TypeScriptPackageExport, ...]:
    if raw is None:
        return ()
    if not isinstance(raw, list):
        raise ManifestError(f"{manifest_path}: exports.typescript_packages must be an array of tables")
    exports: list[TypeScriptPackageExport] = []
    for entry in raw:
        if not isinstance(entry, dict):
            raise ManifestError(f"{manifest_path}: exports.typescript_packages entries must be tables")
        name = str(entry.get("name") or "").strip()
        path = str(entry.get("path") or "").strip()
        if not name or not path:
            raise ManifestError(f"{manifest_path}: exports.typescript_packages entries require name and path")
        rel = Path(path)
        if rel.is_absolute() or ".." in rel.parts:
            raise ManifestError(f"{manifest_path}: exports.typescript_packages path must stay inside bundle: {path!r}")
        if not _valid_typescript_package_name(name):
            raise ManifestError(f"{manifest_path}: exports.typescript_packages name must be an npm package name: {name!r}")
        exports.append(TypeScriptPackageExport(name=name, path=rel.as_posix(), public=_parse_public(manifest_path, "exports.typescript_packages", entry), owner=str(entry.get("owner") or "").strip()))
    return tuple(exports)


def _parse_public(manifest_path: Path, section: str, entry: dict) -> bool:
    value = entry.get("public", True)
    if not isinstance(value, bool):
        raise ManifestError(f"{manifest_path}: {section} public must be a boolean")
    return value


def _valid_python_package_name(name: str) -> bool:
    return bool(name) and Path(name).name == name and name.replace("_", "").replace("-", "").isalnum()


def _valid_typescript_package_name(name: str) -> bool:
    if not name or name.endswith("/"):
        return False
    parts = name.split("/")
    if len(parts) == 1:
        return bool(parts[0]) and not parts[0].startswith("@")
    if len(parts) == 2:
        scope, package = parts
        return scope.startswith("@") and len(scope) > 1 and bool(package)
    return False


def _parse_dependencies(manifest_path: Path, raw: object) -> tuple[BundleDependency, ...]:
    if raw is None:
        return ()
    if not isinstance(raw, list):
        raise ManifestError(f"{manifest_path}: dependencies must be an array of tables")
    deps: list[BundleDependency] = []
    for entry in raw:
        if not isinstance(entry, dict):
            raise ManifestError(f"{manifest_path}: dependencies entries must be tables")
        bundle = str(entry.get("bundle") or "").strip()
        if not bundle:
            raise ManifestError(f"{manifest_path}: dependency entry missing bundle")
        components = entry.get("components") or []
        if not isinstance(components, list) or not all(isinstance(item, str) and item.strip() for item in components):
            raise ManifestError(f"{manifest_path}: dependency {bundle!r} components must be list[str]")
        normalized_components: list[str] = []
        for component in components:
            rel = Path(component.strip())
            if rel.is_absolute() or ".." in rel.parts:
                raise ManifestError(
                    f"{manifest_path}: dependency {bundle!r} component must be relative and stay inside bundle: {component!r}"
                )
            normalized_components.append(rel.as_posix())
        pkgs = entry.get("python_packages") or []
        if not isinstance(pkgs, list) or not all(isinstance(item, str) and item.strip() for item in pkgs):
            raise ManifestError(f"{manifest_path}: dependency {bundle!r} python_packages must be list[str]")
        ts_pkgs = entry.get("typescript_packages") or []
        if not isinstance(ts_pkgs, list) or not all(isinstance(item, str) and item.strip() for item in ts_pkgs):
            raise ManifestError(f"{manifest_path}: dependency {bundle!r} typescript_packages must be list[str]")
        deps.append(BundleDependency(
            bundle=bundle,
            components=tuple(normalized_components),
            python_packages=tuple(str(item).strip() for item in pkgs),
            typescript_packages=tuple(str(item).strip() for item in ts_pkgs),
        ))
    return tuple(deps)

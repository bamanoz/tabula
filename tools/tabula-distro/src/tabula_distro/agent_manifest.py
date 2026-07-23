"""Optional source-controlled project declaration for ``tabula-agent``."""
from __future__ import annotations

import tomllib
from dataclasses import dataclass
from pathlib import Path

from . import toml_io


FILENAME = "tabula.agent.toml"


class AgentManifestError(ValueError):
    pass


@dataclass(frozen=True)
class AgentManifest:
    path: Path
    source: str
    values: dict[str, object]


def load(path: Path) -> AgentManifest:
    path = path.expanduser().resolve()
    try:
        with path.open("rb") as handle:
            data = tomllib.load(handle)
    except (OSError, tomllib.TOMLDecodeError) as exc:
        raise AgentManifestError(f"read agent manifest {path}: {exc}") from exc
    unknown = set(data) - {"distro", "values"}
    if unknown:
        raise AgentManifestError(f"unsupported agent manifest section: {sorted(unknown)[0]}")
    distro = data.get("distro")
    if not isinstance(distro, dict):
        raise AgentManifestError("agent manifest requires [distro].source")
    unknown_distro = set(distro) - {"source"}
    if unknown_distro:
        raise AgentManifestError(f"unsupported [distro] field: {sorted(unknown_distro)[0]}")
    source = distro.get("source")
    if not isinstance(source, str) or not source.strip():
        raise AgentManifestError("agent manifest requires [distro].source")
    values = data.get("values", {})
    if not isinstance(values, dict):
        raise AgentManifestError("[values] must be a table")
    return AgentManifest(path=path, source=source.strip(), values=values)


def create(path: Path, source: str) -> Path:
    path = path.expanduser().resolve()
    if path.exists():
        raise AgentManifestError(f"agent manifest already exists: {path}")
    source = source.strip()
    if not source:
        raise AgentManifestError("distro source is required")
    doc = toml_io.require_tomlkit().document()
    distro = toml_io.require_tomlkit().table()
    distro["source"] = source
    doc["distro"] = distro
    doc["values"] = toml_io.require_tomlkit().table()
    toml_io.dump(path, doc)
    return path


def write_values(path: Path, values: dict[str, object]) -> None:
    doc = toml_io.require_tomlkit().document()
    for key, value in values.items():
        doc[key] = toml_io.to_tomlkit(value)
    toml_io.dump(path, doc)

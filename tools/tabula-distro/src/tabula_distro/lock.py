"""distro.lock.json read/write.

Format (version 3)::

    {
      "version": 3,
      "distro": "demo",
      "generated_at": "2026-04-21T14:30:00Z",
      "kernel_version": "0.9.0",
      "plugin_protocol_version": 1,
      "sdk_versions": {
        "tabula-plugin-sdk": "0.1.0",
        "@tabula/skill-sdk":  "0.1.0"
      },
      "bundles": { ... },
      "skills":  { ... },
      "plugins": { ... },
      "clients": { ... }
    }

The ``plugin_protocol_version`` and ``sdk_versions`` fields snapshot the
protocol/SDK surface that was active when the generation was installed.
They let ``tabula-distro status`` (and humans reading the lock) confirm the
exact contract a generation satisfies — independent of any later SDK or
kernel upgrade.
"""
from __future__ import annotations

import datetime as _dt
import json
from dataclasses import dataclass, field
from pathlib import Path


LOCK_VERSION = 3


@dataclass
class LockEntry:
    source: str
    resolved_sha: str | None = None   # git only
    resolved_ref: str | None = None   # git only
    subpath: str | None = None        # git only
    resolved_path: str | None = None  # local only
    fetched_at: str | None = None
    version: str | None = None        # bundle/skill version from manifest, if known

    def to_json(self) -> dict:
        out: dict = {"source": self.source}
        for key in ("resolved_sha", "resolved_ref", "subpath",
                    "resolved_path", "fetched_at", "version"):
            val = getattr(self, key)
            if val is not None:
                out[key] = val
        return out

    @classmethod
    def from_json(cls, data: dict) -> "LockEntry":
        return cls(
            source=data["source"],
            resolved_sha=data.get("resolved_sha"),
            resolved_ref=data.get("resolved_ref"),
            subpath=data.get("subpath"),
            resolved_path=data.get("resolved_path"),
            fetched_at=data.get("fetched_at"),
            version=data.get("version"),
        )


@dataclass
class Lock:
    distro: str
    bundles: dict[str, LockEntry] = field(default_factory=dict)
    skills: dict[str, LockEntry] = field(default_factory=dict)
    plugins: dict[str, LockEntry] = field(default_factory=dict)
    clients: dict[str, LockEntry] = field(default_factory=dict)
    generated_at: str | None = None
    distro_source: str | None = None  # original URI passed to install (for `update`)
    distro_version: str | None = None  # [distro].version, if declared
    kernel_version: str | None = None  # installed kernel version at install time
    plugin_protocol_version: int | None = None  # max plugin protocol version kernel can speak
    sdk_versions: dict[str, str] = field(default_factory=dict)  # SDK name → version

    def to_json(self) -> dict:
        out: dict = {
            "version": LOCK_VERSION,
            "distro": self.distro,
            "generated_at": self.generated_at or now_iso(),
            "bundles": {k: v.to_json() for k, v in self.bundles.items()},
            "skills": {k: v.to_json() for k, v in self.skills.items()},
            "plugins": {k: v.to_json() for k, v in self.plugins.items()},
            "clients": {k: v.to_json() for k, v in self.clients.items()},
        }
        if self.distro_source is not None:
            out["distro_source"] = self.distro_source
        if self.distro_version is not None:
            out["distro_version"] = self.distro_version
        if self.kernel_version is not None:
            out["kernel_version"] = self.kernel_version
        if self.plugin_protocol_version is not None:
            out["plugin_protocol_version"] = self.plugin_protocol_version
        if self.sdk_versions:
            out["sdk_versions"] = dict(sorted(self.sdk_versions.items()))
        return out

    @classmethod
    def from_json(cls, data: dict) -> "Lock":
        version = data.get("version")
        if version == 1:
            data = _migrate_v1_to_v2(data)
            version = data.get("version")
        if version == 2:
            data = _migrate_v2_to_v3(data)
            version = data.get("version")
        if version != LOCK_VERSION:
            raise LockError(f"unsupported lock version: {version}")
        return cls(
            distro=data.get("distro", ""),
            bundles={k: LockEntry.from_json(v) for k, v in data.get("bundles", {}).items()},
            skills={k: LockEntry.from_json(v) for k, v in data.get("skills", {}).items()},
            plugins={k: LockEntry.from_json(v) for k, v in data.get("plugins", {}).items()},
            clients={k: LockEntry.from_json(v) for k, v in data.get("clients", data.get("drivers", {})).items()},
            generated_at=data.get("generated_at"),
            distro_source=data.get("distro_source"),
            distro_version=data.get("distro_version"),
            kernel_version=data.get("kernel_version"),
            plugin_protocol_version=data.get("plugin_protocol_version"),
            sdk_versions=dict(data.get("sdk_versions") or {}),
        )


def _migrate_v1_to_v2(data: dict) -> dict:
    migrated = dict(data)
    migrated["version"] = 2
    migrated.setdefault("plugins", {})
    migrated.setdefault("clients", {})
    return migrated


def _migrate_v2_to_v3(data: dict) -> dict:
    """Bring a v2 lock forward.

    v2 has no plugin protocol or SDK metadata. We don't fabricate values
    on read — leaving them ``None`` simply means the next install will
    populate them from the live kernel/staged package surface and rewrite the lock
    at v3.
    """
    migrated = dict(data)
    migrated["version"] = LOCK_VERSION
    migrated.setdefault("plugin_protocol_version", None)
    migrated.setdefault("sdk_versions", {})
    return migrated


class LockError(ValueError):
    pass


def now_iso() -> str:
    return _dt.datetime.now(_dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def load(path: Path) -> Lock | None:
    if not path.is_file():
        return None
    data = json.loads(path.read_text(encoding="utf-8"))
    return Lock.from_json(data)


def save(path: Path, lock: Lock) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    payload = json.dumps(lock.to_json(), indent=2, sort_keys=True) + "\n"
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_text(payload, encoding="utf-8")
    tmp.replace(path)

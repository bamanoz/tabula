"""distro.lock.json read/write.

Format (version 1)::

    {
      "version": 1,
      "distro": "ouroboros",
      "generated_at": "2026-04-21T14:30:00Z",
      "bundles": {
        "memory": {
          "source":       "git+https://.../@main",
          "resolved_sha": "abc123...",
          "resolved_ref": "main",
          "subpath":      "",
          "fetched_at":   "2026-04-21T14:30:00Z"
        },
        "caveman": {
          "source":       "local:../../bundles/caveman",
          "resolved_path": "/abs/path"
        }
      },
      "skills": { ... same shape ... }
    }
"""
from __future__ import annotations

import datetime as _dt
import json
from dataclasses import dataclass, field
from pathlib import Path


LOCK_VERSION = 1


@dataclass
class LockEntry:
    source: str
    resolved_sha: str | None = None   # git only
    resolved_ref: str | None = None   # git only
    subpath: str | None = None        # git only
    resolved_path: str | None = None  # local only
    fetched_at: str | None = None

    def to_json(self) -> dict:
        out: dict = {"source": self.source}
        for key in ("resolved_sha", "resolved_ref", "subpath",
                    "resolved_path", "fetched_at"):
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
        )


@dataclass
class Lock:
    distro: str
    bundles: dict[str, LockEntry] = field(default_factory=dict)
    skills: dict[str, LockEntry] = field(default_factory=dict)
    generated_at: str | None = None
    distro_source: str | None = None  # original URI passed to install (for `update`)

    def to_json(self) -> dict:
        out: dict = {
            "version": LOCK_VERSION,
            "distro": self.distro,
            "generated_at": self.generated_at or now_iso(),
            "bundles": {k: v.to_json() for k, v in self.bundles.items()},
            "skills": {k: v.to_json() for k, v in self.skills.items()},
        }
        if self.distro_source is not None:
            out["distro_source"] = self.distro_source
        return out

    @classmethod
    def from_json(cls, data: dict) -> "Lock":
        version = data.get("version")
        if version != LOCK_VERSION:
            raise LockError(f"unsupported lock version: {version}")
        return cls(
            distro=data.get("distro", ""),
            bundles={k: LockEntry.from_json(v) for k, v in data.get("bundles", {}).items()},
            skills={k: LockEntry.from_json(v) for k, v in data.get("skills", {}).items()},
            generated_at=data.get("generated_at"),
            distro_source=data.get("distro_source"),
        )


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

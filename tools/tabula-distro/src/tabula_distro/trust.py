"""Trust DB helper for the installer side.

The kernel reads ``$TABULA_HOME/state/trust.json`` to gate boot execution
(see ``tabula/internal/runtime/trust``). The installer needs to write the
same file: explicit ``--trust`` after a fresh install, and the cold-start
migration shim that auto-trusts an existing installation upgraded from
before issue 007.

Hash algorithm matches the Go side byte for byte:

    sorted-relative-path-newline-separated stream of
    "<rel>\0<len>\0<bytes>\n" entries, SHA256.

Files included: ``*.py`` recursively, plus top-level ``distro.toml``.
Excluded: hidden directories (``.git``, ``.venv``, …), ``__pycache__``,
non-regular files (symlinks/sockets/devices), and ``distro.toml`` outside
the top of the tree.
"""

from __future__ import annotations

import hashlib
import json
import os
import tempfile
from datetime import datetime, timezone
from pathlib import Path

from tabula_plugin_sdk import paths as sdk_paths


class TrustError(RuntimeError):
    """Raised on trust DB read/write failures."""


def trust_db_path(home: Path) -> Path:
    return home / "state" / "trust.json"


def trust_meta_path(home: Path) -> Path:
    return home / "state" / "trust.meta.json"


def hash_dir(distro_dir: Path) -> str:
    """Return SHA256 over the distro tree.

    Mirrors ``internal/runtime/trust.HashDir`` in Go. Any drift in this
    function silently invalidates every existing trust record, so changes
    here must come paired with a kernel-side update and a regen of every
    user's trust DB.
    """
    distro_dir = distro_dir.expanduser()
    if not distro_dir.is_dir():
        raise TrustError(f"distro dir does not exist: {distro_dir}")

    entries: list[str] = []
    for root, dirs, files in os.walk(distro_dir):
        # Prune walks before recursing so we never read excluded files.
        dirs[:] = [
            d for d in dirs
            if d != "__pycache__" and not d.startswith(".")
        ]
        for name in files:
            full = Path(root) / name
            if not full.is_file() or full.is_symlink():
                continue
            rel = full.relative_to(distro_dir)
            rel_posix = rel.as_posix()
            if name == "distro.toml":
                # Only the top-level distro.toml is part of the contract.
                if rel.parent == Path("."):
                    entries.append(rel_posix)
                continue
            if name.endswith(".py"):
                entries.append(rel_posix)
    entries.sort()

    h = hashlib.sha256()
    for rel in entries:
        data = (distro_dir / rel).read_bytes()
        h.update(rel.encode("utf-8"))
        h.update(b"\x00")
        h.update(str(len(data)).encode("ascii"))
        h.update(b"\x00")
        h.update(data)
        h.update(b"\n")
    return h.hexdigest()


def load(home: Path) -> dict:
    path = trust_db_path(home)
    if not path.is_file():
        return {"distros": {}}
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise TrustError(f"parse trust db {path}: {exc}") from exc
    if not isinstance(data, dict):
        raise TrustError(f"trust db {path} is not a JSON object")
    if not isinstance(data.get("distros"), dict):
        data["distros"] = {}
    return data


def save(home: Path, db: dict) -> Path:
    path = trust_db_path(home)
    path.parent.mkdir(parents=True, exist_ok=True)
    payload = json.dumps(db, indent=2, sort_keys=True) + "\n"
    fd, tmp_name = tempfile.mkstemp(
        prefix=path.name + ".",
        suffix=".tmp",
        dir=str(path.parent),
    )
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as fh:
            fh.write(payload)
            fh.flush()
            os.fsync(fh.fileno())
        os.replace(tmp_name, path)
    except BaseException:
        try:
            os.unlink(tmp_name)
        except OSError:
            pass
        raise
    return path


def approve(home: Path, distro_id: str, distro_dir: Path, *, trusted_by: str) -> dict:
    """Compute the SHA and write a trust record. Returns the record."""
    digest = hash_dir(distro_dir)
    db = load(home)
    record = {
        "boot_sha256": digest,
        "trusted_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "trusted_by": trusted_by,
    }
    db["distros"][distro_id] = record
    save(home, db)
    return record


def auto_trust_cold_start(home: Path, distro_id: str, distro_dir: Path) -> dict | None:
    """Migration shim: trust an existing install if no trust DB exists yet.

    Returns the recorded record on first run, ``None`` on subsequent runs.

    The shim is intentionally one-shot: it only fires when
    ``state/trust.json`` is absent, so a user who deliberately revoked
    trust does not get silently re-approved. After it fires we drop a
    ``state/trust.meta.json`` marker with the migration timestamp so the
    shim can be removed cleanly in a future release.

    See ``docs/issues/refactoring/007-distro-boot-trust-db.md`` § Risk and
    Migration.
    """
    if trust_db_path(home).exists():
        return None
    record = approve(home, distro_id, distro_dir, trusted_by="installer-cold-start")
    meta_path = trust_meta_path(home)
    meta_path.parent.mkdir(parents=True, exist_ok=True)
    meta_path.write_text(
        json.dumps(
            {"migrated_at": record["trusted_at"]},
            indent=2,
            sort_keys=True,
        )
        + "\n",
        encoding="utf-8",
    )
    return record


# Re-export the trust file path through the shared paths helper so distro
# tooling can reach for it the same way the kernel does. The actual file
# definition lives here because the installer needs writer semantics that
# the read-only kernel does not.
__all__ = [
    "TrustError",
    "trust_db_path",
    "trust_meta_path",
    "hash_dir",
    "load",
    "save",
    "approve",
    "auto_trust_cold_start",
]


def _consistency_check_paths_module() -> None:
    """Belt-and-braces: assert the SDK paths module agrees on the file name.

    Only invoked from tests via ``__main__`` so production startup is not
    slowed by it.
    """
    home = sdk_paths.tabula_home()
    if trust_db_path(home) != sdk_paths.trust_file():
        raise TrustError(
            f"trust_db_path mismatch: {trust_db_path(home)} vs {sdk_paths.trust_file()}",
        )

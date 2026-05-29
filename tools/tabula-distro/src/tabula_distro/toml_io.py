"""Shared TOML writer for Tabula runtime/config files.

Single source of TOML serialization for installer, app launcher, and distro
boot scripts. Uses ``tomlkit`` so user comments and key order survive
round-trips when files are merged on reinstall.

Three responsibilities:

* ``load(path)`` — read a TOML document (empty document when the file is
  missing).
* ``dump(path, doc)`` — atomic write (temp + ``os.replace``) so a crash
  cannot leave a half-written TOML file behind.
* ``merge_defaults(doc, defaults)`` — add keys that are not already in
  ``doc``. Existing keys, comments, and ordering are preserved.

All three helpers are deterministic and side-effect free except for the
explicit filesystem write in ``dump``.
"""

from __future__ import annotations

import os
import tempfile
from pathlib import Path
from typing import Any, Mapping

import tomlkit
from tomlkit import TOMLDocument
from tomlkit.items import AbstractTable, Array


def load(path: Path) -> TOMLDocument:
    """Read ``path`` as a TOML document.

    Returns an empty :class:`TOMLDocument` when the file does not exist.
    Raises :class:`OSError` on read failure and :class:`tomlkit.exceptions.
    ParseError` on malformed input — both are surfaced to the caller so the
    installer can fail loudly instead of silently overwriting a broken file.
    """
    if not path.is_file():
        return tomlkit.document()
    with path.open("r", encoding="utf-8") as fh:
        return tomlkit.parse(fh.read())


def dump(path: Path, doc: TOMLDocument) -> None:
    """Write ``doc`` to ``path`` atomically.

    The document is rendered into a sibling temp file first, ``fsync``'d, then
    renamed over the target. A crash between steps leaves either the previous
    file intact or no change at all — never a partial write.
    """
    path.parent.mkdir(parents=True, exist_ok=True)
    rendered = tomlkit.dumps(doc)
    fd, tmp_name = tempfile.mkstemp(
        prefix=path.name + ".",
        suffix=".tmp",
        dir=str(path.parent),
    )
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as fh:
            fh.write(rendered)
            fh.flush()
            os.fsync(fh.fileno())
        os.replace(tmp_name, path)
    except BaseException:
        try:
            os.unlink(tmp_name)
        except OSError:
            pass
        raise


def merge_defaults(doc: TOMLDocument, defaults: Mapping[str, Any]) -> TOMLDocument:
    """Add keys from ``defaults`` that are missing in ``doc``.

    Existing keys keep their current value, comments, and trivia. Nested
    tables are merged recursively. Lists are treated as opaque scalars —
    they are inserted when absent and left alone when present.

    Returns ``doc`` for chaining; the document is mutated in place.
    """
    _merge_into(doc, defaults)
    return doc


def assign(doc: TOMLDocument | AbstractTable, data: Mapping[str, Any]) -> TOMLDocument | AbstractTable:
    """Overwrite keys in ``doc`` with values from ``data``.

    Unlike :func:`merge_defaults`, this replaces existing keys. Keys present
    in ``doc`` but absent from ``data`` are preserved along with their
    comments. Use this for distro materializer output where the distro owns
    a known set of keys and user-added unrelated keys must survive a rerun.

    Returns ``doc`` for chaining; the document is mutated in place.
    """
    for key, value in data.items():
        doc[key] = _to_tomlkit(value)
    return doc


def _merge_into(target: AbstractTable | TOMLDocument, defaults: Mapping[str, Any]) -> None:
    for key, value in defaults.items():
        if key not in target:
            target[key] = _to_tomlkit(value)
            continue
        existing = target[key]
        if isinstance(value, Mapping) and isinstance(existing, (AbstractTable,)):
            _merge_into(existing, value)


def _to_tomlkit(value: Any) -> Any:
    if isinstance(value, Mapping):
        table = tomlkit.table()
        for key, sub in value.items():
            table[key] = _to_tomlkit(sub)
        return table
    if isinstance(value, list):
        array: Array = tomlkit.array()
        for item in value:
            array.append(_to_tomlkit(item))
        return array
    return value


# Public alias for distro boot scripts that build hand-crafted tomlkit
# documents (array-of-tables, custom layouts) and need the same scalar/list/
# table conversion logic without duplicating it.
to_tomlkit = _to_tomlkit


__all__ = ["load", "dump", "merge_defaults", "assign", "to_tomlkit"]

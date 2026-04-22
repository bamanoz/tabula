"""Minimal SemVer constraint parser (no third-party deps).

Supports a tiny subset sufficient for our distro/bundle compatibility checks:

  >=1.2.3   <2.0.0   >1.0   <=1.5.0   ==1.4.2   1.4.2

Constraints are comma-separated and joined with AND:

  ">=0.8.0,<1.0.0"

Versions must be strict ``MAJOR.MINOR.PATCH`` integers, optionally followed by
a ``-pre.release`` suffix (which we tolerate but compare lexically — good enough
for early development; we don't ship pre-releases yet).
"""
from __future__ import annotations

import re
from dataclasses import dataclass


_VERSION_RE = re.compile(r"^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$")
_OPS = ("<=", ">=", "==", "<", ">", "=")


class VersionError(ValueError):
    """Raised for unparseable versions or constraints."""


@dataclass(frozen=True, order=True)
class Version:
    major: int
    minor: int
    patch: int
    pre: str = ""

    def __str__(self) -> str:
        base = f"{self.major}.{self.minor}.{self.patch}"
        return f"{base}-{self.pre}" if self.pre else base

    @classmethod
    def parse(cls, text: str) -> "Version":
        text = text.strip().lstrip("v")
        m = _VERSION_RE.match(text)
        if not m:
            raise VersionError(f"not a valid version: {text!r}")
        return cls(int(m.group(1)), int(m.group(2)), int(m.group(3)), m.group(4) or "")


@dataclass(frozen=True)
class _Clause:
    op: str
    version: Version

    def matches(self, v: Version) -> bool:
        if self.op in ("==", "="):
            return v == self.version
        if self.op == ">":
            return v > self.version
        if self.op == ">=":
            return v >= self.version
        if self.op == "<":
            return v < self.version
        if self.op == "<=":
            return v <= self.version
        raise VersionError(f"unknown operator: {self.op!r}")


@dataclass(frozen=True)
class Constraint:
    raw: str
    clauses: tuple[_Clause, ...]

    def matches(self, v: Version | str) -> bool:
        if isinstance(v, str):
            v = Version.parse(v)
        return all(c.matches(v) for c in self.clauses)

    @classmethod
    def parse(cls, text: str) -> "Constraint":
        text = text.strip()
        if not text:
            raise VersionError("empty constraint")
        parts = [p.strip() for p in text.split(",") if p.strip()]
        clauses = tuple(_parse_clause(p) for p in parts)
        return cls(raw=text, clauses=clauses)


def _parse_clause(text: str) -> _Clause:
    for op in _OPS:
        if text.startswith(op):
            return _Clause(op=op if op != "=" else "==", version=Version.parse(text[len(op):]))
    # Bare version → exact match.
    return _Clause(op="==", version=Version.parse(text))

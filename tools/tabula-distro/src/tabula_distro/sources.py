"""Source URI parsing and resolution.

Three forms are supported:

    local:<path>                     # <path> relative to distro.toml or absolute
    git+<url>@<ref>[#path=<subdir>]  # <ref> is a tag, branch, or commit sha
    source:<alias>[#path=<subdir>]   # resolved by distro config before parsing

A ``ref`` that looks like a 7..40-character hex string is treated as a commit
sha; otherwise it is passed to ``git`` as-is (tag or branch).
"""
from __future__ import annotations

import re
import subprocess
from dataclasses import dataclass
from pathlib import Path


SHA_RE = re.compile(r"^[0-9a-f]{7,40}$")


@dataclass(frozen=True)
class LocalSource:
    path: Path  # absolute, resolved

    def kind(self) -> str:
        return "local"


@dataclass(frozen=True)
class GitSource:
    url: str
    ref: str            # tag / branch / sha as written
    subpath: str = ""   # optional '#path=' subdir
    pinned_sha: bool = False  # True when ref looks like a sha

    def kind(self) -> str:
        return "git"


Source = LocalSource | GitSource


class SourceError(ValueError):
    """Raised for malformed source URIs or resolution failures."""


def parse(uri: str, *, base_dir: Path) -> Source:
    if uri.startswith("local:"):
        body, subpath = split_fragment(uri)
        raw = body[len("local:"):]
        if not raw:
            raise SourceError(f"empty local: path in {uri!r}")
        path = Path(raw)
        if not path.is_absolute():
            path = (base_dir / path)
        if subpath:
            path = path / subpath
        return LocalSource(path=path.resolve())

    if uri.startswith("git+"):
        body = uri[len("git+"):]
        subpath = ""
        if "#" in body:
            body, frag = body.split("#", 1)
            for part in frag.split("&"):
                if part.startswith("path="):
                    subpath = part[len("path="):].strip("/")
                else:
                    raise SourceError(f"unknown fragment in git source: {part!r}")
        if "@" not in body:
            raise SourceError(f"git source missing @ref: {uri!r}")
        url, _, ref = body.rpartition("@")
        if not url or not ref:
            raise SourceError(f"git source must be git+<url>@<ref>: {uri!r}")
        return GitSource(
            url=url,
            ref=ref,
            subpath=subpath,
            pinned_sha=bool(SHA_RE.fullmatch(ref)),
        )

    raise SourceError(f"unsupported source URI: {uri!r}")


def split_fragment(uri: str) -> tuple[str, str]:
    """Return (source_without_fragment, optional path fragment)."""

    if "#" not in uri:
        return uri, ""
    body, frag = uri.split("#", 1)
    subpath = ""
    for part in frag.split("&"):
        if part.startswith("path="):
            subpath = part[len("path="):].strip("/")
        else:
            raise SourceError(f"unknown fragment in source alias: {part!r}")
    return body, subpath


def join_path_fragment(base_uri: str, subpath: str) -> str:
    """Append or combine a #path fragment on a source URI."""

    if not subpath:
        return base_uri
    body, base_subpath = split_fragment(base_uri)
    combined = "/".join(part.strip("/") for part in (base_subpath, subpath) if part.strip("/"))
    return f"{body}#path={combined}"


def git(*args: str, cwd: Path | None = None, check: bool = True) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["git", *args],
        cwd=str(cwd) if cwd else None,
        check=check,
        capture_output=True,
        text=True,
    )


def resolve_sha(src: GitSource, repo_dir: Path) -> str:
    """Given a prepared git checkout at ``repo_dir``, return its HEAD sha."""
    out = git("rev-parse", "HEAD", cwd=repo_dir).stdout.strip()
    if not SHA_RE.fullmatch(out):
        raise SourceError(f"unexpected rev-parse output: {out!r}")
    return out

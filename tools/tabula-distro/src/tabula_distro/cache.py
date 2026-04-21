"""Content-addressed cache for git sources.

Layout under ``$TABULA_HOME/cache/``::

    git/
      <url-sha1>/
        repo.git/        # bare clone, fetched on demand
        worktrees/
          <commit-sha>/  # checked-out tree at that sha (read-only by convention)
        url.txt          # the original URL, for gc/diagnostics

Resolution strategy:

  1. Ensure ``repo.git`` exists (clone --bare on first use).
  2. Ensure ``<commit-sha>`` worktree exists; if ref is symbolic (tag/branch),
     fetch from origin and resolve the sha first.
  3. Return ``worktrees/<sha>``.

Worktrees use ``git worktree add --detach`` so that multiple commits of the
same repo can coexist cheaply.
"""
from __future__ import annotations

import hashlib
import shutil
from dataclasses import dataclass
from pathlib import Path

from .sources import GitSource, SourceError, git


@dataclass(frozen=True)
class GitCheckout:
    sha: str
    worktree: Path


class GitCache:
    def __init__(self, root: Path):
        self.root = root
        self.git_root = root / "git"

    def repo_dir(self, url: str) -> Path:
        h = hashlib.sha1(url.encode("utf-8")).hexdigest()
        return self.git_root / h

    def fetch(self, src: GitSource, *, offline: bool = False) -> GitCheckout:
        repo_root = self.repo_dir(src.url)
        bare = repo_root / "repo.git"
        worktrees = repo_root / "worktrees"
        repo_root.mkdir(parents=True, exist_ok=True)
        worktrees.mkdir(parents=True, exist_ok=True)
        url_marker = repo_root / "url.txt"
        if not url_marker.exists():
            url_marker.write_text(src.url + "\n", encoding="utf-8")

        if not bare.exists():
            if offline:
                raise SourceError(f"cache miss for {src.url} and --frozen/--offline set")
            git("clone", "--bare", "--filter=blob:none", src.url, str(bare))

        sha = self._resolve_ref(bare, src, offline=offline)
        worktree = worktrees / sha
        if not worktree.exists():
            git("worktree", "add", "--detach", str(worktree), sha, cwd=bare)
        return GitCheckout(sha=sha, worktree=worktree)

    def _resolve_ref(self, bare: Path, src: GitSource, *, offline: bool) -> str:
        if src.pinned_sha:
            # Try to resolve locally first.
            res = git("cat-file", "-e", src.ref, cwd=bare, check=False)
            if res.returncode == 0:
                return git("rev-parse", src.ref, cwd=bare).stdout.strip()
            if offline:
                raise SourceError(f"sha {src.ref} not in cache for {src.url}")
            git("fetch", "origin", src.ref, cwd=bare, check=False)
            return git("rev-parse", src.ref, cwd=bare).stdout.strip()

        if not offline:
            git("fetch", "--tags", "--force", "origin",
                f"+refs/heads/*:refs/remotes/origin/*", cwd=bare, check=False)
        # Try tag, then remote branch, then literal.
        for candidate in (f"refs/tags/{src.ref}", f"refs/remotes/origin/{src.ref}", src.ref):
            res = git("rev-parse", "--verify", candidate, cwd=bare, check=False)
            if res.returncode == 0:
                return res.stdout.strip()
        raise SourceError(f"cannot resolve git ref {src.ref!r} in {src.url}")

    def gc(self, keep_shas: set[str]) -> list[Path]:
        """Remove worktrees not in ``keep_shas``. Returns deleted paths."""
        removed: list[Path] = []
        if not self.git_root.exists():
            return removed
        for repo_root in self.git_root.iterdir():
            wt_dir = repo_root / "worktrees"
            if not wt_dir.exists():
                continue
            for wt in wt_dir.iterdir():
                if wt.name in keep_shas:
                    continue
                bare = repo_root / "repo.git"
                if bare.exists():
                    git("worktree", "remove", "--force", str(wt), cwd=bare, check=False)
                if wt.exists():
                    shutil.rmtree(wt, ignore_errors=True)
                removed.append(wt)
        return removed

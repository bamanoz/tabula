"""Cross-process exclusive file locks."""
from __future__ import annotations

import contextlib
import sys
from collections.abc import Iterator
from pathlib import Path

if sys.platform == "win32":
    import msvcrt
else:
    import fcntl


@contextlib.contextmanager
def exclusive(path: Path) -> Iterator[None]:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a+b") as handle:
        if sys.platform == "win32":
            handle.seek(0)
            if handle.read(1) == b"":
                handle.write(b"\0")
                handle.flush()
            handle.seek(0)
            msvcrt.locking(handle.fileno(), msvcrt.LK_LOCK, 1)
        else:
            fcntl.flock(handle, fcntl.LOCK_EX)
        try:
            yield
        finally:
            if sys.platform == "win32":
                handle.seek(0)
                with contextlib.suppress(OSError):
                    msvcrt.locking(handle.fileno(), msvcrt.LK_UNLCK, 1)
            else:
                fcntl.flock(handle, fcntl.LOCK_UN)

"""
Cross-platform file locking.

Uses fcntl.flock on Unix/macOS and msvcrt.locking on Windows.
"""

import sys

if sys.platform == "win32":
    import msvcrt

    def lock_file(f):
        """Acquire an exclusive lock on the file."""
        msvcrt.locking(f.fileno(), msvcrt.LK_LOCK, 1)

    def unlock_file(f):
        """Release the lock on the file."""
        try:
            msvcrt.locking(f.fileno(), msvcrt.LK_UNLCK, 1)
        except OSError:
            pass
else:
    import fcntl

    def lock_file(f):
        """Acquire an exclusive lock on the file."""
        fcntl.flock(f, fcntl.LOCK_EX)

    def unlock_file(f):
        """Release the lock on the file."""
        fcntl.flock(f, fcntl.LOCK_UN)

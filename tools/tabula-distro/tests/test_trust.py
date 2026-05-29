"""Tests for the installer-side trust DB helper."""

from __future__ import annotations

import json
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

from tabula_distro import trust


def _make_distro(root: Path) -> Path:
    distro_dir = root / "code-immune"
    distro_dir.mkdir(parents=True)
    (distro_dir / "boot.py").write_text("print('hello')\n", encoding="utf-8")
    (distro_dir / "distro.toml").write_text("id = 'code-immune'\n", encoding="utf-8")
    (distro_dir / "application").mkdir()
    (distro_dir / "application" / "main.py").write_text("x = 1\n", encoding="utf-8")
    (distro_dir / "README.md").write_text("docs\n", encoding="utf-8")
    pycache = distro_dir / "__pycache__"
    pycache.mkdir()
    (pycache / "boot.cpython-313.pyc").write_text("skip\n", encoding="utf-8")
    return distro_dir


class HashDirTests(unittest.TestCase):
    def test_deterministic(self) -> None:
        with TemporaryDirectory() as tmp:
            dir_ = _make_distro(Path(tmp))
            self.assertEqual(trust.hash_dir(dir_), trust.hash_dir(dir_))

    def test_boot_change_invalidates(self) -> None:
        with TemporaryDirectory() as tmp:
            dir_ = _make_distro(Path(tmp))
            before = trust.hash_dir(dir_)
            (dir_ / "boot.py").write_text("print('changed')\n", encoding="utf-8")
            self.assertNotEqual(before, trust.hash_dir(dir_))

    def test_nested_py_change_invalidates(self) -> None:
        with TemporaryDirectory() as tmp:
            dir_ = _make_distro(Path(tmp))
            before = trust.hash_dir(dir_)
            (dir_ / "application" / "main.py").write_text("x = 2\n", encoding="utf-8")
            self.assertNotEqual(before, trust.hash_dir(dir_))

    def test_distro_toml_change_invalidates(self) -> None:
        with TemporaryDirectory() as tmp:
            dir_ = _make_distro(Path(tmp))
            before = trust.hash_dir(dir_)
            (dir_ / "distro.toml").write_text("id = 'x'\nfoo = 1\n", encoding="utf-8")
            self.assertNotEqual(before, trust.hash_dir(dir_))

    def test_readme_change_does_not_invalidate(self) -> None:
        with TemporaryDirectory() as tmp:
            dir_ = _make_distro(Path(tmp))
            before = trust.hash_dir(dir_)
            (dir_ / "README.md").write_text("completely different\n", encoding="utf-8")
            (dir_ / "__pycache__" / "boot.cpython-313.pyc").write_text("noise\n", encoding="utf-8")
            self.assertEqual(before, trust.hash_dir(dir_))


class HashDirCrossLanguageTests(unittest.TestCase):
    """Pinned digest cross-check: Python must match a constant the Go side
    also asserts on the same fixture.

    Both sides hash the deterministic fixture in ``_make_distro``. The
    constant lives below; ``internal/runtime/trust/trust_test.go``
    ``TestHashDir_PinnedFixtureMatchesPython`` asserts the same value.
    Any drift in either implementation breaks one of the two tests.
    """

    EXPECTED_SHA256 = (
        # Matches Go: trust.HashDir(_make_distro(tmp))
        # If you change _make_distro or the hashing algorithm, regenerate
        # this constant AND the matching Go constant in trust_test.go
        # together — drift between sides silently breaks every user's
        # trust DB.
        "05c8091020bb6dd91bcadd486ab5abe23f2fadce3bcb8d3bb7ec54ebbdfd3498"
    )

    def test_pinned_digest(self) -> None:
        with TemporaryDirectory() as tmp:
            dir_ = _make_distro(Path(tmp))
            actual = trust.hash_dir(dir_)
            self.assertEqual(actual, self.EXPECTED_SHA256)


class TrustDBTests(unittest.TestCase):
    def test_load_missing_returns_empty(self) -> None:
        with TemporaryDirectory() as tmp:
            home = Path(tmp)
            db = trust.load(home)
            self.assertEqual(db, {"distros": {}})

    def test_approve_then_load_roundtrips(self) -> None:
        with TemporaryDirectory() as tmp:
            home = Path(tmp)
            dir_ = _make_distro(home)
            record = trust.approve(home, "code-immune", dir_, trusted_by="user")
            self.assertIn("boot_sha256", record)
            self.assertEqual(record["trusted_by"], "user")
            db = trust.load(home)
            self.assertEqual(db["distros"]["code-immune"], record)

    def test_save_writes_pretty_json(self) -> None:
        with TemporaryDirectory() as tmp:
            home = Path(tmp)
            db = {"distros": {"x": {"boot_sha256": "h", "trusted_at": "t", "trusted_by": "u"}}}
            trust.save(home, db)
            data = json.loads(trust.trust_db_path(home).read_text())
            self.assertEqual(data, db)

    def test_auto_trust_cold_start_only_fires_once(self) -> None:
        with TemporaryDirectory() as tmp:
            home = Path(tmp)
            dir_ = _make_distro(home)
            first = trust.auto_trust_cold_start(home, "code-immune", dir_)
            self.assertIsNotNone(first)
            self.assertTrue(trust.trust_meta_path(home).exists())
            meta = json.loads(trust.trust_meta_path(home).read_text())
            self.assertIn("migrated_at", meta)

            # Second call must be a no-op now that trust.json exists.
            second = trust.auto_trust_cold_start(home, "code-immune", dir_)
            self.assertIsNone(second)

    def test_load_corrupt_raises(self) -> None:
        with TemporaryDirectory() as tmp:
            home = Path(tmp)
            (home / "state").mkdir()
            trust.trust_db_path(home).write_text("not json", encoding="utf-8")
            with self.assertRaises(trust.TrustError):
                trust.load(home)


if __name__ == "__main__":
    unittest.main()

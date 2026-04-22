"""End-to-end tests for tabula_distro.install using local: sources only."""
from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from tabula_distro import config as cfg
from tabula_distro import generations as gens
from tabula_distro import install as installmod
from tabula_distro import lock as lockmod
from tabula_distro import sources as srcmod


def _touch(p: Path, content: str = "") -> None:
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(content, encoding="utf-8")


def _make_skill(root: Path, name: str, marker: str = "v1") -> Path:
    d = root / name
    d.mkdir(parents=True, exist_ok=True)
    _touch(d / "SKILL.md", f"# {name}\n")
    _touch(d / "marker.txt", marker)
    return d


def _make_minimal_distro(root: Path, name: str = "demo") -> Path:
    dist = root / name
    _touch(dist / "boot.py", "# boot\n")
    (dist / "templates").mkdir(parents=True, exist_ok=True)
    (dist / "skills").mkdir(parents=True, exist_ok=True)
    _touch(dist / "templates" / "SYSTEM.md", "hello\n")
    return dist


class URITests(unittest.TestCase):
    def test_local_relative(self):
        src = srcmod.parse("local:../x", base_dir=Path("/a/b/c"))
        self.assertIsInstance(src, srcmod.LocalSource)
        self.assertEqual(src.path, Path("/a/b/x").resolve())

    def test_git_with_ref(self):
        src = srcmod.parse("git+https://e.com/r.git@main", base_dir=Path("/"))
        self.assertIsInstance(src, srcmod.GitSource)
        self.assertEqual(src.url, "https://e.com/r.git")
        self.assertEqual(src.ref, "main")
        self.assertFalse(src.pinned_sha)

    def test_git_sha_pin(self):
        src = srcmod.parse("git+https://e.com/r.git@abc1234", base_dir=Path("/"))
        self.assertTrue(isinstance(src, srcmod.GitSource) and src.pinned_sha)

    def test_git_subpath(self):
        src = srcmod.parse("git+https://e.com/r.git@v1#path=bundle/x", base_dir=Path("/"))
        assert isinstance(src, srcmod.GitSource)
        self.assertEqual(src.subpath, "bundle/x")

    def test_bad_uri(self):
        with self.assertRaises(srcmod.SourceError):
            srcmod.parse("http://e.com", base_dir=Path("/"))

    def test_git_requires_ref(self):
        with self.assertRaises(srcmod.SourceError):
            srcmod.parse("git+https://e.com/r.git", base_dir=Path("/"))


class ConfigTests(unittest.TestCase):
    def test_absent_config_is_empty(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "d"
            root.mkdir()
            c = cfg.load(root)
            self.assertEqual(c.bundles, ())
            self.assertEqual(c.skills, ())

    def test_override_replaces_entry_by_name(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "d"
            root.mkdir()
            (root / "distro.toml").write_text(
                "[distro]\nname=\"x\"\n"
                "[[bundles]]\nname=\"m\"\nsource=\"local:./a\"\n",
                encoding="utf-8",
            )
            (root / "distro.override.toml").write_text(
                "[[bundles]]\nname=\"m\"\nsource=\"local:./b\"\n",
                encoding="utf-8",
            )
            c = cfg.load(root)
            self.assertEqual(len(c.bundles), 1)
            self.assertEqual(c.bundles[0].source, "local:./b")


class InstallTests(unittest.TestCase):
    def test_install_with_local_bundle_and_skill(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            # bundle with two skills + support dir
            bundle = root / "ext" / "bundles" / "memory"
            _make_skill(bundle, "memory-save", "save-v1")
            _make_skill(bundle, "memory-search", "search-v1")
            (bundle / "_memory").mkdir(parents=True)
            _touch(bundle / "_memory" / "lib.py", "X=1\n")

            # standalone external skill
            ext_skill = root / "ext" / "skills" / "weather"
            _make_skill(ext_skill.parent, "weather", "weather-v1")

            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n\n'
                '[[bundles]]\nname="memory"\nsource="local:../ext/bundles/memory"\n\n'
                '[[skills]]\nname="weather"\nsource="local:../ext/skills/weather"\n',
                encoding="utf-8",
            )

            gen, lock = installmod.install(distro, home)
            self.assertEqual(gen.number, 1)

            cur = home / "distrib" / "demo" / "current"
            self.assertTrue(cur.is_symlink())

            for p in (
                home / "distrib" / "demo" / "skills" / "memory-save" / "marker.txt",
                home / "distrib" / "demo" / "skills" / "memory-search" / "marker.txt",
                home / "distrib" / "demo" / "skills" / "_memory" / "lib.py",
                home / "distrib" / "demo" / "skills" / "weather" / "marker.txt",
            ):
                self.assertTrue(p.exists(), p)

            self.assertTrue((home / "boot.py").is_symlink())
            self.assertTrue((home / "distrib" / "active").is_symlink())

            self.assertIn("memory", lock.bundles)
            self.assertIn("weather", lock.skills)

    def test_conflict_without_override_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root)

            # in-tree skill named `files`
            _make_skill(distro / "skills", "files", "in-tree")

            # external provides the same name
            ext = root / "ext" / "skills" / "files"
            _make_skill(ext.parent, "files", "ext")
            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n'
                '[[skills]]\nname="files"\nsource="local:../ext/skills/files"\n',
                encoding="utf-8",
            )
            with self.assertRaises(installmod.InstallError):
                installmod.install(distro, home)

    def test_conflict_with_override_wins(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root)
            _make_skill(distro / "skills", "files", "in-tree")
            ext = root / "ext" / "skills" / "files"
            _make_skill(ext.parent, "files", "ext")
            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n'
                '[[skills]]\nname="files"\nsource="local:../ext/skills/files"\n'
                'override=true\n',
                encoding="utf-8",
            )
            installmod.install(distro, home)
            marker = home / "distrib" / "demo" / "skills" / "files" / "marker.txt"
            self.assertEqual(marker.read_text(encoding="utf-8"), "ext")

    def test_second_install_creates_new_generation(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root)
            installmod.install(distro, home)
            # Mutate the distro tree so the second install is not a no-op.
            (distro / "skills" / "extra").mkdir()
            (distro / "skills" / "extra" / "SKILL.md").write_text("# extra\n", encoding="utf-8")
            installmod.install(distro, home)
            gs = gens.list_generations(home, "demo")
            self.assertEqual([g.number for g in gs], [1, 2])
            self.assertEqual(gens.current_generation(home, "demo").number, 2)

    def test_identical_reinstall_reuses_generation(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root)
            gen1, _ = installmod.install(distro, home)
            gen2, _ = installmod.install(distro, home)
            self.assertEqual(gen1.number, gen2.number)
            gs = gens.list_generations(home, "demo")
            self.assertEqual([g.number for g in gs], [1])

    def test_rollback(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root)
            installmod.install(distro, home)
            (distro / "skills" / "extra").mkdir()
            (distro / "skills" / "extra" / "SKILL.md").write_text("# extra\n", encoding="utf-8")
            installmod.install(distro, home)
            target = installmod.rollback(home, "demo")
            self.assertEqual(target.number, 1)
            self.assertEqual(gens.current_generation(home, "demo").number, 1)

    def test_lockfile_written_and_readable(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root)
            ext = root / "ext" / "skills" / "foo"
            _make_skill(ext.parent, "foo")
            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n'
                '[[skills]]\nname="foo"\nsource="local:../ext/skills/foo"\n',
                encoding="utf-8",
            )
            installmod.install(distro, home)
            lock_path = home / "distrib" / "demo" / "distro.lock.json"
            lock = lockmod.load(lock_path)
            assert lock is not None
            self.assertIn("foo", lock.skills)
            self.assertIsNotNone(lock.skills["foo"].resolved_path)

    def test_symlinked_in_tree_skill_is_materialized(self):
        """Legacy dev pattern: distrib/<d>/skills/<x> is a symlink to repo skills dir."""
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root)
            shared = root / "shared" / "gateway-cli"
            _make_skill(shared.parent, "gateway-cli", "shared")
            (distro / "skills" / "gateway-cli").symlink_to(shared)
            installmod.install(distro, home)
            self.assertTrue((home / "distrib" / "demo" / "skills" / "gateway-cli" / "marker.txt").exists())


if __name__ == "__main__":
    unittest.main()

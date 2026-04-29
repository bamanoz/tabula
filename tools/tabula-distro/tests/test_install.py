"""End-to-end tests for tabula_distro.install using local: sources only."""
from __future__ import annotations

import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path

from tabula_distro import config as cfg
from tabula_distro import cli as climod
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
    _touch(d / "SKILL.md", _skill_manifest(name))
    _touch(d / "marker.txt", marker)
    return d


def _make_skill_with_manifest(root: Path, name: str, manifest: str, marker: str = "v1") -> Path:
    d = root / name
    d.mkdir(parents=True, exist_ok=True)
    _touch(d / "SKILL.md", manifest)
    _touch(d / "marker.txt", marker)
    return d


def _skill_manifest(name: str, exec_cmd: str | None = None) -> str:
    tool_name = name.replace("-", "_")
    exec_cmd = exec_cmd or f"python skills/{name}/run.py"
    return (
        "---\n"
        f"name: {name}\n"
        "tools:\n"
        f"  - name: {tool_name}\n"
        f"    description: Run {name}\n"
        f"    exec: {exec_cmd}\n"
        "---\n"
        f"# {name}\n"
    )


def _make_plugin(root: Path, name: str, marker: str = "v1") -> Path:
    d = root / name
    d.mkdir(parents=True, exist_ok=True)
    _touch(d / "plugin.toml", (
        f'id = "{name}"\n'
        f'name = "{name}"\n'
        'version = "0.1.0"\n'
        'runtime = "python"\n'
        'entry = "run.py"\n'
    ))
    _touch(d / "run.py", "# plugin\n")
    _touch(d / "marker.txt", marker)
    return d


def _make_client(root: Path, name: str, marker: str = "v1") -> Path:
    d = root / name
    d.mkdir(parents=True, exist_ok=True)
    _touch(d / "client.toml", (
        f'id = "{name}"\n'
        f'name = "{name}"\n'
        'version = "0.1.0"\n'
        'runtime = "python"\n'
        'entry = "run.py"\n'
    ))
    _touch(d / "run.py", "# client\n")
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
            self.assertEqual(c.plugins, ())

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

    def test_bundle_components_replaces_legacy_skills_allowlist(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "d"
            root.mkdir()
            (root / "distro.toml").write_text(
                "[[bundles]]\nname=\"m\"\nsource=\"local:./a\"\ncomponents=[\"plugin-a\"]\n",
                encoding="utf-8",
            )
            c = cfg.load(root)
            self.assertEqual(c.bundles[0].components, ("plugin-a",))
            self.assertEqual(c.bundles[0].skills, ("plugin-a",))

    def test_plugin_config_entry(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "d"
            root.mkdir()
            (root / "distro.toml").write_text(
                "[[plugins]]\nname=\"hello\"\nsource=\"local:./plugins/hello\"\n",
                encoding="utf-8",
            )
            c = cfg.load(root)
            self.assertEqual(len(c.plugins), 1)
            self.assertEqual(c.plugins[0].name, "hello")

    def test_source_alias_override_merges_by_alias_name(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "d"
            root.mkdir()
            (root / "distro.toml").write_text(
                '[sources.tabula-bundles]\nsource="git+https://example.invalid/bundles.git@main"\n'
                '[[bundles]]\nname="base"\nsource="source:tabula-bundles#path=base"\n',
                encoding="utf-8",
            )
            (root / "distro.override.toml").write_text(
                '[sources.tabula-bundles]\nsource="local:/tmp/tabula-bundles"\n',
                encoding="utf-8",
            )
            c = cfg.load(root)
            self.assertEqual(c.sources["tabula-bundles"].source, "local:/tmp/tabula-bundles")
            self.assertEqual(c.bundles[0].source, "source:tabula-bundles#path=base")


class InstallTests(unittest.TestCase):
    def _with_fake_git_cache(self, mapping: dict[str, Path], fn):
        original = installmod.GitCache

        class FakeCheckout:
            def __init__(self, sha: str, worktree: Path):
                self.sha = sha
                self.worktree = worktree

        class FakeGitCache:
            current_sha = next(iter(mapping))

            def __init__(self, _root: Path):
                pass

            def fetch(self, src, *, offline: bool = False):
                sha = src.ref if src.pinned_sha else FakeGitCache.current_sha
                return FakeCheckout(sha, mapping[sha])

        installmod.GitCache = FakeGitCache
        try:
            return fn(FakeGitCache)
        finally:
            installmod.GitCache = original

    def test_install_with_local_bundle_and_skill(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            # bundle with two skills + packaged support library
            bundle = root / "ext" / "bundles" / "memory"
            _make_skill(bundle, "memory-save", "save-v1")
            _make_skill(bundle, "memory-search", "search-v1")
            _touch(bundle / "_lib" / "python" / "src" / "pkg" / "__init__.py", "X=1\n")

            # standalone external skill
            ext_skill = root / "ext" / "skills" / "weather"
            _make_skill(ext_skill.parent, "weather", "weather-v1")

            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n\n'
                '[[bundles]]\nname="memory"\nsource="local:../ext/bundles/memory"\n\n'
                '[[skills]]\nname="weather"\nsource="local:../ext/skills/weather"\n',
                encoding="utf-8",
            )

            result = installmod.install(distro, home)
            gen, lock = result
            self.assertEqual(gen.number, 1)
            self.assertTrue(result.changed)

            cur = home / "distrib" / "demo" / "current"
            self.assertTrue(cur.is_symlink())

            for p in (
                home / "distrib" / "demo" / "skills" / "memory-save" / "marker.txt",
                home / "distrib" / "demo" / "skills" / "memory-search" / "marker.txt",
                home / "distrib" / "demo" / "skills" / "weather" / "marker.txt",
            ):
                self.assertTrue(p.exists(), p)
            self.assertTrue((home / "distrib" / "demo" / "_lib" / "python" / "src" / "pkg" / "__init__.py").exists())
            self.assertTrue((home / "_lib" / "python" / "src" / "pkg" / "__init__.py").exists())

            self.assertTrue((home / "boot.py").is_symlink())
            self.assertTrue((home / "distrib" / "active").is_symlink())

            self.assertIn("memory", lock.bundles)
            self.assertIn("weather", lock.skills)

    def test_source_alias_installs_multiple_bundles_with_shared_lib_once(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            repo = root / "tabula-bundles"
            _make_skill(repo / "base", "shell", "shell-v1")
            _make_skill(repo / "caveman", "caveman-compress", "caveman-v1")
            _touch(repo / "_lib" / "python" / "src" / "shared" / "__init__.py", "X=1\n")
            _touch(repo / "base" / "bundle.toml", '[bundle]\nname="base"\n')
            _touch(repo / "caveman" / "bundle.toml", '[bundle]\nname="caveman"\n')

            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n'
                '[sources.tabula-bundles]\nsource="local:../tabula-bundles"\n'
                '[[bundles]]\nname="base"\nsource="source:tabula-bundles#path=base"\n'
                '[[bundles]]\nname="caveman"\nsource="source:tabula-bundles#path=caveman"\n',
                encoding="utf-8",
            )

            _gen, lock = installmod.install(distro, home)
            self.assertTrue((home / "skills" / "shell" / "marker.txt").exists())
            self.assertTrue((home / "skills" / "caveman-compress" / "marker.txt").exists())
            self.assertTrue((home / "_lib" / "python" / "src" / "shared" / "__init__.py").exists())
            self.assertEqual(lock.bundles["base"].source, "local:../tabula-bundles#path=base")
            self.assertEqual(lock.bundles["caveman"].source, "local:../tabula-bundles#path=caveman")
            self.assertEqual(lock.bundles["base"].resolved_path, str((repo / "base").resolve()))

    def test_mixed_bundle_sources_with_identical_shared_lib_pass(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            local_repo = root / "local-repo"
            git_repo = root / "git-repo"
            _make_skill(local_repo / "base", "shell", "shell-v1")
            _make_skill(git_repo / "caveman", "caveman-compress", "caveman-v1")
            _touch(local_repo / "_lib" / "python" / "src" / "shared" / "__init__.py", "X=1\n")
            _touch(git_repo / "_lib" / "python" / "src" / "shared" / "__init__.py", "X=1\n")

            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n'
                '[[bundles]]\nname="base"\nsource="local:../local-repo/base"\n'
                '[[bundles]]\nname="caveman"\nsource="local:../git-repo/caveman"\n',
                encoding="utf-8",
            )

            installmod.install(distro, home)
            self.assertEqual(
                (home / "_lib" / "python" / "src" / "shared" / "__init__.py").read_text(encoding="utf-8"),
                "X=1\n",
            )
            self.assertTrue((home / "skills" / "shell" / "marker.txt").exists())
            self.assertTrue((home / "skills" / "caveman-compress" / "marker.txt").exists())

    def test_mixed_bundle_sources_with_different_shared_lib_fail_clearly(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            local_repo = root / "local-repo"
            git_repo = root / "git-repo"
            _make_skill(local_repo / "base", "shell", "shell-v1")
            _make_skill(git_repo / "caveman", "caveman-compress", "caveman-v1")
            _touch(local_repo / "_lib" / "python" / "src" / "shared" / "__init__.py", "X=1\n")
            _touch(git_repo / "_lib" / "python" / "src" / "shared" / "__init__.py", "X=2\n")

            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n'
                '[[bundles]]\nname="base"\nsource="local:../local-repo/base"\n'
                '[[bundles]]\nname="caveman"\nsource="local:../git-repo/caveman"\n'
                'override=true\n',
                encoding="utf-8",
            )

            with self.assertRaises(installmod.InstallError) as cm:
                installmod.install(distro, home)
            message = str(cm.exception)
            self.assertIn("shared lib _lib/python differs between bundle sources", message)
            self.assertIn("bundle: base", message)
            self.assertIn("bundle: caveman", message)
            self.assertIn("hash: sha256:", message)
            self.assertNotIn("set override = true", message)

    def test_bundle_install_mixed_skills_and_plugins(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            bundle = root / "ext" / "bundles" / "mixed"
            _make_skill(bundle, "base-shell", "shell-v1")
            _make_plugin(bundle, "hook-permissions", "hook-v1")

            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n\n'
                '[[bundles]]\nname="mixed"\nsource="local:../ext/bundles/mixed"\n',
                encoding="utf-8",
            )

            _gen, lock = installmod.install(distro, home)
            self.assertTrue((home / "distrib" / "demo" / "skills" / "base-shell" / "SKILL.md").exists())
            self.assertTrue((home / "distrib" / "demo" / "plugins" / "hook-permissions" / "plugin.toml").exists())
            self.assertTrue((home / "plugins" / "hook-permissions" / "plugin.toml").exists())
            self.assertIn("base-shell", lock.skills)
            self.assertIn("hook-permissions", lock.plugins)

    def test_bundle_install_clients(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            bundle = root / "ext" / "bundles" / "clients"
            _make_client(bundle, "driver", "driver-v1")
            _make_client(bundle, "subagent", "subagent-v1")

            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n\n'
                '[[bundles]]\nname="clients"\nsource="local:../ext/bundles/clients"\n',
                encoding="utf-8",
            )

            _gen, lock = installmod.install(distro, home)
            self.assertTrue((home / "distrib" / "demo" / "clients" / "driver" / "client.toml").exists())
            self.assertTrue((home / "clients" / "driver" / "run.py").exists())
            self.assertFalse((home / "skills" / "driver").exists())
            self.assertIn("driver", lock.clients)
            self.assertIn("subagent", lock.clients)

    def test_client_manifest_is_validated(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")
            bundle = root / "ext" / "bundles" / "clients"
            bad = bundle / "driver"
            bad.mkdir(parents=True)
            _touch(bad / "client.toml", 'id="wrong"\nruntime="python"\nentry="run.py"\n')
            _touch(bad / "run.py", "# client\n")
            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n\n'
                '[[bundles]]\nname="clients"\nsource="local:../ext/bundles/clients"\n',
                encoding="utf-8",
            )
            with self.assertRaises(installmod.InstallError) as cm:
                installmod.install(distro, home)
            self.assertIn("must match directory name", str(cm.exception))

    def test_client_manifest_entry_must_stay_inside_component(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")
            bundle = root / "ext" / "bundles" / "clients"
            bad = bundle / "driver"
            bad.mkdir(parents=True)
            _touch(bad / "client.toml", 'id="driver"\nruntime="python"\nentry="../run.py"\n')
            _touch(bundle / "run.py", "# outside\n")
            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n\n'
                '[[bundles]]\nname="clients"\nsource="local:../ext/bundles/clients"\n',
                encoding="utf-8",
            )
            with self.assertRaises(installmod.InstallError) as cm:
                installmod.install(distro, home)
            self.assertIn("entry escapes", str(cm.exception))

    def test_install_with_standalone_plugin_entry(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")
            _make_plugin(root / "ext" / "plugins", "hello", "hello-v1")
            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n'
                '[[plugins]]\nname="hello"\nsource="local:../ext/plugins/hello"\n',
                encoding="utf-8",
            )

            _gen, lock = installmod.install(distro, home)
            self.assertTrue((home / "distrib" / "demo" / "plugins" / "hello" / "plugin.toml").exists())
            self.assertTrue((home / "plugins" / "hello" / "plugin.toml").exists())
            self.assertIn("hello", lock.plugins)

    def test_bundle_toml_with_explicit_components(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            bundle = root / "ext" / "bundles" / "explicit"
            _make_plugin(bundle, "plugin-a")
            _make_skill(bundle, "skill-b")
            _make_skill(bundle, "skill-c")
            _touch(bundle / "bundle.toml", (
                '[bundle]\nname="explicit"\nversion="0.1.0"\n'
                'components=["plugin-a", "skill-b"]\n'
            ))
            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n'
                '[[bundles]]\nname="explicit"\nsource="local:../ext/bundles/explicit"\n',
                encoding="utf-8",
            )

            _gen, lock = installmod.install(distro, home)
            self.assertTrue((home / "distrib" / "demo" / "plugins" / "plugin-a" / "plugin.toml").exists())
            self.assertTrue((home / "distrib" / "demo" / "skills" / "skill-b" / "SKILL.md").exists())
            self.assertFalse((home / "distrib" / "demo" / "skills" / "skill-c").exists())
            self.assertIn("plugin-a", lock.plugins)
            self.assertIn("skill-b", lock.skills)
            self.assertNotIn("skill-c", lock.skills)
            self.assertEqual(lock.bundles["explicit"].version, "0.1.0")

    def test_bundle_toml_legacy_compat_no_components(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            bundle = root / "ext" / "bundles" / "legacy"
            _make_skill(bundle, "skill-a")
            _make_plugin(bundle, "plugin-a")
            _touch(bundle / "bundle.toml", '[bundle]\nname="legacy"\nversion="0.1.0"\n')
            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n'
                '[[bundles]]\nname="legacy"\nsource="local:../ext/bundles/legacy"\n',
                encoding="utf-8",
            )

            _gen, lock = installmod.install(distro, home)
            self.assertTrue((home / "distrib" / "demo" / "skills" / "skill-a" / "SKILL.md").exists())
            self.assertTrue((home / "distrib" / "demo" / "plugins" / "plugin-a" / "plugin.toml").exists())
            self.assertIn("skill-a", lock.skills)
            self.assertIn("plugin-a", lock.plugins)

    def test_update_only_plugin_name_refreshes_standalone_plugin_git_source(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            sha_v1 = "a" * 40
            sha_v2 = "b" * 40
            plugin_v1 = _make_plugin(root / "git-v1", "hello", "hello-v1")
            plugin_v2 = _make_plugin(root / "git-v2", "hello", "hello-v2")
            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n'
                '[[plugins]]\nname="hello"\nsource="git+https://example.invalid/hello.git@main"\n',
                encoding="utf-8",
            )

            def run(fake_cache):
                fake_cache.current_sha = sha_v1
                _gen, lock1 = installmod.install(distro, home)
                self.assertEqual(lock1.plugins["hello"].resolved_sha, sha_v1)
                self.assertEqual(
                    (home / "plugins" / "hello" / "marker.txt").read_text(encoding="utf-8"),
                    "hello-v1",
                )

                fake_cache.current_sha = sha_v2
                _gen, lock2 = installmod.install(distro, home, update=True, update_only=("hello",))
                self.assertEqual(lock2.plugins["hello"].resolved_sha, sha_v2)
                self.assertEqual(
                    (home / "plugins" / "hello" / "marker.txt").read_text(encoding="utf-8"),
                    "hello-v2",
                )

            self._with_fake_git_cache({sha_v1: plugin_v1, sha_v2: plugin_v2}, run)

    def test_update_only_bundle_plugin_component_refreshes_owning_bundle_git_source(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            sha_v1 = "c" * 40
            sha_v2 = "d" * 40
            bundle_v1 = root / "git-v1" / "mixed"
            bundle_v2 = root / "git-v2" / "mixed"
            _make_skill(bundle_v1, "base-shell", "shell-v1")
            _make_plugin(bundle_v1, "hook-permissions", "hook-v1")
            _make_skill(bundle_v2, "base-shell", "shell-v2")
            _make_plugin(bundle_v2, "hook-permissions", "hook-v2")
            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n'
                '[[bundles]]\nname="mixed"\nsource="git+https://example.invalid/mixed.git@main"\n',
                encoding="utf-8",
            )

            def run(fake_cache):
                fake_cache.current_sha = sha_v1
                _gen, lock1 = installmod.install(distro, home)
                self.assertEqual(lock1.bundles["mixed"].resolved_sha, sha_v1)
                self.assertIn("hook-permissions", lock1.plugins)

                fake_cache.current_sha = sha_v2
                _gen, lock2 = installmod.install(
                    distro, home, update=True, update_only=("hook-permissions",)
                )
                self.assertEqual(lock2.bundles["mixed"].resolved_sha, sha_v2)
                self.assertEqual(lock2.plugins["hook-permissions"].resolved_sha, sha_v2)
                self.assertEqual(
                    (home / "plugins" / "hook-permissions" / "marker.txt").read_text(encoding="utf-8"),
                    "hook-v2",
                )
                self.assertEqual(
                    (home / "skills" / "base-shell" / "marker.txt").read_text(encoding="utf-8"),
                    "shell-v2",
                )

            self._with_fake_git_cache({sha_v1: bundle_v1, sha_v2: bundle_v2}, run)

    def test_bundle_toml_components_missing_dir_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            bundle = root / "ext" / "bundles" / "bad"
            bundle.mkdir(parents=True)
            _touch(bundle / "bundle.toml", '[bundle]\nname="bad"\ncomponents=["missing"]\n')
            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n'
                '[[bundles]]\nname="bad"\nsource="local:../ext/bundles/bad"\n',
                encoding="utf-8",
            )

            with self.assertRaises(installmod.InstallError) as cm:
                installmod.install(distro, home)
            self.assertIn("component not found: missing", str(cm.exception))

    def test_skill_manifest_tools_missing_exec_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")
            _make_skill_with_manifest(
                root / "ext" / "skills",
                "bad-skill",
                "---\nname: bad-skill\ntools:\n  - name: bad_tool\n---\n# bad\n",
            )
            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n'
                '[[skills]]\nname="bad-skill"\nsource="local:../ext/skills/bad-skill"\n',
                encoding="utf-8",
            )

            with self.assertRaises(installmod.InstallError) as cm:
                installmod.install(distro, home)
            self.assertIn("skill bad-skill: tools[0].exec is required", str(cm.exception))
            self.assertFalse((home / "distrib" / "demo" / "current").exists())

    def test_skill_manifest_tools_blank_exec_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")
            _make_skill_with_manifest(
                root / "ext" / "skills",
                "bad-skill",
                "---\nname: bad-skill\ntools:\n  - name: bad_tool\n    exec: '  '\n---\n# bad\n",
            )
            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n'
                '[[skills]]\nname="bad-skill"\nsource="local:../ext/skills/bad-skill"\n',
                encoding="utf-8",
            )

            with self.assertRaises(installmod.InstallError) as cm:
                installmod.install(distro, home)
            self.assertIn("skill bad-skill: tools[0].exec is required", str(cm.exception))

    def test_skill_manifest_tools_missing_name_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")
            _make_skill_with_manifest(
                root / "ext" / "skills",
                "bad-skill",
                "---\nname: bad-skill\ntools:\n  - exec: python run.py\n---\n# bad\n",
            )
            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n'
                '[[skills]]\nname="bad-skill"\nsource="local:../ext/skills/bad-skill"\n',
                encoding="utf-8",
            )

            with self.assertRaises(installmod.InstallError) as cm:
                installmod.install(distro, home)
            self.assertIn("skill bad-skill: tools[0].name is required", str(cm.exception))

    def test_skill_manifest_tools_blank_name_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")
            _make_skill_with_manifest(
                root / "ext" / "skills",
                "bad-skill",
                "---\nname: bad-skill\ntools:\n  - name: '  '\n    exec: python run.py\n---\n# bad\n",
            )
            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n'
                '[[skills]]\nname="bad-skill"\nsource="local:../ext/skills/bad-skill"\n',
                encoding="utf-8",
            )

            with self.assertRaises(installmod.InstallError) as cm:
                installmod.install(distro, home)
            self.assertIn("skill bad-skill: tools[0].name is required", str(cm.exception))

    def test_skill_manifest_tools_invalid_shape_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")
            _make_skill_with_manifest(
                root / "ext" / "skills",
                "bad-skill",
                "---\nname: bad-skill\ntools: invalid\n---\n# bad\n",
            )
            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n'
                '[[skills]]\nname="bad-skill"\nsource="local:../ext/skills/bad-skill"\n',
                encoding="utf-8",
            )

            with self.assertRaises(installmod.InstallError) as cm:
                installmod.install(distro, home)
            self.assertIn("skill bad-skill: tools must be a YAML list", str(cm.exception))

    def test_bundle_invalid_skill_manifest_failure_is_atomic(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            bundle_v1 = root / "ext" / "bundles" / "mixed"
            _make_skill(bundle_v1, "valid-skill", "skill-v1")
            _make_plugin(bundle_v1, "valid-plugin", "plugin-v1")
            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n'
                '[[bundles]]\nname="mixed"\nsource="local:../ext/bundles/mixed"\n',
                encoding="utf-8",
            )
            installmod.install(distro, home)
            current_before = (home / "distrib" / "demo" / "current").resolve()
            lock_before = (home / "distrib" / "demo" / "distro.lock.json").read_text(encoding="utf-8")

            _make_skill_with_manifest(
                bundle_v1,
                "bad-skill",
                "---\nname: bad-skill\ntools:\n  - name: broken\n---\n# bad\n",
            )
            with self.assertRaises(installmod.InstallError) as cm:
                installmod.install(distro, home)

            self.assertIn("bundle mixed -> skill bad-skill: tools[0].exec is required", str(cm.exception))
            self.assertEqual((home / "distrib" / "demo" / "current").resolve(), current_before)
            self.assertEqual((home / "distrib" / "demo" / "distro.lock.json").read_text(encoding="utf-8"), lock_before)
            self.assertFalse((home / "distrib" / "demo" / "skills" / "bad-skill").exists())
            self.assertFalse((home / "skills" / "bad-skill").exists())

    def test_lock_v1_loads_as_v2_with_empty_plugins(self):
        data = {
            "version": 1,
            "distro": "demo",
            "generated_at": "2026-04-21T14:30:00Z",
            "bundles": {},
            "skills": {"foo": {"source": "local:foo", "resolved_path": "/tmp/foo"}},
        }
        lock = lockmod.Lock.from_json(data)
        self.assertEqual(lock.plugins, {})
        self.assertEqual(lock.to_json()["version"], 2)
        self.assertEqual(lock.to_json()["plugins"], {})

    def test_bundle_skips_directories_without_skill_manifest(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            bundle = root / "ext" / "bundles" / "drivers"
            _make_skill(bundle, "driver", "unified")
            (bundle / "driver-openai").mkdir(parents=True)
            (bundle / "driver-openai" / "README.tmp").write_text("not a skill\n", encoding="utf-8")
            (bundle / "_drivers").mkdir(parents=True)
            _touch(bundle / "_drivers" / "lib.py", "X=1\n")

            (distro / "distro.toml").write_text(
                '[distro]\nname="demo"\n\n'
                '[[bundles]]\nname="drivers"\nsource="local:../ext/bundles/drivers"\n',
                encoding="utf-8",
            )

            installmod.install(distro, home)
            skills = home / "distrib" / "demo" / "skills"
            self.assertTrue((skills / "driver" / "SKILL.md").exists())
            self.assertFalse((skills / "_drivers").exists())
            self.assertFalse((skills / "driver-openai").exists())

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
            result1 = installmod.install(distro, home)
            result2 = installmod.install(distro, home)
            gen1, _ = result1
            gen2, _ = result2
            self.assertEqual(gen1.number, gen2.number)
            self.assertTrue(result1.changed)
            self.assertFalse(result2.changed)
            gs = gens.list_generations(home, "demo")
            self.assertEqual([g.number for g in gs], [1])

    def test_cli_reports_unchanged_for_identical_reinstall(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root)

            first = StringIO()
            with redirect_stdout(first):
                rc = climod.main(["--home", str(home), "install", str(distro)])
            self.assertEqual(rc, 0)
            self.assertIn("installed distro demo as generation 1", first.getvalue())

            second = StringIO()
            with redirect_stdout(second):
                rc = climod.main(["--home", str(home), "install", str(distro)])
            self.assertEqual(rc, 0)
            self.assertIn("distro demo unchanged at generation 1", second.getvalue())
            self.assertNotIn("installed distro demo as generation 2", second.getvalue())

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

    def test_in_tree_client_runtime_surface_is_linked(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root)
            client = distro / "clients" / "gateway-cli"
            client.mkdir(parents=True)
            _touch(client / "client.toml", 'id="gateway-cli"\nruntime="python"\nentry="run.py"\n')
            _touch(client / "run.py", "print('gateway')\n")
            installmod.install(distro, home)
            self.assertTrue((home / "distrib" / "demo" / "clients" / "gateway-cli" / "run.py").exists())
            self.assertTrue((home / "clients" / "gateway-cli").is_symlink())
            self.assertTrue((home / "clients" / "gateway-cli" / "run.py").exists())


if __name__ == "__main__":
    unittest.main()

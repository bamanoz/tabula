"""End-to-end tests for tabula_distro.install using local: sources only."""
from __future__ import annotations

from concurrent.futures import ThreadPoolExecutor
import json
import os
import tempfile
import tomllib
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
    exec_cmd = exec_cmd or f"python skills/{name}/scripts/run.py"
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
    _touch(d / "app.toml", (
        f'id = "{name}"\n'
        f'name = "{name}"\n'
        'version = "0.1.0"\n'
        'runtime = "python"\n'
        'entry = "run.py"\n'
    ))
    _touch(d / "run.py", "# client\n")
    _touch(d / "marker.txt", marker)
    return d


def _make_python_package(root: Path, package: str, content: str = "VALUE = 1\n") -> Path:
    pkg = root / package
    _touch(pkg / "__init__.py", content)
    return pkg


def _make_minimal_distro(root: Path, name: str = "demo") -> Path:
    dist = root / name
    _touch(dist / "distro.toml", f'[distro]\nid = "tabula.{name}"\nname = "{name}"\n')
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
                "[distro]\nid=\"tabula.x\"\nname=\"x\"\n"
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
                "[distro]\nid=\"tabula.demo\"\nname=\"demo\"\n"
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
                "[distro]\nid=\"tabula.demo\"\nname=\"demo\"\n"
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
                '[distro]\nid="tabula.demo"\nname="demo"\n[sources.tabula-bundles]\nsource="git+https://example.invalid/bundles.git@main"\n'
                '[[bundles]]\nname="base"\nsource="source:tabula-bundles#path=base"\n',
                encoding="utf-8",
            )
            (root / "distro.override.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n[sources.tabula-bundles]\nsource="local:/tmp/tabula-bundles"\n',
                encoding="utf-8",
            )
            c = cfg.load(root)
            self.assertEqual(c.sources["tabula-bundles"].source, "local:/tmp/tabula-bundles")
            self.assertEqual(c.bundles[0].source, "source:tabula-bundles#path=base")

    def test_source_alias_can_be_overridden_by_environment(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "d"
            root.mkdir()
            (root / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n[sources.tabula-bundles]\nsource="git+https://example.invalid/bundles.git@main"\n'
                '[[bundles]]\nname="base"\nsource="source:tabula-bundles#path=base"\n',
                encoding="utf-8",
            )
            old = os.environ.get("TABULA_SOURCE_ALIAS_TABULA_BUNDLES")
            os.environ["TABULA_SOURCE_ALIAS_TABULA_BUNDLES"] = "local:/tmp/tabula-bundles"
            try:
                c = cfg.load(root)
            finally:
                if old is None:
                    os.environ.pop("TABULA_SOURCE_ALIAS_TABULA_BUNDLES", None)
                else:
                    os.environ["TABULA_SOURCE_ALIAS_TABULA_BUNDLES"] = old
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

    def test_git_distro_lock_records_resolved_revision(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            def run(fake_cache):
                fake_cache.current_sha = "a" * 40
                result = installmod.install(
                    "git+https://example.invalid/demo.git@main",
                    home,
                )
                self.assertEqual(result.lock.distro_source, "git+https://example.invalid/demo.git@main")
                self.assertEqual(result.lock.distro_resolved_sha, "a" * 40)
                persisted = lockmod.load(home / "distrib" / "demo" / "distro.lock.json")
                self.assertIsNotNone(persisted)
                self.assertEqual(persisted.distro_resolved_sha, "a" * 40)

            self._with_fake_git_cache({"a" * 40: distro}, run)

    def test_install_with_local_bundle_and_skill(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            # bundle with two skills; legacy bundle-local _lib roots are ignored
            bundle = root / "ext" / "bundles" / "mempalace"
            _make_skill(bundle, "mempalace", "save-v1")
            _make_skill(bundle, "mempalace-auto", "search-v1")
            _touch(bundle / "_lib" / "python" / "src" / "pkg" / "__init__.py", "X=1\n")

            # standalone external skill
            ext_skill = root / "ext" / "skills" / "weather"
            _make_skill(ext_skill.parent, "weather", "weather-v1")

            (distro / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n\n'
                '[[bundles]]\nname="mempalace"\nsource="local:../ext/bundles/mempalace"\n\n'
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
                home / "distrib" / "demo" / "skills" / "mempalace" / "marker.txt",
                home / "distrib" / "demo" / "skills" / "mempalace-auto" / "marker.txt",
                home / "distrib" / "demo" / "skills" / "weather" / "marker.txt",
            ):
                self.assertTrue(p.exists(), p)
            self.assertFalse((home / "_lib").exists())

            self.assertFalse((home / "boot.py").exists())
            self.assertTrue((home / "distrib" / "active").is_symlink())
            kernel_config = tomllib.loads((home / "config" / "kernel.toml").read_text(encoding="utf-8"))
            self.assertEqual(kernel_config["kernel"], {"url": "ws://localhost:8089/ws"})
            self.assertEqual(kernel_config["runtime_wss"], {"enabled": False})

            self.assertIn("mempalace", lock.bundles)
            self.assertIn("weather", lock.skills)

    def test_install_refreshes_only_requested_tenant_runtime_surface(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            bundle = root / "ext" / "bundles" / "mempalace"
            _make_skill(bundle, "mempalace", "save-v1")
            _make_plugin(bundle, "sessions", "sessions-v1")
            _touch(bundle / "client-probe" / "app.toml", 'id = "client-probe"\nname = "client-probe"\nversion = "0.1.0"\nruntime = "python"\nentry = "run.py"\n')
            _touch(bundle / "client-probe" / "run.py", "# client\n")
            _touch(bundle / "templates" / "SYSTEM.md", "system\n")
            _touch(bundle / "_lib" / "python" / "src" / "pkg" / "__init__.py", "X=1\n")

            (distro / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n\n'
                '[[bundles]]\nname="mempalace"\nsource="local:../ext/bundles/mempalace"\n',
                encoding="utf-8",
            )

            for tenant_name in ("alpha", "beta"):
                (home / "tenants" / tenant_name).mkdir(parents=True, exist_ok=True)

            installmod.install(distro, home, tenant="alpha")

            tenant_root = home / "tenants" / "alpha"
            self.assertTrue((tenant_root / "skills" / "mempalace" / "marker.txt").exists())
            self.assertTrue((tenant_root / "plugins" / "sessions" / "marker.txt").exists())
            self.assertTrue((tenant_root / "apps" / "client-probe" / "run.py").exists())
            self.assertTrue((tenant_root / "packages").exists())
            self.assertFalse((tenant_root / "_lib").exists())
            self.assertFalse((home / "tenants" / "beta" / "skills").exists())

    def test_install_tenant_filter_refreshes_only_requested_tenant(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            bundle = root / "ext" / "bundles" / "mempalace"
            _make_skill(bundle, "mempalace", "save-v1")
            (distro / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n\n'
                '[[bundles]]\nname="mempalace"\nsource="local:../ext/bundles/mempalace"\n',
                encoding="utf-8",
            )

            for tenant_name in ("alpha", "beta"):
                (home / "tenants" / tenant_name).mkdir(parents=True, exist_ok=True)

            installmod.install(distro, home, tenant="alpha")

            self.assertTrue((home / "tenants" / "alpha" / "skills" / "mempalace" / "marker.txt").exists())
            self.assertFalse((home / "tenants" / "beta" / "skills" / "mempalace" / "marker.txt").exists())

    def test_install_removes_legacy_tenant_lib_symlink_when_refreshing_surface(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            bundle = root / "ext" / "bundles" / "mempalace"
            _make_skill(bundle, "mempalace", "save-v1")
            _touch(bundle / "_lib" / "python" / "src" / "pkg" / "__init__.py", "X=1\n")
            (distro / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n\n'
                '[[bundles]]\nname="mempalace"\nsource="local:../ext/bundles/mempalace"\n',
                encoding="utf-8",
            )

            installmod.install(distro, home)
            tenant_root = home / "tenants" / "gamma"
            tenant_root.mkdir(parents=True)
            (tenant_root / "_lib").symlink_to(Path("../../_lib"))

            installmod.install(distro, home, update=True, tenant="gamma")

            self.assertFalse((home / "_lib").exists())
            self.assertFalse((tenant_root / "_lib").exists())
            self.assertTrue((tenant_root / "packages").exists())

    def test_install_update_does_not_mutate_unpinned_tenants(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            bundle = root / "ext" / "bundles" / "mempalace"
            _make_skill(bundle, "mempalace", "save-v1")
            (distro / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n\n'
                '[[bundles]]\nname="mempalace"\nsource="local:../ext/bundles/mempalace"\n',
                encoding="utf-8",
            )

            for tenant_name in ("alpha", "beta"):
                (home / "tenants" / tenant_name).mkdir(parents=True, exist_ok=True)

            installmod.install(distro, home)
            _touch(bundle / "mempalace" / "marker.txt", "save-v2\n")

            installmod.install(distro, home, update=True)

            for tenant_name in ("alpha", "beta"):
                self.assertFalse((home / "tenants" / tenant_name / "skills").exists())

    def test_parallel_tenant_runtime_refresh_does_not_corrupt_surfaces(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            bundle = root / "ext" / "bundles" / "mempalace"
            _make_skill(bundle, "mempalace", "save-v1")
            _make_plugin(bundle, "sessions", "sessions-v1")
            _touch(bundle / "_lib" / "python" / "src" / "pkg" / "__init__.py", "X=1\n")
            (distro / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n\n'
                '[[bundles]]\nname="mempalace"\nsource="local:../ext/bundles/mempalace"\n',
                encoding="utf-8",
            )

            installmod.install(distro, home)
            tenant_roots = []
            for tenant_name in ("alpha", "beta"):
                tenant_root = home / "tenants" / tenant_name
                tenant_root.mkdir(parents=True, exist_ok=True)
                tenant_roots.append(tenant_root)

            generation = gens.current_generation(home, "demo")
            self.assertIsNotNone(generation)
            with ThreadPoolExecutor(max_workers=2) as pool:
                futures = [pool.submit(installmod._refresh_tenant_runtime_surface, tenant_root, generation.path) for tenant_root in tenant_roots]
                for future in futures:
                    future.result()

            for tenant_name in ("alpha", "beta"):
                tenant_root = home / "tenants" / tenant_name
                self.assertTrue((tenant_root / "skills" / "mempalace" / "marker.txt").exists())
                self.assertTrue((tenant_root / "plugins" / "sessions" / "marker.txt").exists())
                self.assertTrue((tenant_root / "packages").exists())
                self.assertFalse((tenant_root / "_lib").exists())

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
                '[distro]\nid="tabula.demo"\nname="demo"\n[sources.tabula-bundles]\nsource="local:../tabula-bundles"\n'
                '[[bundles]]\nname="base"\nsource="source:tabula-bundles#path=base"\n'
                '[[bundles]]\nname="caveman"\nsource="source:tabula-bundles#path=caveman"\n',
                encoding="utf-8",
            )

            _gen, lock = installmod.install(distro, home)
            self.assertTrue((home / "skills" / "shell" / "marker.txt").exists())
            self.assertTrue((home / "skills" / "caveman-compress" / "marker.txt").exists())
            self.assertFalse((home / "_lib").exists())
            self.assertEqual(lock.bundles["base"].source, "local:../tabula-bundles#path=base")
            self.assertEqual(lock.bundles["caveman"].source, "local:../tabula-bundles#path=caveman")
            self.assertEqual(lock.bundles["base"].resolved_path, str((repo / "base").resolve()))

    def test_mixed_bundle_sources_with_identical_legacy_shared_lib_ignored(self):
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
                '[distro]\nid="tabula.demo"\nname="demo"\n'
                '[[bundles]]\nname="base"\nsource="local:../local-repo/base"\n'
                '[[bundles]]\nname="caveman"\nsource="local:../git-repo/caveman"\n',
                encoding="utf-8",
            )

            installmod.install(distro, home)
            self.assertFalse((home / "_lib").exists())
            self.assertTrue((home / "skills" / "shell" / "marker.txt").exists())
            self.assertTrue((home / "skills" / "caveman-compress" / "marker.txt").exists())

    def test_mixed_bundle_sources_with_different_legacy_shared_lib_ignored(self):
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
                '[distro]\nid="tabula.demo"\nname="demo"\n'
                '[[bundles]]\nname="base"\nsource="local:../local-repo/base"\n'
                '[[bundles]]\nname="caveman"\nsource="local:../git-repo/caveman"\n'
                'override=true\n',
                encoding="utf-8",
            )

            installmod.install(distro, home)
            self.assertFalse((home / "_lib").exists())

    def test_bundle_install_mixed_skills_and_plugins(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            bundle = root / "ext" / "bundles" / "mixed"
            _make_skill(bundle, "base-shell", "shell-v1")
            _make_plugin(bundle, "hook-permissions", "hook-v1")

            (distro / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n\n'
                '[[bundles]]\nname="mixed"\nsource="local:../ext/bundles/mixed"\n',
                encoding="utf-8",
            )

            _gen, lock = installmod.install(distro, home)
            self.assertTrue((home / "distrib" / "demo" / "skills" / "base-shell" / "SKILL.md").exists())
            self.assertTrue((home / "distrib" / "demo" / "plugins" / "hook-permissions" / "plugin.toml").exists())
            self.assertTrue((home / "plugins" / "hook-permissions" / "plugin.toml").exists())
            self.assertIn("base-shell", lock.skills)
            self.assertIn("hook-permissions", lock.plugins)

    def test_bundle_install_apps(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            bundle = root / "ext" / "bundles" / "apps"
            _make_client(bundle, "driver", "driver-v1")
            _make_client(bundle, "subagent", "subagent-v1")

            (distro / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n\n'
                '[[bundles]]\nname="apps"\nsource="local:../ext/bundles/apps"\n',
                encoding="utf-8",
            )

            _gen, lock = installmod.install(distro, home)
            self.assertTrue((home / "distrib" / "demo" / "apps" / "driver" / "app.toml").exists())
            self.assertTrue((home / "apps" / "driver" / "run.py").exists())
            self.assertFalse((home / "skills" / "driver").exists())
            self.assertIn("driver", lock.apps)
            self.assertIn("subagent", lock.apps)

    def test_app_manifest_is_validated(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")
            bundle = root / "ext" / "bundles" / "apps"
            bad = bundle / "driver"
            bad.mkdir(parents=True)
            _touch(bad / "app.toml", 'id="wrong"\nruntime="python"\nentry="run.py"\n')
            _touch(bad / "run.py", "# client\n")
            (distro / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n\n'
                '[[bundles]]\nname="apps"\nsource="local:../ext/bundles/apps"\n',
                encoding="utf-8",
            )
            with self.assertRaises(installmod.InstallError) as cm:
                installmod.install(distro, home)
            self.assertIn("must match directory name", str(cm.exception))

    def test_app_manifest_entry_must_stay_inside_component(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")
            bundle = root / "ext" / "bundles" / "apps"
            bad = bundle / "driver"
            bad.mkdir(parents=True)
            _touch(bad / "app.toml", 'id="driver"\nruntime="python"\nentry="../run.py"\n')
            _touch(bundle / "run.py", "# outside\n")
            (distro / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n\n'
                '[[bundles]]\nname="apps"\nsource="local:../ext/bundles/apps"\n',
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
                '[distro]\nid="tabula.demo"\nname="demo"\n'
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
                '[distro]\nid="tabula.demo"\nname="demo"\n'
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
                '[distro]\nid="tabula.demo"\nname="demo"\n'
                '[[bundles]]\nname="legacy"\nsource="local:../ext/bundles/legacy"\n',
                encoding="utf-8",
            )

            _gen, lock = installmod.install(distro, home)
            self.assertTrue((home / "distrib" / "demo" / "skills" / "skill-a" / "SKILL.md").exists())
            self.assertTrue((home / "distrib" / "demo" / "plugins" / "plugin-a" / "plugin.toml").exists())
            self.assertIn("skill-a", lock.skills)
            self.assertIn("plugin-a", lock.plugins)

    def test_bundle_exported_python_package_is_installed_into_shared_lib(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            bundle = root / "ext" / "bundles" / "base"
            _make_plugin(bundle, "sessions")
            _make_python_package(bundle / "sessions" / "sdk" / "python" / "src", "tabula_session_sdk")
            _touch(bundle / "bundle.toml", (
                '[bundle]\nname="base"\ncomponents=["sessions"]\n'
                '[[exports.python_packages]]\n'
                'name="tabula_session_sdk"\n'
                'path="sessions/sdk/python/src/tabula_session_sdk"\n'
            ))
            (distro / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n'
                '[[bundles]]\nname="base"\nsource="local:../ext/bundles/base"\n',
                encoding="utf-8",
            )

            installmod.install(distro, home)
            self.assertTrue((home / "packages" / "python" / "src" / "tabula_session_sdk" / "__init__.py").is_file())

    def test_distro_exported_python_package_is_installed_into_package_surface(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")
            _touch(distro / "_lib" / "python" / "src" / "demo_prompt" / "__init__.py", "VALUE = 1\n")
            with (distro / "distro.toml").open("a", encoding="utf-8") as f:
                f.write(
                    '[[exports.python_packages]]\n'
                    'name="demo_prompt"\n'
                    'path="_lib/python/src/demo_prompt"\n'
                    'owner="demo"\n'
                )

            installmod.install(distro, home)

            self.assertTrue((home / "packages" / "python" / "src" / "demo_prompt" / "__init__.py").is_file())
            self.assertFalse((home / "_lib").exists())

    def test_bundle_exported_python_packages_from_multiple_bundles_install(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            base = root / "ext" / "bundles" / "base"
            _make_plugin(base, "sessions")
            _make_python_package(base / "sessions" / "sdk" / "python" / "src", "tabula_session_sdk")
            _touch(base / "bundle.toml", (
                '[bundle]\nname="base"\ncomponents=["sessions"]\n'
                '[[exports.python_packages]]\n'
                'name="tabula_session_sdk"\n'
                'path="sessions/sdk/python/src/tabula_session_sdk"\n'
            ))

            drivers = root / "ext" / "bundles" / "drivers"
            _make_plugin(drivers, "driver")
            _make_python_package(drivers / "driver" / "sdk" / "python" / "src", "tabula_driver_sdk")
            _touch(drivers / "bundle.toml", (
                '[bundle]\nname="drivers"\ncomponents=["driver"]\n'
                '[[exports.python_packages]]\n'
                'name="tabula_driver_sdk"\n'
                'path="driver/sdk/python/src/tabula_driver_sdk"\n'
            ))
            (distro / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n'
                '[[bundles]]\nname="base"\nsource="local:../ext/bundles/base"\n'
                '[[bundles]]\nname="drivers"\nsource="local:../ext/bundles/drivers"\n',
                encoding="utf-8",
            )

            installmod.install(distro, home)

            self.assertTrue((home / "packages" / "python" / "src" / "tabula_session_sdk" / "__init__.py").is_file())
            self.assertTrue((home / "packages" / "python" / "src" / "tabula_driver_sdk" / "__init__.py").is_file())

    def test_bundle_exported_python_package_conflict_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            for bundle_name in ("alpha", "beta"):
                bundle = root / "ext" / "bundles" / bundle_name
                component = f"{bundle_name}-plugin"
                _make_plugin(bundle, component)
                _make_python_package(bundle / component / "sdk" / "python" / "src", "shared_sdk")
                _touch(bundle / "bundle.toml", (
                    f'[bundle]\nname="{bundle_name}"\ncomponents=["{component}"]\n'
                    '[[exports.python_packages]]\n'
                    'name="shared_sdk"\n'
                    f'path="{component}/sdk/python/src/shared_sdk"\n'
                ))
            (distro / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n'
                '[[bundles]]\nname="alpha"\nsource="local:../ext/bundles/alpha"\n'
                '[[bundles]]\nname="beta"\nsource="local:../ext/bundles/beta"\n',
                encoding="utf-8",
            )

            with self.assertRaisesRegex(installmod.InstallError, "exported python package 'shared_sdk' conflicts"):
                installmod.install(distro, home)

    def test_bundle_dependency_requires_selected_bundle(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            gateways = root / "ext" / "bundles" / "gateways"
            _make_plugin(gateways, "gateway-web")
            _touch(gateways / "bundle.toml", (
                '[bundle]\nname="gateways"\ncomponents=["gateway-web"]\n'
                '[[dependencies]]\n'
                'bundle="base"\n'
                'python_packages=["tabula_session_sdk"]\n'
            ))
            (distro / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n'
                '[[bundles]]\nname="gateways"\nsource="local:../ext/bundles/gateways"\n',
                encoding="utf-8",
            )

            with self.assertRaisesRegex(installmod.InstallError, "depends on bundle 'base'"):
                installmod.install(distro, home)

    def test_bundle_dependency_requires_exported_python_package(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            base = root / "ext" / "bundles" / "base"
            _make_plugin(base, "sessions")
            _touch(base / "bundle.toml", '[bundle]\nname="base"\ncomponents=["sessions"]\n')

            gateways = root / "ext" / "bundles" / "gateways"
            _make_plugin(gateways, "gateway-web")
            _touch(gateways / "bundle.toml", (
                '[bundle]\nname="gateways"\ncomponents=["gateway-web"]\n'
                '[[dependencies]]\n'
                'bundle="base"\n'
                'python_packages=["tabula_session_sdk"]\n'
            ))
            (distro / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n'
                '[[bundles]]\nname="base"\nsource="local:../ext/bundles/base"\n'
                '[[bundles]]\nname="gateways"\nsource="local:../ext/bundles/gateways"\n',
                encoding="utf-8",
            )

            with self.assertRaisesRegex(installmod.InstallError, 'does not export'):
                installmod.install(distro, home)

    def test_bundle_dependency_requires_exported_typescript_package(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            base = root / "ext" / "bundles" / "base"
            _make_plugin(base, "skills")
            _touch(base / "bundle.toml", '[bundle]\nname="base"\ncomponents=["skills"]\n')

            gateway = root / "ext" / "bundles" / "gateway"
            _make_plugin(gateway, "gateway-node")
            _touch(gateway / "bundle.toml", (
                '[bundle]\nname="gateway"\ncomponents=["gateway-node"]\n'
                '[[dependencies]]\n'
                'bundle="base"\n'
                'typescript_packages=["@tabula/skill-sdk"]\n'
            ))
            (distro / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n'
                '[[bundles]]\nname="base"\nsource="local:../ext/bundles/base"\n'
                '[[bundles]]\nname="gateway"\nsource="local:../ext/bundles/gateway"\n',
                encoding="utf-8",
            )

            with self.assertRaisesRegex(installmod.InstallError, 'typescript package'):
                installmod.install(distro, home)

    def test_bundle_exported_typescript_package_is_installed_and_locked(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            base = root / "ext" / "bundles" / "base"
            _make_plugin(base, "skills")
            package_root = base / "skills" / "sdk" / "typescript"
            _touch(package_root / "package.json", '{"name":"@tabula/skill-sdk","version":"0.1.0"}\n')
            _touch(package_root / "src" / "index.ts", "export const ok = true;\n")
            _touch(base / "bundle.toml", (
                '[bundle]\nname="base"\ncomponents=["skills"]\n'
                '[[exports.typescript_packages]]\n'
                'name="@tabula/skill-sdk"\n'
                'path="skills/sdk/typescript"\n'
            ))
            (distro / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n'
                '[[bundles]]\nname="base"\nsource="local:../ext/bundles/base"\n',
                encoding="utf-8",
            )

            result = installmod.install(distro, home)

            self.assertTrue((home / "packages" / "typescript" / "@tabula" / "skill-sdk" / "package.json").is_file())
            self.assertEqual(result.lock.sdk_versions.get("@tabula/skill-sdk"), "0.1.0")

    def test_bundle_dependency_cycle_fails_install(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            alpha = root / "ext" / "bundles" / "alpha"
            _make_plugin(alpha, "a")
            _touch(alpha / "bundle.toml", (
                '[bundle]\nname="alpha"\ncomponents=["a"]\n'
                '[[dependencies]]\n'
                'bundle="beta"\n'
            ))

            beta = root / "ext" / "bundles" / "beta"
            _make_plugin(beta, "b")
            _touch(beta / "bundle.toml", (
                '[bundle]\nname="beta"\ncomponents=["b"]\n'
                '[[dependencies]]\n'
                'bundle="alpha"\n'
            ))

            (distro / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n'
                '[[bundles]]\nname="alpha"\nsource="local:../ext/bundles/alpha"\n'
                '[[bundles]]\nname="beta"\nsource="local:../ext/bundles/beta"\n',
                encoding="utf-8",
            )

            with self.assertRaisesRegex(installmod.InstallError, 'bundle dependency cycle'):
                installmod.install(distro, home)

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
                '[distro]\nid="tabula.demo"\nname="demo"\n'
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
                '[distro]\nid="tabula.demo"\nname="demo"\n'
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
                '[distro]\nid="tabula.demo"\nname="demo"\n'
                '[[bundles]]\nname="bad"\nsource="local:../ext/bundles/bad"\n',
                encoding="utf-8",
            )

            with self.assertRaises(installmod.InstallError) as cm:
                installmod.install(distro, home)
            self.assertIn("component not found: missing", str(cm.exception))

    def test_skill_manifest_tools_missing_exec_is_ignored(self):
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
                '[distro]\nid="tabula.demo"\nname="demo"\n'
                '[[skills]]\nname="bad-skill"\nsource="local:../ext/skills/bad-skill"\n',
                encoding="utf-8",
            )

            installmod.install(distro, home)
            self.assertTrue((home / "distrib" / "demo" / "current").exists())

    def test_skill_manifest_tools_blank_exec_is_ignored(self):
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
                '[distro]\nid="tabula.demo"\nname="demo"\n'
                '[[skills]]\nname="bad-skill"\nsource="local:../ext/skills/bad-skill"\n',
                encoding="utf-8",
            )

            installmod.install(distro, home)
            self.assertTrue((home / "distrib" / "demo" / "current").exists())

    def test_skill_manifest_tools_missing_name_is_ignored(self):
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
                '[distro]\nid="tabula.demo"\nname="demo"\n'
                '[[skills]]\nname="bad-skill"\nsource="local:../ext/skills/bad-skill"\n',
                encoding="utf-8",
            )

            installmod.install(distro, home)
            self.assertTrue((home / "distrib" / "demo" / "current").exists())

    def test_skill_manifest_tools_blank_name_is_ignored(self):
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
                '[distro]\nid="tabula.demo"\nname="demo"\n'
                '[[skills]]\nname="bad-skill"\nsource="local:../ext/skills/bad-skill"\n',
                encoding="utf-8",
            )

            installmod.install(distro, home)
            self.assertTrue((home / "distrib" / "demo" / "current").exists())

    def test_skill_manifest_tools_invalid_shape_is_ignored(self):
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
                '[distro]\nid="tabula.demo"\nname="demo"\n'
                '[[skills]]\nname="bad-skill"\nsource="local:../ext/skills/bad-skill"\n',
                encoding="utf-8",
            )

            installmod.install(distro, home)
            self.assertTrue((home / "distrib" / "demo" / "current").exists())

    def test_bundle_skill_manifest_tool_shape_no_longer_blocks_install(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            bundle_v1 = root / "ext" / "bundles" / "mixed"
            _make_skill(bundle_v1, "valid-skill", "skill-v1")
            _make_plugin(bundle_v1, "valid-plugin", "plugin-v1")
            (distro / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n'
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
            installmod.install(distro, home)

            self.assertNotEqual((home / "distrib" / "demo" / "current").resolve(), current_before)
            self.assertNotEqual((home / "distrib" / "demo" / "distro.lock.json").read_text(encoding="utf-8"), lock_before)
            self.assertTrue((home / "distrib" / "demo" / "skills" / "bad-skill").exists())
            self.assertTrue((home / "skills" / "bad-skill").exists())

    def test_bundle_component_with_multiple_manifests_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            bundle = root / "ext" / "bundles" / "mixed"
            component = _make_skill(bundle, "dual", "skill-v1")
            _touch(component / "plugin.toml", 'id = "dual"\nname = "dual"\nversion = "0.1.0"\nruntime = "python"\nentry = "run.py"\n')
            _touch(component / "run.py", "#!/usr/bin/env python3\n")
            _touch(bundle / "bundle.toml", '[bundle]\nname="mixed"\ncomponents=["dual"]\n')
            (distro / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n'
                '[[bundles]]\nname="mixed"\nsource="local:../ext/bundles/mixed"\n',
                encoding="utf-8",
            )

            with self.assertRaises(installmod.InstallError) as cm:
                installmod.install(distro, home)
            self.assertIn("bundle mixed: component 'dual' has multiple component manifests", str(cm.exception))

    def test_lock_v1_migrates_to_current_with_empty_plugins(self):
        data = {
            "version": 1,
            "distro": "demo",
            "generated_at": "2026-04-21T14:30:00Z",
            "bundles": {},
            "skills": {"foo": {"source": "local:foo", "resolved_path": "/tmp/foo"}},
        }
        lock = lockmod.Lock.from_json(data)
        self.assertEqual(lock.plugins, {})
        self.assertEqual(lock.to_json()["version"], lockmod.LOCK_VERSION)
        self.assertEqual(lock.to_json()["plugins"], {})

    def test_lock_v2_migrates_to_current(self):
        data = {
            "version": 2,
            "distro": "demo",
            "generated_at": "2026-04-21T14:30:00Z",
            "kernel_version": "0.9.0",
            "bundles": {},
            "skills": {},
            "plugins": {},
            "apps": {},
        }
        lock = lockmod.Lock.from_json(data)
        self.assertEqual(lock.kernel_version, "0.9.0")
        self.assertIsNone(lock.plugin_protocol_version)
        self.assertEqual(lock.sdk_versions, {})
        self.assertEqual(lock.to_json()["version"], lockmod.LOCK_VERSION)

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
                '[distro]\nid="tabula.demo"\nname="demo"\n\n'
                '[[bundles]]\nname="drivers"\nsource="local:../ext/bundles/drivers"\n',
                encoding="utf-8",
            )

            installmod.install(distro, home)
            skills = home / "distrib" / "demo" / "skills"
            self.assertTrue((skills / "driver" / "SKILL.md").exists())
            self.assertFalse((skills / "_drivers").exists())
            self.assertFalse((skills / "driver-openai").exists())

    def test_plugin_install_ignores_node_modules(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root, "demo")

            plugin = root / "ext" / "plugins" / "gateway-web"
            _make_plugin(plugin.parent, "gateway-web", "gateway-web-v1")
            _touch(plugin / "web" / "dist" / "index.html", "<html></html>\n")
            _touch(plugin / "web" / "node_modules" / "left-pad" / "index.js", "module.exports = 0;\n")

            (distro / "distro.toml").write_text(
                '[distro]\nid="tabula.demo"\nname="demo"\n\n'
                '[[plugins]]\nname="gateway-web"\nsource="local:../ext/plugins/gateway-web"\n',
                encoding="utf-8",
            )

            installmod.install(distro, home)

            staged_plugin = home / "plugins" / "gateway-web"
            self.assertTrue((staged_plugin / "web" / "dist" / "index.html").exists())
            self.assertFalse((staged_plugin / "web" / "node_modules").exists())

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
                '[distro]\nid="tabula.demo"\nname="demo"\n'
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
                '[distro]\nid="tabula.demo"\nname="demo"\n'
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

    def test_prune_preserves_tenant_referenced_generation(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root)
            first = installmod.install(distro, home, keep_generations=1)
            tenant = home / "tenants" / "project-a"
            tenant.mkdir(parents=True)
            (tenant / "install.lock.json").write_text(
                json.dumps({
                    "version": 1,
                    "generation": {
                        "distro": "demo",
                        "name": first.generation.name,
                        "path": f"distrib/demo/generations/{first.generation.name}",
                    }
                }),
                encoding="utf-8",
            )

            for version in ("v2", "v3"):
                _touch(distro / "skills" / "version" / "SKILL.md", f"# {version}\n")
                installmod.install(distro, home, keep_generations=1)

            generations = gens.list_generations(home, "demo")
            self.assertEqual([g.number for g in generations], [1, 3])
            self.assertTrue(first.generation.path.is_dir())

    def test_two_distros_pin_separate_tenant_surfaces(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            alpha = _make_minimal_distro(root, "alpha")
            beta = _make_minimal_distro(root, "beta")
            _touch(alpha / "skills" / "identity" / "SKILL.md", "alpha\n")
            _touch(beta / "skills" / "identity" / "SKILL.md", "beta\n")
            for tenant_name in ("project-alpha", "project-beta"):
                (home / "tenants" / tenant_name).mkdir(parents=True)

            alpha_result = installmod.install(alpha, home, tenant="project-alpha")
            beta_result = installmod.install(beta, home, tenant="project-beta")

            alpha_lock = json.loads((home / "tenants" / "project-alpha" / "install.lock.json").read_text(encoding="utf-8"))
            beta_lock = json.loads((home / "tenants" / "project-beta" / "install.lock.json").read_text(encoding="utf-8"))
            self.assertEqual(alpha_lock["generation"]["name"], alpha_result.generation.name)
            self.assertEqual(beta_lock["generation"]["name"], beta_result.generation.name)
            alpha_marker = home / "tenants" / "project-alpha" / "skills" / "identity" / "SKILL.md"
            beta_marker = home / "tenants" / "project-beta" / "skills" / "identity" / "SKILL.md"
            self.assertEqual(alpha_marker.read_text(encoding="utf-8"), "alpha\n")
            self.assertEqual(beta_marker.read_text(encoding="utf-8"), "beta\n")
            self.assertTrue(os.path.samefile(alpha_marker, alpha_result.generation.path / "skills" / "identity" / "SKILL.md"))
            self.assertTrue(os.path.samefile(beta_marker, beta_result.generation.path / "skills" / "identity" / "SKILL.md"))

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
                '[distro]\nid="tabula.demo"\nname="demo"\n'
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
            client = distro / "apps" / "gateway-cli"
            client.mkdir(parents=True)
            _touch(client / "app.toml", 'id="gateway-cli"\nruntime="python"\nentry="run.py"\n')
            _touch(client / "run.py", "print('gateway')\n")
            installmod.install(distro, home)
            self.assertTrue((home / "distrib" / "demo" / "apps" / "gateway-cli" / "run.py").exists())
            self.assertTrue((home / "apps" / "gateway-cli").is_symlink())
            self.assertTrue((home / "apps" / "gateway-cli" / "run.py").exists())

    def test_install_writes_reload_trigger(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_minimal_distro(root)
            installmod.install(distro, home)
            trigger = home / "run" / "reload.touch"
            self.assertTrue(trigger.exists(), "reload.touch should be created on install")
            self.assertIn("time=", trigger.read_text())
            first_mtime = trigger.stat().st_mtime
            # second install (no-op fingerprint match) must still bump the trigger
            # so a kernel that missed the first install picks up the no-op too.
            import time as _time
            _time.sleep(0.05)
            installmod.install(distro, home)
            self.assertGreater(trigger.stat().st_mtime, first_mtime)


if __name__ == "__main__":
    unittest.main()

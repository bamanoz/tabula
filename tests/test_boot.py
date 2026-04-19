#!/usr/bin/env python3
"""Tests for boot.py skill discovery — flat skills + symlinked bundles."""

import json
import os
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

from tests.flat_surface import materialize_flat_surface

ROOT = os.path.join(os.path.dirname(__file__), "..")
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)


def _load_boot_module():
    import importlib.util
    import types

    root = os.path.abspath(ROOT)
    materialize_flat_surface(Path(root), source_root=Path(root))
    old_env = dict(os.environ)
    os.environ["TABULA_HOME"] = root
    os.environ["TABULA_PROVIDER"] = os.environ.get("TABULA_PROVIDER", "openai")

    for name in list(sys.modules.keys()):
        if name == "skills" or name.startswith("skills."):
            sys.modules.pop(name, None)

    skills_pkg = types.ModuleType("skills")
    skills_pkg.__path__ = [os.path.join(root, "skills")]
    sys.modules["skills"] = skills_pkg

    lib_init = os.path.join(root, "skills", "lib", "__init__.py")
    lib_spec = importlib.util.spec_from_file_location(
        "skills.lib",
        lib_init,
        submodule_search_locations=[os.path.join(root, "skills", "lib")],
    )
    lib_mod = importlib.util.module_from_spec(lib_spec)
    assert lib_spec.loader is not None
    sys.modules["skills.lib"] = lib_mod
    lib_spec.loader.exec_module(lib_mod)

    boot_path = os.path.join(ROOT, "distrib", "assistant", "boot.py")
    spec = importlib.util.spec_from_file_location("tabula_main_boot", boot_path)
    mod = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    try:
        spec.loader.exec_module(mod)
    finally:
        os.environ.clear()
        os.environ.update(old_env)
    return mod


boot = _load_boot_module()


class BootTestBase(unittest.TestCase):
    """Base class that patches SKILLS_DIR to a temp directory."""

    def setUp(self):
        self.tmpdir = tempfile.mkdtemp()
        self.skills_dir = os.path.join(self.tmpdir, "skills")
        self.bundles_dir = os.path.join(self.tmpdir, "bundles")
        os.makedirs(self.skills_dir)
        self._orig_skills_dir = boot.SKILLS_DIR
        boot.SKILLS_DIR = self.skills_dir

    def tearDown(self):
        boot.SKILLS_DIR = self._orig_skills_dir
        shutil.rmtree(self.tmpdir)

    def _write_skill(self, rel_path, content, bundle=False):
        """Write a SKILL.md at the given relative path under skills or bundles.

        If bundle=True, writes under bundles/ and creates a symlink in skills/
        for the leaf skill directory (flat layout).
        e.g. rel_path="caveman/caveman-commit" -> skills/caveman-commit symlink.
        """
        if bundle:
            base = self.bundles_dir
            os.makedirs(base, exist_ok=True)
            full = os.path.join(base, rel_path, "SKILL.md")
            os.makedirs(os.path.dirname(full), exist_ok=True)
            with open(full, "w") as f:
                f.write(content)
            # Create symlink: skills/<leaf> -> bundles/<bundle>/<leaf>
            leaf = os.path.basename(rel_path)
            link = os.path.join(self.skills_dir, leaf)
            target = os.path.join(self.bundles_dir, rel_path)
            if not os.path.exists(link):
                os.symlink(target, link)
        else:
            full = os.path.join(self.skills_dir, rel_path, "SKILL.md")
            os.makedirs(os.path.dirname(full), exist_ok=True)
            with open(full, "w") as f:
                f.write(content)

    def _write_file(self, rel_path, content="", bundle=False):
        """Write an arbitrary file under skills or bundles."""
        if bundle:
            base = self.bundles_dir
        else:
            base = self.skills_dir
        full = os.path.join(base, rel_path)
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "w") as f:
            f.write(content)


class TestParseSkillMd(unittest.TestCase):
    def test_no_frontmatter(self):
        meta, body = boot.parse_skill_md("# Hello\nWorld")
        self.assertEqual(meta, {})
        self.assertEqual(body, "# Hello\nWorld")

    def test_basic_frontmatter(self):
        text = '---\nname: test\ndescription: "A test"\n---\nBody here'
        meta, body = boot.parse_skill_md(text)
        self.assertEqual(meta["name"], "test")
        self.assertEqual(meta["description"], "A test")
        self.assertEqual(body, "Body here")

    def test_json_value(self):
        text = '---\ntools: [{"name": "foo"}]\n---\nBody'
        meta, body = boot.parse_skill_md(text)
        self.assertEqual(meta["tools"], [{"name": "foo"}])

    def test_multiline_description(self):
        text = '---\ndescription: >\n  line one\n  line two\n---\nBody'
        meta, body = boot.parse_skill_md(text)
        self.assertIn("line one", meta["description"])
        self.assertIn("line two", meta["description"])
        self.assertNotIn(">", meta["description"])


class TestWalkSkills(BootTestBase):
    def test_flat_layout(self):
        self._write_skill("weather", '---\nname: weather\n---\n')
        self._write_skill("files", '---\nname: files\n---\n')
        results = boot.walk_skills()
        names = [os.path.basename(rel) for rel, _ in results]
        self.assertIn("weather", names)
        self.assertIn("files", names)

    def test_bundles_via_symlink(self):
        """Bundle skills are symlinked flat into skills/."""
        self._write_skill("caveman/caveman-commit", '---\nname: caveman-commit\n---\n', bundle=True)
        self._write_skill("caveman/caveman-review", '---\nname: caveman-review\n---\n', bundle=True)
        results = boot.walk_skills()
        rels = [rel for rel, _ in results]
        self.assertIn("caveman-commit", rels)
        self.assertIn("caveman-review", rels)

    def test_mixed_skills_and_bundles(self):
        self._write_skill("weather", '---\nname: weather\n---\n')
        self._write_skill("caveman/hook-caveman", '---\nname: hook-caveman\n---\n', bundle=True)
        results = boot.walk_skills()
        rels = [rel for rel, _ in results]
        self.assertIn("weather", rels)
        self.assertIn("hook-caveman", rels)

    def test_skips_dotdirs(self):
        self._write_skill(".hidden", '---\nname: hidden\n---\n')
        self._write_skill("__pycache__", '---\nname: cache\n---\n')
        results = boot.walk_skills()
        self.assertEqual(len(results), 0)

    def test_empty_dir(self):
        boot.SKILLS_DIR = os.path.join(self.tmpdir, "nonexistent")
        results = boot.walk_skills()
        self.assertEqual(results, [])


class TestScanSkills(BootTestBase):
    def test_flat(self):
        self._write_skill("weather", '---\nname: weather\ndescription: "Get weather"\n---\nBody')
        skills = boot.scan_skills()
        self.assertEqual(len(skills), 1)
        self.assertIn("weather", skills[0])
        self.assertIn("Get weather", skills[0])

    def test_bundle_skill(self):
        self._write_skill("caveman/caveman-commit", '---\nname: caveman-commit\ndescription: "Terse commits"\n---\n', bundle=True)
        skills = boot.scan_skills()
        names = " ".join(skills)
        self.assertIn("caveman-commit", names)
        self.assertIn("Terse commits", names)

    def test_no_description_hidden(self):
        self._write_skill("hidden", '---\nname: hidden\n---\n')
        skills = boot.scan_skills()
        self.assertEqual(len(skills), 0)


class TestDiscoverSkillTools(BootTestBase):
    def test_flat_tool(self):
        self._write_skill("weather", '---\ntools: [{"name": "get_weather", "description": "Get weather"}]\n---\n')
        self._write_file("weather/run.py", "")
        tools = boot.discover_skill_tools()
        self.assertEqual(len(tools), 1)
        self.assertEqual(tools[0]["name"], "get_weather")
        self.assertIn("skills/weather/run.py", tools[0]["exec"])

    def test_bundle_tool(self):
        """Bundle tools get skills/ prefix since they're symlinked."""
        self._write_skill("caveman/caveman-compress",
                          '---\ntools: [{"name": "caveman_compress", "description": "Compress"}]\n---\n',
                          bundle=True)
        self._write_file("caveman/caveman-compress/run.py", "", bundle=True)
        tools = boot.discover_skill_tools()
        self.assertEqual(len(tools), 1)
        self.assertEqual(tools[0]["name"], "caveman_compress")
        # Exec path is flat: skills/caveman-compress/run.py
        self.assertIn("skills/caveman-compress/run.py", tools[0]["exec"])

    def test_custom_exec(self):
        self._write_skill("custom", '---\ntools: [{"name": "my_tool", "exec": "node index.js", "description": "Custom"}]\n---\n')
        tools = boot.discover_skill_tools()
        self.assertEqual(tools[0]["exec"], "node index.js")

    def test_duplicate_tool_override(self):
        self._write_skill("a-skill", '---\ntools: [{"name": "dup", "description": "First"}]\n---\n')
        self._write_file("a-skill/run.py", "")
        self._write_skill("b-skill", '---\ntools: [{"name": "dup", "description": "Second"}]\n---\n')
        self._write_file("b-skill/run.py", "")
        tools = boot.discover_skill_tools()
        self.assertEqual(len(tools), 1)
        self.assertEqual(tools[0]["description"], "Second")

    def test_kernel_tool_collision(self):
        self._write_skill("bad", '---\ntools: [{"name": "shell_exec", "description": "Oops"}]\n---\n')
        tools = boot.discover_skill_tools()
        self.assertEqual(len(tools), 0)


class TestDiscoverSlashCommands(BootTestBase):
    def test_flat(self):
        self._write_skill("weather", '---\nname: weather\ndescription: "Weather"\nuser-invocable: true\n---\nBody')
        cmds = boot.discover_slash_commands()
        self.assertEqual(len(cmds), 1)
        self.assertEqual(cmds[0]["name"], "weather")
        self.assertEqual(cmds[0]["body"], "Body")

    def test_bundle_command(self):
        self._write_skill("caveman/caveman-commit",
                          '---\nname: caveman-commit\ndescription: "Commits"\nuser-invocable: true\n---\nCommit body',
                          bundle=True)
        cmds = boot.discover_slash_commands()
        self.assertEqual(len(cmds), 1)
        self.assertEqual(cmds[0]["name"], "caveman-commit")

    def test_not_invocable_skipped(self):
        self._write_skill("internal", '---\nname: internal\ndescription: "Not invocable"\n---\n')
        cmds = boot.discover_slash_commands()
        self.assertEqual(len(cmds), 0)


class TestBuildSpawn(BootTestBase):
    def test_hook_flat(self):
        self._write_skill("hook-logger", '---\nname: hook-logger\ndescription: "Logger"\n---\n')
        self._write_file("hook-logger/run.py", "")
        procs = boot.build_spawn()
        hook_procs = [p for p in procs if "hook-logger" in p]
        self.assertEqual(len(hook_procs), 1)
        self.assertIn("skills/hook-logger/run.py", hook_procs[0])

    def test_hook_in_bundle(self):
        self._write_skill("caveman/hook-caveman", '---\nname: hook-caveman\ndescription: "Caveman hook"\n---\n', bundle=True)
        self._write_file("caveman/hook-caveman/run.py", "", bundle=True)
        procs = boot.build_spawn()
        hook_procs = [p for p in procs if "hook-caveman" in p]
        self.assertEqual(len(hook_procs), 1)
        self.assertIn("skills/hook-caveman/run.py", hook_procs[0])

    def test_no_duplicate_hooks(self):
        self._write_skill("hook-a", '---\nname: hook-a\n---\n')
        self._write_file("hook-a/run.py", "")
        self._write_skill("hook-b", '---\nname: hook-b\n---\n')
        self._write_file("hook-b/run.py", "")
        procs = boot.build_spawn()
        hook_procs = [p for p in procs if "hook-" in p]
        self.assertEqual(len(hook_procs), 2)


class TestIncludeSkill(unittest.TestCase):
    def test_regular_skill(self):
        self.assertTrue(boot.include_skill("weather"))

    def test_active_driver(self):
        self.assertTrue(boot.include_skill(f"driver-{boot.ACTIVE_PROVIDER}"))

    def test_inactive_driver(self):
        self.assertFalse(boot.include_skill("driver-fakeprovider"))

    def test_active_subagent(self):
        self.assertTrue(boot.include_skill(f"subagent-{boot.ACTIVE_PROVIDER}"))

    def test_inactive_subagent(self):
        self.assertFalse(boot.include_skill("subagent-fakeprovider"))


class TestFullDiscovery(BootTestBase):
    """Integration test: flat skills + symlinked bundles."""

    def test_flat_skills_with_bundles(self):
        # Flat skills
        self._write_skill(f"driver-{boot.ACTIVE_PROVIDER}",
                          f'---\nname: driver-{boot.ACTIVE_PROVIDER}\ndescription: "Active driver"\n---\n')
        self._write_file(f"driver-{boot.ACTIVE_PROVIDER}/run.py", "")
        self._write_skill("weather",
                          '---\nname: weather\ndescription: "Weather"\nuser-invocable: true\ntools: [{"name": "get_weather"}]\n---\nWeather body')
        self._write_file("weather/run.py", "")

        # Bundle (each sub-skill symlinked flat into skills/)
        self._write_skill("caveman/caveman",
                          '---\nname: caveman\ndescription: "Caveman mode"\nuser-invocable: true\n---\nBody',
                          bundle=True)
        self._write_skill("caveman/caveman-commit",
                          '---\nname: caveman-commit\ndescription: "Commits"\nuser-invocable: true\n---\nCommit body',
                          bundle=True)
        self._write_skill("caveman/caveman-compress",
                          '---\ntools: [{"name": "caveman_compress", "description": "Compress"}]\ndescription: "Compress"\n---\n',
                          bundle=True)
        self._write_file("caveman/caveman-compress/run.py", "", bundle=True)
        self._write_skill("caveman/hook-caveman",
                          '---\nname: hook-caveman\ndescription: "Hook"\n---\n',
                          bundle=True)
        self._write_file("caveman/hook-caveman/run.py", "", bundle=True)

        # Verify skills list
        skills = boot.scan_skills()
        skill_text = " ".join(skills)
        self.assertIn("caveman", skill_text)
        self.assertIn("caveman-commit", skill_text)
        self.assertIn("weather", skill_text)
        self.assertIn(f"driver-{boot.ACTIVE_PROVIDER}", skill_text)

        # Verify tools — all use skills/ prefix
        tools = boot.discover_skill_tools()
        tool_map = {t["name"]: t for t in tools}
        self.assertIn("caveman_compress", tool_map)
        self.assertIn("get_weather", tool_map)
        self.assertIn("skills/caveman-compress/run.py", tool_map["caveman_compress"]["exec"])
        self.assertIn("skills/weather/run.py", tool_map["get_weather"]["exec"])

        # Verify slash commands
        cmds = boot.discover_slash_commands()
        cmd_names = [c["name"] for c in cmds]
        self.assertIn("caveman", cmd_names)
        self.assertIn("caveman-commit", cmd_names)
        self.assertIn("weather", cmd_names)

        # Verify hook spawn — uses skills/ prefix via symlink
        procs = boot.build_spawn()
        hook_procs = [p for p in procs if "hook-caveman" in p]
        self.assertEqual(len(hook_procs), 1)
        self.assertIn("skills/hook-caveman", hook_procs[0])


class TestLoadPermissions(BootTestBase):
    def setUp(self):
        super().setUp()
        self._orig_perm_file = boot.PERMISSIONS_FILE
        self.perm_file = os.path.join(self.tmpdir, "config", "skills", "hook-permissions", "permissions.json")
        os.makedirs(os.path.dirname(self.perm_file), exist_ok=True)
        boot.PERMISSIONS_FILE = self.perm_file

    def tearDown(self):
        boot.PERMISSIONS_FILE = self._orig_perm_file
        super().tearDown()

    def test_valid(self):
        with open(self.perm_file, "w") as f:
            json.dump({"rules": [
                {"tool": "shell_exec", "command": "rm *", "effect": "deny"},
                {"tool": "*", "effect": "allow"},
            ]}, f)
        rules = boot.load_permissions()
        self.assertEqual(len(rules), 2)
        self.assertEqual(rules[0]["tool"], "shell_exec")
        self.assertEqual(rules[0]["effect"], "deny")

    def test_missing(self):
        rules = boot.load_permissions()
        self.assertEqual(rules, [])

    def test_empty(self):
        with open(self.perm_file, "w") as f:
            f.write("{}")
        rules = boot.load_permissions()
        self.assertEqual(rules, [])

    def test_invalid_json(self):
        with open(self.perm_file, "w") as f:
            f.write("not json")
        rules = boot.load_permissions()
        self.assertEqual(rules, [])


class TestFilterDeniedTools(unittest.TestCase):
    def test_removes_unconditionally_denied(self):
        tools = [
            {"name": "write_file", "description": "Write"},
            {"name": "read_file", "description": "Read"},
            {"name": "shell_exec", "description": "Exec"},
        ]
        perms = [
            {"tool": "write_file", "effect": "deny"},
            {"tool": "*", "effect": "allow"},
        ]
        filtered = boot.filter_denied_tools(tools, perms)
        names = [t["name"] for t in filtered]
        self.assertNotIn("write_file", names)
        self.assertIn("read_file", names)
        self.assertIn("shell_exec", names)

    def test_keeps_conditional_deny(self):
        tools = [{"name": "shell_exec", "description": "Exec"}]
        perms = [
            {"tool": "shell_exec", "command": "rm *", "effect": "deny"},
            {"tool": "*", "effect": "allow"},
        ]
        filtered = boot.filter_denied_tools(tools, perms)
        self.assertEqual(len(filtered), 1)
        self.assertEqual(filtered[0]["name"], "shell_exec")

    def test_glob_deny(self):
        tools = [
            {"name": "write_file", "description": "Write"},
            {"name": "write_config", "description": "Config"},
            {"name": "read_file", "description": "Read"},
        ]
        perms = [{"tool": "write_*", "effect": "deny"}]
        filtered = boot.filter_denied_tools(tools, perms)
        names = [t["name"] for t in filtered]
        self.assertEqual(names, ["read_file"])

    def test_empty_permissions(self):
        tools = [{"name": "shell_exec", "description": "Exec"}]
        filtered = boot.filter_denied_tools(tools, [])
        self.assertEqual(len(filtered), 1)


class TestBuildSpawnPermissions(BootTestBase):
    def setUp(self):
        super().setUp()
        self._orig_perm_file = boot.PERMISSIONS_FILE
        self.perm_file = os.path.join(self.tmpdir, "config", "skills", "hook-permissions", "permissions.json")
        os.makedirs(os.path.dirname(self.perm_file), exist_ok=True)
        boot.PERMISSIONS_FILE = self.perm_file

    def tearDown(self):
        boot.PERMISSIONS_FILE = self._orig_perm_file
        super().tearDown()

    def test_hook_permissions_spawned_when_file_exists(self):
        self._write_skill("hook-permissions", '---\nname: hook-permissions\n---\n')
        self._write_file("hook-permissions/run.py", "")
        with open(self.perm_file, "w") as f:
            json.dump({"rules": [{"tool": "*", "effect": "allow"}]}, f)
        procs = boot.build_spawn()
        perm_procs = [p for p in procs if "hook-permissions" in p]
        self.assertEqual(len(perm_procs), 1)

    def test_hook_permissions_not_spawned_without_file(self):
        self._write_skill("hook-permissions", '---\nname: hook-permissions\n---\n')
        self._write_file("hook-permissions/run.py", "")
        procs = boot.build_spawn()
        perm_procs = [p for p in procs if "hook-permissions" in p]
        self.assertEqual(len(perm_procs), 0)


class TestBootConfigShape(unittest.TestCase):
    def test_config_shape_has_no_system_prompt(self):
        config = {
            "url": "ws://localhost:8089/ws",
            "spawn": [],
            "kernel_tools": ["shell_exec", "process_spawn", "process_kill", "process_list"],
            "tools": [],
            "commands": [],
        }
        self.assertNotIn("system_prompt", config)

    def test_default_kernel_tools_exposed(self):
        self.assertEqual(boot.discover_kernel_tools(), ["shell_exec", "process_spawn", "process_kill", "process_list"])

    def test_kernel_tools_can_be_restricted_by_env(self):
        old = os.environ.get("TABULA_KERNEL_TOOLS")
        try:
            os.environ["TABULA_KERNEL_TOOLS"] = "shell_exec,process_list"
            self.assertEqual(boot.discover_kernel_tools(), ["shell_exec", "process_list"])
        finally:
            if old is None:
                os.environ.pop("TABULA_KERNEL_TOOLS", None)
            else:
                os.environ["TABULA_KERNEL_TOOLS"] = old


class TestLoadEnv(BootTestBase):
    def setUp(self):
        super().setUp()
        self._orig_tabula_home = boot.TABULA_HOME
        self._orig_provider = os.environ.get("TABULA_PROVIDER")
        self._orig_model = os.environ.get("OPENAI_MODEL")
        boot.TABULA_HOME = self.tmpdir

    def tearDown(self):
        boot.TABULA_HOME = self._orig_tabula_home
        if self._orig_provider is None:
            os.environ.pop("TABULA_PROVIDER", None)
        else:
            os.environ["TABULA_PROVIDER"] = self._orig_provider
        if self._orig_model is None:
            os.environ.pop("OPENAI_MODEL", None)
        else:
            os.environ["OPENAI_MODEL"] = self._orig_model
        super().tearDown()

    def test_load_env_populates_missing_values(self):
        with open(os.path.join(self.tmpdir, ".env"), "w") as f:
            f.write("TABULA_PROVIDER=openai\nOPENAI_MODEL=gpt-5.4\n")

        os.environ.pop("TABULA_PROVIDER", None)
        os.environ.pop("OPENAI_MODEL", None)
        boot.load_env()

        self.assertEqual(os.environ.get("TABULA_PROVIDER"), "openai")
        self.assertEqual(os.environ.get("OPENAI_MODEL"), "gpt-5.4")

    def test_load_env_does_not_override_existing_shell_env(self):
        with open(os.path.join(self.tmpdir, ".env"), "w") as f:
            f.write("TABULA_PROVIDER=anthropic\n")

        os.environ["TABULA_PROVIDER"] = "openai"
        boot.load_env()

        self.assertEqual(os.environ.get("TABULA_PROVIDER"), "openai")


if __name__ == "__main__":
    unittest.main()

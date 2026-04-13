#!/usr/bin/env python3
"""Tests for boot.py skill discovery — flat skills + bundles layout."""

import json
import os
import shutil
import sys
import tempfile
import unittest

# Import boot module from repo root
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
import boot


class BootTestBase(unittest.TestCase):
    """Base class that patches SKILLS_DIR and BUNDLES_DIR to temp directories."""

    def setUp(self):
        self.tmpdir = tempfile.mkdtemp()
        self.skills_dir = os.path.join(self.tmpdir, "skills")
        self.bundles_dir = os.path.join(self.tmpdir, "bundles")
        os.makedirs(self.skills_dir)
        os.makedirs(self.bundles_dir)
        self._orig_skills_dir = boot.SKILLS_DIR
        self._orig_bundles_dir = boot.BUNDLES_DIR
        boot.SKILLS_DIR = self.skills_dir
        boot.BUNDLES_DIR = self.bundles_dir

    def tearDown(self):
        boot.SKILLS_DIR = self._orig_skills_dir
        boot.BUNDLES_DIR = self._orig_bundles_dir
        shutil.rmtree(self.tmpdir)

    def _write_skill(self, rel_path, content, bundle=False):
        """Write a SKILL.md at the given relative path under skills or bundles."""
        base = self.bundles_dir if bundle else self.skills_dir
        full = os.path.join(base, rel_path, "SKILL.md")
        os.makedirs(os.path.dirname(full), exist_ok=True)
        with open(full, "w") as f:
            f.write(content)

    def _write_file(self, rel_path, content="", bundle=False):
        """Write an arbitrary file under skills or bundles."""
        base = self.bundles_dir if bundle else self.skills_dir
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


class TestWalkSkills(BootTestBase):
    def test_flat_layout(self):
        self._write_skill("weather", '---\nname: weather\n---\n')
        self._write_skill("files", '---\nname: files\n---\n')
        results = boot.walk_skills()
        names = [os.path.basename(rel) for _, rel, _ in results]
        self.assertIn("weather", names)
        self.assertIn("files", names)
        # All should have "skills" prefix
        prefixes = {prefix for prefix, _, _ in results}
        self.assertEqual(prefixes, {"skills"})

    def test_bundles(self):
        self._write_skill("caveman/caveman-commit", '---\nname: caveman-commit\n---\n', bundle=True)
        self._write_skill("caveman/caveman-review", '---\nname: caveman-review\n---\n', bundle=True)
        results = boot.walk_skills()
        for prefix, rel, _ in results:
            self.assertEqual(prefix, "bundles")
        rels = [rel for _, rel, _ in results]
        self.assertIn(os.path.join("caveman", "caveman-commit"), rels)
        self.assertIn(os.path.join("caveman", "caveman-review"), rels)

    def test_mixed_skills_and_bundles(self):
        self._write_skill("weather", '---\nname: weather\n---\n')
        self._write_skill("caveman/hook-caveman", '---\nname: hook-caveman\n---\n', bundle=True)
        results = boot.walk_skills()
        prefixes = {prefix for prefix, _, _ in results}
        self.assertEqual(prefixes, {"skills", "bundles"})

    def test_skips_dotdirs(self):
        self._write_skill(".hidden", '---\nname: hidden\n---\n')
        self._write_skill("__pycache__", '---\nname: cache\n---\n')
        results = boot.walk_skills()
        self.assertEqual(len(results), 0)

    def test_empty_dirs(self):
        boot.SKILLS_DIR = os.path.join(self.tmpdir, "nonexistent")
        boot.BUNDLES_DIR = os.path.join(self.tmpdir, "also_nonexistent")
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
        self._write_skill("caveman/caveman-compress",
                          '---\ntools: [{"name": "caveman_compress", "description": "Compress"}]\n---\n',
                          bundle=True)
        self._write_file("caveman/caveman-compress/run.py", "", bundle=True)
        tools = boot.discover_skill_tools()
        self.assertEqual(len(tools), 1)
        self.assertEqual(tools[0]["name"], "caveman_compress")
        self.assertIn("bundles/caveman/caveman-compress/run.py", tools[0]["exec"])

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
        self._write_skill("bad", '---\ntools: [{"name": "EXEC", "description": "Oops"}]\n---\n')
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
        self.assertIn("bundles/caveman/hook-caveman/run.py", hook_procs[0])

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
    """Integration test: flat skills + bundles layout like the actual repo."""

    def test_flat_skills_with_bundles(self):
        # Flat skills
        self._write_skill(f"driver-{boot.ACTIVE_PROVIDER}",
                          f'---\nname: driver-{boot.ACTIVE_PROVIDER}\ndescription: "Active driver"\n---\n')
        self._write_file(f"driver-{boot.ACTIVE_PROVIDER}/run.py", "")
        self._write_skill("weather",
                          '---\nname: weather\ndescription: "Weather"\nuser-invocable: true\ntools: [{"name": "get_weather"}]\n---\nWeather body')
        self._write_file("weather/run.py", "")

        # Bundle
        self._write_skill("caveman",
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

        # Verify tools — flat skill uses skills/ prefix, bundle uses bundles/
        tools = boot.discover_skill_tools()
        tool_map = {t["name"]: t for t in tools}
        self.assertIn("caveman_compress", tool_map)
        self.assertIn("get_weather", tool_map)
        self.assertIn("bundles/caveman/caveman-compress/run.py", tool_map["caveman_compress"]["exec"])
        self.assertIn("skills/weather/run.py", tool_map["get_weather"]["exec"])

        # Verify slash commands
        cmds = boot.discover_slash_commands()
        cmd_names = [c["name"] for c in cmds]
        self.assertIn("caveman", cmd_names)
        self.assertIn("caveman-commit", cmd_names)
        self.assertIn("weather", cmd_names)

        # Verify hook spawn from bundle
        procs = boot.build_spawn()
        hook_procs = [p for p in procs if "hook-caveman" in p]
        self.assertEqual(len(hook_procs), 1)
        self.assertIn("bundles/caveman/hook-caveman", hook_procs[0])


if __name__ == "__main__":
    unittest.main()

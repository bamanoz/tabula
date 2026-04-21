#!/usr/bin/env python3
"""Tests for skills/files/run.py semantics."""

from __future__ import annotations

import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import sys

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

import importlib.util


FILES_RUN_PATH = ROOT / "distrib" / "familiar" / "skills" / "files" / "run.py"
spec = importlib.util.spec_from_file_location("tabula_files_run", FILES_RUN_PATH)
_files = importlib.util.module_from_spec(spec)
assert spec.loader is not None
spec.loader.exec_module(_files)

tool_read = _files.tool_read
tool_list_dir = _files.tool_list_dir
tool_glob = _files.tool_glob
tool_grep = _files.tool_grep
tool_write = _files.tool_write
tool_edit = _files.tool_edit
tool_multiedit = _files.tool_multiedit
tool_apply_patch = _files.tool_apply_patch
RG_REQUIRED_ERROR = _files.RG_REQUIRED_ERROR


class TestFilesSkill(unittest.TestCase):
    def _rg_available(self) -> bool:
        return _files.shutil.which("rg") is not None

    def test_write_new_file_without_prior_read(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "note.txt"

            result = tool_write({"path": str(path), "content": "new\ntext\n"})

            self.assertEqual(result, f"Wrote 2 lines to {path}")
            self.assertEqual(path.read_text(encoding="utf-8"), "new\ntext\n")

    def test_write_existing_file_requires_full_read(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "note.txt"
            path.write_text("old\n", encoding="utf-8")

            error = json.loads(tool_write({"path": str(path), "content": "new\n"}))

            self.assertIn("file was not fully read first", error["error"])

    def test_edit_requires_read_first(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "note.txt"
            path.write_text("hello\n", encoding="utf-8")

            error = json.loads(tool_edit({"path": str(path), "old_string": "hello", "new_string": "hi"}))

            self.assertIn("file was not fully read first", error["error"])

    def test_edit_after_full_read_succeeds(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "note.txt"
            path.write_text("hello\nworld\n", encoding="utf-8")

            read_result = tool_read({"path": str(path)})
            edit_result = tool_edit({"path": str(path), "old_string": "world", "new_string": "tabula"})

            self.assertIn("<path>", read_result)
            self.assertEqual(edit_result, f"Replaced 1 occurrence(s) in {path}")
            self.assertEqual(path.read_text(encoding="utf-8"), "hello\ntabula\n")

    def test_edit_detects_stale_read(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "note.txt"
            path.write_text("hello\nworld\n", encoding="utf-8")

            tool_read({"path": str(path)})
            path.write_text("hello\nplanet\n", encoding="utf-8")

            error = json.loads(tool_edit({"path": str(path), "old_string": "planet", "new_string": "tabula"}))

            self.assertIn("file has been modified since it was read", error["error"])

    def test_partial_read_does_not_authorize_edit(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "note.txt"
            path.write_text("one\ntwo\nthree\n", encoding="utf-8")

            tool_read({"path": str(path), "limit": 1})
            error = json.loads(tool_edit({"path": str(path), "old_string": "one", "new_string": "ONE"}))

            self.assertIn("file was not fully read first", error["error"])

    def test_multiedit_applies_multiple_replacements(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "note.txt"
            path.write_text("alpha\nbeta\ngamma\n", encoding="utf-8")

            tool_read({"path": str(path)})
            result = tool_multiedit(
                {
                    "path": str(path),
                    "edits": [
                        {"old_string": "alpha", "new_string": "ALPHA"},
                        {"old_string": "gamma", "new_string": "GAMMA"},
                    ],
                }
            )

            self.assertEqual(result, f"Applied 2 edit(s) with 2 replacement(s) to {path}")
            self.assertEqual(path.read_text(encoding="utf-8"), "ALPHA\nbeta\nGAMMA\n")

    def test_read_rejects_directory(self):
        with tempfile.TemporaryDirectory() as tmp:
            error = json.loads(tool_read({"path": tmp}))
            self.assertIn("use list_dir instead", error["error"])

    def test_list_dir_marks_directories_and_symlinks(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "dir").mkdir()
            (root / "file.txt").write_text("x", encoding="utf-8")
            os.symlink(root / "file.txt", root / "link.txt")

            result = tool_list_dir({"path": str(root)})

            self.assertIn("dir/", result)
            self.assertIn("file.txt", result)
            self.assertIn("link.txt@", result)

    def test_list_dir_supports_depth_and_pagination(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            nested = root / "a" / "b"
            nested.mkdir(parents=True)
            (root / "root.txt").write_text("x", encoding="utf-8")
            (nested / "deep.txt").write_text("y", encoding="utf-8")

            shallow = tool_list_dir({"path": str(root), "depth": 1})
            deep = tool_list_dir({"path": str(root), "depth": 3, "offset": 2, "limit": 2})

            self.assertNotIn("a/b/", shallow)
            self.assertIn("a/b/", deep)
            self.assertIn("Use offset=4 to continue", deep)

    def test_glob_requires_rg(self):
        with tempfile.TemporaryDirectory() as tmp:
            with patch.object(_files.shutil, "which", return_value=None):
                error = json.loads(tool_glob({"pattern": "*.txt", "path": tmp}))

            self.assertEqual(error["error"], RG_REQUIRED_ERROR)

    def test_glob_returns_matching_paths(self):
        if not self._rg_available():
            self.skipTest("rg not installed")

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "a.py").write_text("print('a')\n", encoding="utf-8")
            (root / "b.txt").write_text("b\n", encoding="utf-8")
            sub = root / "sub"
            sub.mkdir()
            (sub / "c.py").write_text("print('c')\n", encoding="utf-8")

            result = tool_glob({"pattern": "**/*.py", "path": str(root)})

            self.assertIn(str(root / "a.py"), result)
            self.assertIn(str(sub / "c.py"), result)
            self.assertNotIn(str(root / "b.txt"), result)

    def test_grep_requires_rg(self):
        with tempfile.TemporaryDirectory() as tmp:
            with patch.object(_files.shutil, "which", return_value=None):
                error = json.loads(tool_grep({"pattern": "hello", "path": tmp}))

            self.assertEqual(error["error"], RG_REQUIRED_ERROR)

    def test_grep_lines_files_and_count_modes(self):
        if not self._rg_available():
            self.skipTest("rg not installed")

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "one.py").write_text("alpha\nbeta\n", encoding="utf-8")
            (root / "two.py").write_text("alpha\nalpha\n", encoding="utf-8")
            (root / "skip.txt").write_text("alpha\n", encoding="utf-8")

            lines = tool_grep({"pattern": "alpha", "path": str(root), "include": "*.py", "mode": "lines"})
            files = tool_grep({"pattern": "alpha", "path": str(root), "include": "*.py", "mode": "files"})
            counts = tool_grep({"pattern": "alpha", "path": str(root), "include": "*.py", "mode": "count"})

            self.assertIn(str(root / "one.py") + ":", lines)
            self.assertIn(str(root / "two.py"), files)
            self.assertNotIn(str(root / "skip.txt"), files)
            self.assertIn(f"{root / 'one.py'}: 1", counts)
            self.assertIn(f"{root / 'two.py'}: 2", counts)

    def test_apply_patch_updates_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "note.txt"
            path.write_text("before\nafter\n", encoding="utf-8")

            result = tool_apply_patch(
                {
                    "patch_text": "\n".join(
                        [
                            "*** Begin Patch",
                            f"*** Update File: {path}",
                            "@@",
                            "-before",
                            "+changed",
                            " after",
                            "*** End Patch",
                        ]
                    )
                }
            )

            self.assertIn("Applied patch:", result)
            self.assertEqual(path.read_text(encoding="utf-8"), "changed\nafter\n")

    def test_apply_patch_adds_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "new.txt"

            result = tool_apply_patch(
                {
                    "patch_text": "\n".join(
                        [
                            "*** Begin Patch",
                            f"*** Add File: {path}",
                            "+hello",
                            "+world",
                            "*** End Patch",
                        ]
                    )
                }
            )

            self.assertIn(f"A {path}", result)
            self.assertEqual(path.read_text(encoding="utf-8"), "hello\nworld")

    def test_apply_patch_deletes_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "gone.txt"
            path.write_text("delete me\n", encoding="utf-8")

            result = tool_apply_patch(
                {
                    "patch_text": "\n".join(
                        [
                            "*** Begin Patch",
                            f"*** Delete File: {path}",
                            "*** End Patch",
                        ]
                    )
                }
            )

            self.assertIn(f"D {path}", result)
            self.assertFalse(path.exists())

    def test_apply_patch_moves_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            source = Path(tmp) / "old.txt"
            target = Path(tmp) / "new.txt"
            source.write_text("hello\n", encoding="utf-8")

            result = tool_apply_patch(
                {
                    "patch_text": "\n".join(
                        [
                            "*** Begin Patch",
                            f"*** Update File: {source}",
                            f"*** Move to: {target}",
                            "@@",
                            " hello",
                            "*** End Patch",
                        ]
                    )
                }
            )

            self.assertIn(f"R {source} -> {target}", result)
            self.assertFalse(source.exists())
            self.assertEqual(target.read_text(encoding="utf-8"), "hello\n")

    def test_apply_patch_rejects_context_mismatch(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "note.txt"
            path.write_text("before\nafter\n", encoding="utf-8")

            error = json.loads(
                tool_apply_patch(
                    {
                        "patch_text": "\n".join(
                            [
                                "*** Begin Patch",
                                f"*** Update File: {path}",
                                "@@",
                                "-missing",
                                "+changed",
                                " after",
                                "*** End Patch",
                            ]
                        )
                    }
                )
            )

            self.assertIn("hunk context not found", error["error"])
            self.assertEqual(path.read_text(encoding="utf-8"), "before\nafter\n")


if __name__ == "__main__":
    unittest.main()

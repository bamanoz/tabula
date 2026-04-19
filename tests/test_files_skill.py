#!/usr/bin/env python3
"""Tests for skills/files/run.py semantics."""

from __future__ import annotations

import json
import os
import tempfile
import unittest
from pathlib import Path


import sys

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from skills.files.run import tool_read_file, tool_str_replace, tool_write_file


class TestFilesSkill(unittest.TestCase):
    def test_write_file_overwrites_without_prior_read(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "note.txt"
            path.write_text("old\n", encoding="utf-8")

            result = tool_write_file({"path": str(path), "content": "new\ntext\n"})

            self.assertEqual(result, f"Wrote 2 lines to {path}")
            self.assertEqual(path.read_text(encoding="utf-8"), "new\ntext\n")

    def test_str_replace_still_requires_read_first(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "note.txt"
            path.write_text("hello\n", encoding="utf-8")

            error = json.loads(tool_str_replace({"path": str(path), "old_string": "hello", "new_string": "hi"}))

            self.assertIn("file was not read first", error["error"])

    def test_write_marks_file_read_for_later_replace(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "note.txt"
            path.write_text("hello\n", encoding="utf-8")

            write_result = tool_write_file({"path": str(path), "content": "hello\nworld\n"})
            replace_result = tool_str_replace({"path": str(path), "old_string": "world", "new_string": "табула"})

            self.assertEqual(write_result, f"Wrote 2 lines to {path}")
            self.assertEqual(replace_result, f"Replaced 1 occurrence(s) in {path}")
            self.assertEqual(path.read_text(encoding="utf-8"), "hello\nтабула\n")


if __name__ == "__main__":
    unittest.main()

#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
from pathlib import Path
import shutil
import tempfile
import unittest

from tabula_testbed import TestbedClient


class FilesBundleSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"

    def setUp(self) -> None:
        self.workdir = Path(tempfile.mkdtemp(prefix="tabula-files-testbed."))

    def tearDown(self) -> None:
        shutil.rmtree(self.workdir, ignore_errors=True)

    def make_client(self, name: str, session: str = "testbed-files") -> TestbedClient:
        client = TestbedClient(self.url, name=name)
        client.connect_join(session)
        return client

    def test_files_tools_execute_installed_behavior(self):
        target = self.workdir / "notes.txt"
        direct = self.workdir / "direct.txt"
        direct.write_text("direct old\n", encoding="utf-8")

        with self.make_client("testbed-files-main") as client:
            required = {"read", "list_dir", "glob", "grep", "write", "edit", "multiedit", "apply_patch"}
            client.wait_tools(required, session="testbed-files")

            read_dir = client.call_tool("read", {"path": str(self.workdir)}).json()
            self.assertIn("path is a directory", read_dir.get("error", ""))

            not_read = client.call_tool("edit", {
                "path": str(direct),
                "old_string": "old",
                "new_string": "new",
            }).json()
            self.assertIn("file was not fully read first", not_read.get("error", ""))

            written = client.call_tool("write", {
                "path": str(target),
                "content": "alpha\nbeta\nneedle\n",
            }).output
            self.assertIn("Wrote 3 lines", written)

            read = client.call_tool("read", {"path": str(target)}).output
            self.assertIn("1: alpha", read)
            self.assertIn("3: needle", read)

            edited = client.call_tool("edit", {
                "path": str(target),
                "old_string": "beta",
                "new_string": "BETA",
            }).output
            self.assertIn("Replaced 1 occurrence", edited)

            multiedited = client.call_tool("multiedit", {
                "path": str(target),
                "edits": [
                    {"old_string": "alpha", "new_string": "ALPHA"},
                    {"old_string": "needle", "new_string": "haystack"},
                ],
            }).output
            self.assertIn("Applied 2 edit(s)", multiedited)

            read = client.call_tool("read", {"path": str(target)}).output
            self.assertIn("1: ALPHA", read)
            self.assertIn("2: BETA", read)
            self.assertIn("3: haystack", read)

            listed = client.call_tool("list_dir", {"path": str(self.workdir)}).output
            self.assertIn("notes.txt", listed)
            self.assertIn("direct.txt", listed)

            globbed = client.call_tool("glob", {"path": str(self.workdir), "pattern": "*.txt"}).output
            self.assertIn(str(target), globbed)

            grepped = client.call_tool("grep", {
                "path": str(self.workdir),
                "pattern": "haystack",
                "include": "*.txt",
            }).output
            self.assertIn("Found 1 matches", grepped)
            self.assertIn(str(target), grepped)

            added = self.workdir / "patch-added.txt"
            patched = client.call_tool("apply_patch", {"patch_text": f"""*** Begin Patch
*** Add File: {added}
+patch content
+needle patch
*** Update File: {target}
@@
 ALPHA
-BETA
+BETA!
 haystack
*** End Patch"""}).output
            self.assertIn("Applied patch", patched)
            self.assertTrue(added.is_file())
            self.assertIn("BETA!", target.read_text(encoding="utf-8"))

            deleted = client.call_tool("apply_patch", {"patch_text": f"""*** Begin Patch
*** Delete File: {added}
*** End Patch"""}).output
            self.assertIn("Applied patch", deleted)
            self.assertFalse(added.exists())


def main() -> int:
    parser = argparse.ArgumentParser(description="Run files testbed smoke tests")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    FilesBundleSmoke.url = args.url
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(FilesBundleSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

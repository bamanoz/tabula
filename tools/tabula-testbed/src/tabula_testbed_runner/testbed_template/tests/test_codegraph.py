#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import unittest

from tabula_testbed import TestbedClient


class CodeGraphSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def make_client(self) -> TestbedClient:
        client = TestbedClient(self.url, name="testbed-codegraph")
        client.connect_join("testbed-codegraph")
        return client

    def configure_fake_codegraph(self) -> Path:
        home = Path(self.tabula_home)
        project = home / "codegraph-project"
        project.mkdir(parents=True, exist_ok=True)
        fake = home / "bin" / "fake-codegraph"
        fake.parent.mkdir(parents=True, exist_ok=True)
        fake.write_text(
            "#!/usr/bin/env python3\n"
            "import json, sys\n"
            "if len(sys.argv) > 1 and sys.argv[1] == 'serve':\n"
            "    project = sys.argv[4] if len(sys.argv) > 4 else ''\n"
            "    for line in sys.stdin:\n"
            "        msg = json.loads(line)\n"
            "        if msg.get('method') == 'initialize':\n"
            "            print(json.dumps({'jsonrpc':'2.0','id':msg['id'],'result':{'capabilities':{}}}), flush=True)\n"
            "        elif msg.get('method') == 'tools/list':\n"
            "            print(json.dumps({'jsonrpc':'2.0','id':msg['id'],'result':{'tools':[]}}), flush=True)\n"
            "        elif msg.get('method') == 'tools/call':\n"
            "            payload = {'tool': msg['params']['name'], 'arguments': msg['params'].get('arguments'), 'project': project}\n"
            "            print(json.dumps({'jsonrpc':'2.0','id':msg['id'],'result':{'content':[{'type':'text','text':json.dumps(payload)}]}}), flush=True)\n"
            "    raise SystemExit(0)\n"
            "print(' '.join(sys.argv[1:]))\n",
            encoding="utf-8",
        )
        fake.chmod(0o755)
        for plugin in ("codegraph-query", "codegraph-admin"):
            cfg = home / "config" / "plugins" / plugin / "config.toml"
            cfg.parent.mkdir(parents=True, exist_ok=True)
            cfg.write_text(f'command = "{fake}"\ndefault_project_path = "{project}"\ntimeout_seconds = 20\n', encoding="utf-8")
        return project

    def test_codegraph_installed_wrappers_execute(self):
        project = self.configure_fake_codegraph()
        with self.make_client() as client:
            client.wait_tools({"codegraph_explore", "codegraph_status", "codegraph_sync"}, session="testbed-codegraph")
            explored = client.call_tool("codegraph_explore", {"query": "AuthService login"}, timeout=30).json()
            self.assertTrue(explored.get("success"), explored)
            payload = json.loads(explored["content"][0]["text"])
            self.assertEqual(payload["tool"], "codegraph_explore")
            self.assertEqual(payload["arguments"], {"query": "AuthService login"})
            self.assertEqual(Path(payload["project"]).resolve(), project.resolve())

            synced = client.call_tool("codegraph_sync", {"quiet": True}, timeout=30).json()
            self.assertTrue(synced.get("success"), synced)
            self.assertIn("sync", synced.get("stdout", ""))


def main() -> int:
    parser = argparse.ArgumentParser(description="Run CodeGraph testbed smoke tests")
    parser.add_argument("--url", default=os.environ.get("TABULA_URL", "ws://localhost:8089/ws"))
    parser.add_argument("--observer-url", default="")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    CodeGraphSmoke.url = args.url
    CodeGraphSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(CodeGraphSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import unittest

from tabula_testbed import TestbedClient


class OpenSpecSmoke(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    def make_client(self) -> TestbedClient:
        client = TestbedClient(self.url, name="testbed-openspec")
        client.connect()
        client.create_session("testbed-openspec")
        return client

    def configure_fake_openspec(self) -> Path:
        home = Path(self.tabula_home)
        project = home / "openspec-project"
        project.mkdir(parents=True, exist_ok=True)
        fake = home / "bin" / ("fake-openspec.cmd" if os.name == "nt" else "fake-openspec.py")
        fake.parent.mkdir(parents=True, exist_ok=True)
        if os.name == "nt":
            root = json.dumps(str(project))
            fake.write_text(
                "@echo off\n"
                "if \"%*\"==\"list --json\" goto list\n"
                "if \"%*\"==\"list --specs --json\" goto specs\n"
                "if \"%*\"==\"status --change add-demo --json\" goto status\n"
                "if \"%*\"==\"instructions proposal --change add-demo --json\" goto instructions\n"
                "if \"%*\"==\"instructions apply --change add-demo --json\" goto apply\n"
                "if \"%*\"==\"validate --json\" goto validate\n"
                "if \"%*\"==\"new change add-demo --json\" goto new_change\n"
                "if \"%*\"==\"archive add-demo --json\" goto archive\n"
                "echo {\"error\":\"unexpected args\"} 1>&2\n"
                "exit /b 9\n"
                ":list\n"
                f"echo {{\"changes\":[{{\"name\":\"add-demo\",\"status\":\"active\"}}],\"root\":{{\"path\":{root},\"source\":\"implicit\"}}}}\n"
                "exit /b 0\n"
                ":specs\n"
                f"echo {{\"specs\":[{{\"name\":\"demo\"}}],\"root\":{{\"path\":{root},\"source\":\"implicit\"}}}}\n"
                "exit /b 0\n"
                ":status\n"
                f"echo {{\"change\":\"add-demo\",\"artifacts\":[],\"root\":{{\"path\":{root},\"source\":\"implicit\"}}}}\n"
                "exit /b 0\n"
                ":instructions\n"
                "echo {\"change\":\"add-demo\",\"artifact\":\"proposal\",\"resolvedOutputPath\":\"openspec/changes/add-demo/proposal.md\",\"instructions\":\"write proposal\"}\n"
                "exit /b 0\n"
                ":apply\n"
                "echo {\"change\":\"add-demo\",\"phase\":\"apply\",\"instructions\":\"implement tasks\"}\n"
                "exit /b 0\n"
                ":validate\n"
                f"echo {{\"valid\":true,\"root\":{{\"path\":{root},\"source\":\"implicit\"}}}}\n"
                "exit /b 0\n"
                ":new_change\n"
                f"echo {{\"change\":\"add-demo\",\"created\":true,\"root\":{{\"path\":{root},\"source\":\"implicit\"}}}}\n"
                "exit /b 0\n"
                ":archive\n"
                "echo {\"change\":\"add-demo\",\"archived\":true,\"archivePath\":\"openspec/changes/archive/2099-01-01-add-demo\"}\n"
                "exit /b 0\n",
                encoding="ascii",
            )
        else:
            fake.write_text(
                "#!/usr/bin/env python3\n"
                "import json, os, sys\n"
                "args = sys.argv[1:]\n"
                "root = os.getcwd()\n"
                "responses = {\n"
                " ('list', '--json'): {'changes': [{'name': 'add-demo', 'status': 'active'}], 'root': {'path': root, 'source': 'implicit'}},\n"
                " ('list', '--specs', '--json'): {'specs': [{'name': 'demo'}], 'root': {'path': root, 'source': 'implicit'}},\n"
                " ('status', '--change', 'add-demo', '--json'): {'change': 'add-demo', 'artifacts': [], 'root': {'path': root, 'source': 'implicit'}},\n"
                " ('instructions', 'proposal', '--change', 'add-demo', '--json'): {'change': 'add-demo', 'artifact': 'proposal', 'resolvedOutputPath': 'openspec/changes/add-demo/proposal.md', 'instructions': 'write proposal'},\n"
                " ('instructions', 'apply', '--change', 'add-demo', '--json'): {'change': 'add-demo', 'phase': 'apply', 'instructions': 'implement tasks'},\n"
                " ('validate', '--json'): {'valid': True, 'root': {'path': root, 'source': 'implicit'}},\n"
                " ('new', 'change', 'add-demo', '--json'): {'change': 'add-demo', 'created': True, 'root': {'path': root, 'source': 'implicit'}},\n"
                " ('archive', 'add-demo', '--json'): {'change': 'add-demo', 'archived': True, 'archivePath': 'openspec/changes/archive/2099-01-01-add-demo'},\n"
                "}\n"
                "payload = responses.get(tuple(args))\n"
                "if payload is None:\n"
                "    print(json.dumps({'error': 'unexpected args', 'args': args}), file=sys.stderr)\n"
                "    raise SystemExit(9)\n"
                "print(json.dumps(payload))\n",
                encoding="utf-8",
            )
        fake.chmod(0o755)
        cfg = home / "config" / "plugins" / "openspec-cli" / "config.toml"
        cfg.parent.mkdir(parents=True, exist_ok=True)
        cfg.write_text(f"command = {json.dumps(str(fake))}\ntimeout_ms = 30000\n", encoding="utf-8")
        return project

    def test_openspec_cli_wrappers_execute(self):
        project = self.configure_fake_openspec()
        with self.make_client() as client:
            listed = client.call_tool("openspec_list", {"cwd": str(project)}, timeout=40).json()
            self.assertTrue(listed["ok"], listed)
            self.assertEqual(listed["data"]["changes"][0]["name"], "add-demo")
            self.assertEqual(Path(listed["data"]["root"]["path"]).resolve(), project.resolve())

            specs = client.call_tool("openspec_list", {"cwd": str(project), "specs": True}, timeout=40).json()
            self.assertTrue(specs["ok"], specs)
            self.assertEqual(specs["data"]["specs"][0]["name"], "demo")

            status = client.call_tool("openspec_status", {"cwd": str(project), "change": "add-demo"}, timeout=40).json()
            self.assertTrue(status["ok"], status)
            self.assertEqual(status["data"]["change"], "add-demo")

            instructions = client.call_tool(
                "openspec_instructions",
                {"cwd": str(project), "change": "add-demo", "artifact": "proposal"},
                timeout=40,
            ).json()
            self.assertTrue(instructions["ok"], instructions)
            self.assertEqual(instructions["data"]["resolvedOutputPath"], "openspec/changes/add-demo/proposal.md")

            apply = client.call_tool("openspec_apply_instructions", {"cwd": str(project), "change": "add-demo"}, timeout=40).json()
            self.assertTrue(apply["ok"], apply)
            self.assertEqual(apply["data"]["phase"], "apply")

            created = client.call_tool("openspec_new_change", {"cwd": str(project), "name": "add-demo"}, timeout=40).json()
            self.assertTrue(created["ok"], created)
            self.assertTrue(created["data"]["created"])

            validated = client.call_tool("openspec_validate", {"cwd": str(project)}, timeout=40).json()
            self.assertTrue(validated["ok"], validated)
            self.assertTrue(validated["data"]["valid"])

            archived = client.call_tool("openspec_archive", {"cwd": str(project), "name": "add-demo"}, timeout=40).json()
            self.assertTrue(archived["ok"], archived)
            self.assertTrue(archived["data"]["archived"])


def main() -> int:
    parser = argparse.ArgumentParser(description="Run OpenSpec testbed smoke tests")
    parser.add_argument("--url", default=os.environ.get("TABULA_URL", "ws://localhost:8089/ws"))
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    OpenSpecSmoke.url = args.url
    OpenSpecSmoke.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(OpenSpecSmoke))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

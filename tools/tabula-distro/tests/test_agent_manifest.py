from __future__ import annotations

import tempfile
import tomllib
import unittest
from contextlib import redirect_stderr, redirect_stdout
from io import StringIO
from pathlib import Path
from unittest import mock

from tabula_distro import agent_cli

from test_agent_cli import _make_distro, _write


class AgentManifestTests(unittest.TestCase):
    def _run(self, argv: list[str]) -> tuple[int, str, str]:
        stdout = StringIO()
        stderr = StringIO()
        with redirect_stdout(stdout), redirect_stderr(stderr):
            code = agent_cli.main(argv)
        return code, stdout.getvalue(), stderr.getvalue()

    def test_init_creates_minimal_manifest(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            project = root / "project"
            project.mkdir()
            distro = _make_distro(root)

            code, _out, err = self._run([
                "--home", str(root / "home"), "init",
                "--root", str(project), "--distro", str(distro),
            ])

            self.assertEqual(code, 0, err)
            data = tomllib.loads((project / "tabula.agent.toml").read_text(encoding="utf-8"))
            self.assertEqual(data, {"distro": {"source": str(distro)}, "values": {}})
            serialized = (project / "tabula.agent.toml").read_text(encoding="utf-8")
            for forbidden in ("tenant", "binding", "kernel", "runtime", "secret"):
                self.assertNotIn(forbidden, serialized.lower())

    def test_apply_installs_distinct_local_tenants_for_two_clones(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = _make_distro(root)
            clone_a = root / "clone-a"
            clone_b = root / "clone-b"
            clone_a.mkdir()
            clone_b.mkdir()
            manifest = f'[distro]\nsource = "{distro}"\n\n[values]\nmode = "strict"\n'
            _write(clone_a / "tabula.agent.toml", manifest)
            _write(clone_b / "tabula.agent.toml", manifest)

            with mock.patch.object(
                agent_cli.secrets, "token_hex", side_effect=["aaaaaaaaaaaa", "bbbbbbbbbbbb"]
            ):
                self.assertEqual(self._run([
                    "--home", str(home), "apply", "--root", str(clone_a), "--no-start",
                ])[0], 0)
                self.assertEqual(self._run([
                    "--home", str(home), "apply", "--root", str(clone_b), "--no-start",
                ])[0], 0)

            registry = tomllib.loads((home / "bindings.toml").read_text(encoding="utf-8"))
            self.assertEqual(
                {entry["tenant"] for entry in registry["directory"]},
                {"agent-aaaaaaaaaaaa", "agent-bbbbbbbbbbbb"},
            )
            for tenant in ("agent-aaaaaaaaaaaa", "agent-bbbbbbbbbbbb"):
                values = tomllib.loads((home / "tenants" / tenant / "values.toml").read_text(encoding="utf-8"))
                self.assertEqual(values, {"mode": "strict"})

    def test_apply_rematerializes_selected_same_source_tenant(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            project = root / "project"
            project.mkdir()
            distro = _make_distro(root)
            _write(project / "tabula.agent.toml", f'[distro]\nsource = "{distro}"\n\n[values]\nmode = "one"\n')
            args = ["--home", str(home), "apply", "--root", str(project), "--no-start"]

            with mock.patch.object(agent_cli.secrets, "token_hex", return_value="cccccccccccc"):
                self.assertEqual(self._run(args)[0], 0)
            _write(project / "tabula.agent.toml", f'[distro]\nsource = "{distro}"\n\n[values]\nmode = "two"\n')
            code, out, err = self._run(args)

            self.assertEqual(code, 0, err)
            self.assertIn("using existing tenant agent-cccccccccccc", out)
            values = tomllib.loads((home / "tenants" / "agent-cccccccccccc" / "values.toml").read_text(encoding="utf-8"))
            self.assertEqual(values, {"mode": "two"})
            self.assertEqual(len(list((home / "tenants").iterdir())), 1)

    def test_apply_rejects_host_topology_fields(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            project = root / "project"
            project.mkdir()
            _write(
                project / "tabula.agent.toml",
                '[distro]\nsource = "git+https://example.invalid/d.git@main"\n\n[kernel]\nurl = "ws://example"\n',
            )

            code, _out, err = self._run([
                "--home", str(root / "home"), "apply", "--root", str(project), "--no-start",
            ])

            self.assertEqual(code, 1)
            self.assertIn("unsupported agent manifest section: kernel", err)


if __name__ == "__main__":
    unittest.main()

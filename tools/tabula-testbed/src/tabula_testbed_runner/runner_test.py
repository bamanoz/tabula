from __future__ import annotations

import json
import os
import unittest
from unittest import mock
from pathlib import Path
from tempfile import TemporaryDirectory

from tabula_testbed_runner.runner import (
    attached_runtime_pid,
    describe_testbed_sources,
    entry_protocol_markers,
    format_protocol_markers,
    lint_selection,
    manifest_launch_details,
    process_alive,
    prune_old_testbed_homes,
    resolve_selection,
    runtime_tenants_for_suites,
    selected_bundles,
    set_runtime_tenants,
    SuiteSpec,
    virtualenv_layout,
    verify_kernel_config,
    verify_runtime_sidecar_layout,
    wait_for_supervised_runtime,
)


class RunnerProtocolMarkerTests(unittest.TestCase):
    def test_process_alive_detects_current_process(self) -> None:
        self.assertTrue(process_alive(os.getpid()))

    def test_virtualenv_layout_uses_platform_specific_context(self) -> None:
        with TemporaryDirectory() as raw:
            python, bin_dir = virtualenv_layout(Path(raw) / ".venv")

            self.assertEqual(python.parent, bin_dir)
            self.assertEqual(bin_dir.name, "Scripts" if os.name == "nt" else "bin")
            self.assertEqual(python.suffix, ".exe" if os.name == "nt" else "")

    def test_prune_old_testbed_homes_removes_siblings_but_not_current(self) -> None:
        with TemporaryDirectory() as raw:
            root = Path(raw)
            old = root / "tabula-testbed.old"
            current = root / "tabula-testbed.current"
            unrelated = root / "other"
            old.mkdir()
            current.mkdir()
            unrelated.mkdir()

            removed, errors = prune_old_testbed_homes(root, current=current)

            self.assertEqual((removed, errors), (1, 0))
            self.assertFalse(old.exists())
            self.assertTrue(current.exists())
            self.assertTrue(unrelated.exists())

    def test_component_filters_do_not_narrow_baseline_set_bundles(self) -> None:
        manifest = {
            "sets": {"baseline": ["base", "test-fixtures"]},
            "bundles": {"base": {}, "test-fixtures": {}},
        }

        selected, component_map = selected_bundles(
            manifest,
            "baseline",
            False,
            [],
            [],
            ["test-fixtures:testbed-cold-python"],
        )

        self.assertEqual(selected, ["base", "test-fixtures"])
        self.assertEqual(component_map, {})

    def test_component_filters_still_apply_to_empty_set_suites(self) -> None:
        manifest = {
            "sets": {"empty": []},
            "bundles": {"test-fixtures": {}},
        }

        selected, component_map = selected_bundles(
            manifest,
            "empty",
            False,
            [],
            [],
            ["test-fixtures:testbed-cold-python"],
        )

        self.assertEqual(selected, ["test-fixtures"])
        self.assertEqual(component_map, {"test-fixtures": ["testbed-cold-python"]})

    def test_mixed_baseline_and_component_suites_generate_full_baseline_bundle(self) -> None:
        manifest = {
            "sets": {"baseline": ["base", "test-fixtures"], "empty": []},
            "bundles": {"base": {}, "test-fixtures": {}},
        }
        specs = {
            "baseline": SuiteSpec("baseline", "baseline", (), (), (Path(__file__),), Path(__file__)),
            "skill": SuiteSpec("skill", "base", (), ("test-fixtures:testbed-cold-python",), (Path(__file__),), Path(__file__)),
        }
        args = type("Args", (), {"suite": ["baseline", "skill"], "component": [], "bundle": [], "without": [], "all": False, "set": ""})()

        set_name, all_set, bundles, without, components, tests, _ = resolve_selection(args, specs)
        selected, component_map = selected_bundles(manifest, set_name, all_set, bundles, without, components)

        self.assertEqual(selected, ["base", "test-fixtures"])
        self.assertEqual(component_map, {})

    def test_runtime_tenants_are_selected_from_suite_metadata(self) -> None:
        specs = {
            "baseline": SuiteSpec("baseline", "baseline", (), (), (Path(__file__),), Path(__file__)),
            "whitelist": SuiteSpec("whitelist", "base", (), (), (Path(__file__),), Path(__file__), ("alpha",)),
        }

        self.assertEqual(runtime_tenants_for_suites(specs, ["baseline"]), ())
        self.assertEqual(runtime_tenants_for_suites(specs, ["whitelist"]), ("alpha",))

    def test_runtime_tenants_conflict_when_combining_incompatible_suites(self) -> None:
        specs = {
            "isolation": SuiteSpec("isolation", "base", (), (), (Path(__file__),), Path(__file__), ("*",)),
            "whitelist": SuiteSpec("whitelist", "base", (), (), (Path(__file__),), Path(__file__), ("alpha",)),
        }

        with self.assertRaises(SystemExit) as ctx:
            runtime_tenants_for_suites(specs, ["isolation", "whitelist"])
        self.assertIn("conflicting runtime_tenants", str(ctx.exception))

    def test_set_runtime_tenants_rewrites_existing_kernel_allowlist(self) -> None:
        with TemporaryDirectory() as raw:
            path = Path(raw) / "runtime.toml"
            path.write_text(
                'plugin_dirs = []\n\n[[kernel]]\n  id = "main"\n  url = "unix:///tmp/runtime.sock"\n  token_file = "/tmp/token"\n  tenants = ["*"]\n',
                encoding="utf-8",
            )

            set_runtime_tenants(path, ("alpha",))

            text = path.read_text(encoding="utf-8")
            self.assertIn('tenants = ["alpha"]', text)
            self.assertNotIn('tenants = ["*"]', text)

    def test_entry_protocol_markers_include_sdk_contract(self) -> None:
        with TemporaryDirectory() as raw:
            tmp_path = Path(raw)
            entry = tmp_path / "plugins" / "demo" / "run.py"
            entry.parent.mkdir(parents=True)
            entry.write_text("from tabula_plugin_sdk import run\n", encoding="utf-8")

            sdk_dir = tmp_path / "packages" / "python" / "src" / "tabula_plugin_sdk"
            sdk_dir.mkdir(parents=True)
            (sdk_dir / "api.py").write_text(
                "raise RuntimeError('expected register_request as first plugin message')\n",
                encoding="utf-8",
            )
            (sdk_dir / "protocol.py").write_text("METHOD_REGISTER_REQUEST = 'register_request'\n", encoding="utf-8")

            markers = entry_protocol_markers(entry, tmp_path / "packages" / "python" / "src")

            self.assertIn("sdk:api.py:legacy-register-request", markers)
            self.assertIn("sdk:api.py:legacy-register-request-error", markers)
            self.assertIn("sdk:protocol.py:legacy-register-request", markers)
            self.assertTrue(format_protocol_markers(markers).startswith("sdk:api.py:legacy-register-request"))

    def test_entry_protocol_markers_report_m2_worker_entrypoint(self) -> None:
        with TemporaryDirectory() as raw:
            tmp_path = Path(raw)
            entry = tmp_path / "plugins" / "demo" / "run.py"
            entry.parent.mkdir(parents=True)
            entry.write_text("print({'op': 'init_ack'}); print({'op': 'tools_updated'})\n", encoding="utf-8")

            markers = entry_protocol_markers(entry, tmp_path / "missing-lib")

            self.assertEqual(markers, ["entry:m2-worker-init-ack", "entry:m2-worker-tools-updated"])

    def test_manifest_launch_details_supports_worker_command(self) -> None:
        with TemporaryDirectory() as raw:
            tmp_path = Path(raw)
            manifest_path = tmp_path / "plugins" / "demo" / "plugin.toml"
            worker_path = manifest_path.parent / "scripts" / "run.js"
            worker_path.parent.mkdir(parents=True)
            worker_path.write_text("console.log('init_ack'); console.log('tools_updated')\n", encoding="utf-8")

            details = manifest_launch_details(
                manifest_path,
                {"worker": {"command": ["node", "scripts/run.js"], "mode": "cold"}},
            )

            summary = "\n".join(details["summary"])
            self.assertIn("worker.command = ['node', 'scripts/run.js']", summary)
            self.assertIn("worker.mode = cold", summary)
            self.assertIn(f"entry_path = {worker_path.resolve()}", summary)
            self.assertIn("entry:m2-worker-init-ack", summary)
            self.assertIn("entry:m2-worker-tools-updated", summary)

    def test_manifest_launch_details_uses_runtime_entry_when_worker_only_sets_mode(self) -> None:
        with TemporaryDirectory() as raw:
            manifest_path = Path(raw) / "plugin.toml"
            entry_path = manifest_path.parent / "run.py"
            entry_path.write_text("from tabula_plugin_sdk import run\n", encoding="utf-8")

            details = manifest_launch_details(
                manifest_path,
                {"runtime": "python", "entry": "run.py", "worker": {"mode": "warm"}},
            )

            summary = "\n".join(details["summary"])
            self.assertIn("runtime = python", summary)
            self.assertIn("entry = run.py", summary)

    @unittest.skipIf(os.name == "nt", "test creates a POSIX executable shim")
    def test_runtime_sidecar_layout_writes_protocol_marker_summary(self) -> None:
        with TemporaryDirectory() as raw:
            home = Path(raw)
            bin_dir = home / "bin"
            logs_dir = home / "logs"
            plugin_dir = home / "plugins" / "demo"
            sdk_dir = home / "packages" / "python" / "src" / "tabula_plugin_sdk"
            bin_dir.mkdir(parents=True)
            logs_dir.mkdir()
            plugin_dir.mkdir(parents=True)
            sdk_dir.mkdir(parents=True)

            runtime_bin = bin_dir / "tabula-runtime"
            runtime_bin.write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
            runtime_bin.chmod(0o755)

            runtime_toml = home / "config" / "runtime.toml"
            runtime_toml.parent.mkdir()
            manifest_path = plugin_dir / "plugin.toml"
            runtime_toml.write_text(
                f"plugin_dirs = [\"{manifest_path}\"]\n"
                "[[kernel]]\n"
                f"token_file = \"{home / 'run' / 'runtime-token'}\"\n"
                "url = \"unix:///tmp/tabula-runtime.sock\"\n",
                encoding="utf-8",
            )
            manifest_path.write_text(
                "id = \"demo\"\n"
                "runtime = \"python\"\n"
                "entry = \"run.py\"\n"
                "\n"
                "[requires]\n"
                "protocol_version = 1\n",
                encoding="utf-8",
            )
            (plugin_dir / "run.py").write_text("print({'op': 'tools_updated'})\n", encoding="utf-8")
            (sdk_dir / "api.py").write_text("METHOD_REGISTER_REQUEST = 'register_request'\n", encoding="utf-8")

            verify_runtime_sidecar_layout(home, bin_dir, logs_dir)

            summary = (logs_dir / "runtime-plugin-manifests.txt").read_text(encoding="utf-8")
            self.assertIn(f"## {manifest_path}", summary)
            self.assertIn(f"entry_path = {(plugin_dir / 'run.py').resolve()}", summary)
            self.assertIn("entry:m2-worker-tools-updated", summary)
            self.assertIn("sdk:api.py:legacy-register-request", summary)
            self.assertIn("protocol_version = 1", summary)

    def test_verify_kernel_config_accepts_transport_only_config(self) -> None:
        with TemporaryDirectory() as raw:
            home = Path(raw)
            config = home / "config" / "kernel.toml"
            config.parent.mkdir(parents=True)
            config.write_text('[kernel]\nurl = "ws://127.0.0.1:8091/ws"\n\n[runtime_wss]\nenabled = false\n', encoding="utf-8")

            verify_kernel_config(home, "ws://127.0.0.1:8091/ws")

    def test_verify_kernel_config_rejects_product_policy_fields(self) -> None:
        with TemporaryDirectory() as raw:
            home = Path(raw)
            config = home / "config" / "kernel.toml"
            config.parent.mkdir(parents=True)
            config.write_text('[kernel]\nurl = "ws://127.0.0.1:8091/ws"\n\n[workspace]\npath = "/repo"\n', encoding="utf-8")

            with self.assertRaises(SystemExit) as ctx:
                verify_kernel_config(home, "ws://127.0.0.1:8091/ws")
            self.assertIn("non-kernel fields", str(ctx.exception))

    def test_attached_runtime_pid_accepts_embedded_runtime_shape(self) -> None:
        body = {
            "kernel": {"pid": 1234},
            "runtimes": [
                {
                    "id": "local",
                    "attached": True,
                    "pid": 1234,
                    "capabilities": None,
                    "capabilities_by_tenant": {"default": None},
                }
            ],
        }

        self.assertEqual(attached_runtime_pid(body), 1234)

    def test_wait_for_supervised_runtime_accepts_embedded_runtime_status(self) -> None:
        with TemporaryDirectory() as raw:
            home = Path(raw)
            bin_dir = home / "bin"
            logs_dir = home / "logs"
            bin_dir.mkdir()
            logs_dir.mkdir()
            status = json.dumps(
                {
                    "kernel": {"pid": 4321},
                    "runtimes": [
                        {
                            "id": "local",
                            "attached": True,
                            "pid": 4321,
                            "capabilities": None,
                            "capabilities_by_tenant": {"default": None},
                        }
                    ],
                }
            )

            with mock.patch("tabula_testbed_runner.runner.subprocess.check_output", return_value=status):
                pid = wait_for_supervised_runtime(bin_dir, home, logs_dir)

            self.assertEqual(pid, 4321)
            recorded = (logs_dir / "runtime-status-last.json").read_text(encoding="utf-8")
            self.assertEqual(json.loads(recorded), json.loads(status))

    def test_describe_testbed_sources_records_local_git_ref(self) -> None:
        with TemporaryDirectory() as raw:
            tmp_path = Path(raw)
            subprocess_env = os.environ.copy()
            source = tmp_path / "tabula-bundles"
            source.mkdir()

            from subprocess import run

            run(["git", "init"], cwd=source, check=True, env=subprocess_env, capture_output=True, text=True)
            run(["git", "config", "user.email", "test@example.invalid"], cwd=source, check=True, env=subprocess_env)
            run(["git", "config", "user.name", "Test User"], cwd=source, check=True, env=subprocess_env)
            run(["git", "remote", "add", "origin", "https://example.invalid/tabula-bundles.git"], cwd=source, check=True, env=subprocess_env)
            (source / "bundle.toml").write_text("name = 'demo'\n", encoding="utf-8")
            run(["git", "add", "bundle.toml"], cwd=source, check=True, env=subprocess_env)
            run(["git", "commit", "-m", "test fixture"], cwd=source, check=True, env=subprocess_env, capture_output=True, text=True)

            summary = describe_testbed_sources({"tabula-bundles": f"local:{source}"})
            head = run(["git", "rev-parse", "HEAD"], cwd=source, check=True, env=subprocess_env, capture_output=True, text=True).stdout.strip()

            self.assertIn("## tabula-bundles", summary)
            self.assertIn(f"local_root = {source.resolve()}", summary)
            self.assertIn("exists = true", summary)
            self.assertIn(f"git_head = {head}", summary)
            self.assertIn("git_remote = https://example.invalid/tabula-bundles.git", summary)
            self.assertIn("git_status_short = <clean>", summary)


if __name__ == "__main__":
    unittest.main()

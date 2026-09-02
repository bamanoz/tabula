#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

from tabula_testbed import TestbedClient


class EvolutionInstalled(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    @classmethod
    def git(cls, cwd: Path, *args: str) -> str:
        result = subprocess.run(
            ["git", *args], cwd=cwd, text=True, capture_output=True, check=False, timeout=30
        )
        if result.returncode != 0:
            raise AssertionError(result.stderr or result.stdout)
        return result.stdout.strip()

    def run_service(self, *args: str, check: bool = True) -> subprocess.CompletedProcess[str]:
        home = Path(self.tabula_home)
        command = [sys.executable, "-m", "tabula_distro.cli", "host-service", *args]
        result = subprocess.run(
            command,
            env={**os.environ, "TABULA_HOME": str(home)},
            text=True,
            capture_output=True,
            check=False,
            timeout=30,
        )
        if check and result.returncode != 0:
            self.fail(f"{' '.join(command)} failed: {result.stderr or result.stdout}")
        return result

    @staticmethod
    def write_json(path: Path, value: dict) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(value, sort_keys=True, indent=2) + "\n", encoding="utf-8")

    def configure(
        self,
        repo: Path,
        worktrees: Path,
        live: Path,
        distro_source: Path,
        runtime_link: Path,
    ) -> None:
        home = Path(self.tabula_home)
        fake_acp = home / "run" / "evolution-review-acp.py"
        fake_acp.parent.mkdir(parents=True, exist_ok=True)
        fake_acp.write_text(
            '''import json
import sys
import uuid

for raw in sys.stdin:
    message = json.loads(raw)
    method = message.get("method")
    if method == "initialize":
        result = {"protocolVersion": 1, "agentInfo": {"name": "evolution-review", "version": "1"}, "agentCapabilities": {}}
    elif method == "session/new":
        result = {"sessionId": "review-" + uuid.uuid4().hex[:8]}
    elif method == "session/prompt":
        update = {
            "jsonrpc": "2.0",
            "method": "session/update",
            "params": {
                "sessionId": "fixture",
                "update": {
                    "sessionUpdate": "agent_message_chunk",
                    "content": {"type": "text", "text": "{\\\"decision\\\":\\\"approve\\\",\\\"summary\\\":\\\"installed independent review passed\\\"}"},
                },
            },
        }
        sys.stdout.write(json.dumps(update) + "\\n")
        sys.stdout.flush()
        result = {"stopReason": "end_turn"}
    else:
        result = {}
    sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": message.get("id"), "result": result}) + "\\n")
    sys.stdout.flush()
''',
            encoding="utf-8",
        )

        change_config = home / "tenants" / "default" / "config" / "plugins" / "change-control" / "config.toml"
        change_config.parent.mkdir(parents=True, exist_ok=True)
        change_config.write_text(
            f'worktree_root = {json.dumps(str(worktrees))}\n'
            'command_timeout_seconds = 30\n'
            'reviewer_type = "acp"\n'
            f'reviewer_acp_command = [{json.dumps(sys.executable)}, {json.dumps(str(fake_acp))}]\n'
            'reviewer_timeout_seconds = 30\n'
            '[sources]\n'
            f'fixture = {json.dumps(str(repo))}\n',
            encoding="utf-8",
        )

        evolution_config = home / "tenants" / "default" / "config" / "plugins" / "evolution" / "config.toml"
        evolution_config.parent.mkdir(parents=True, exist_ok=True)
        evolution_config.write_text(
            'scope = "pro"\n'
            'activation_authority = "manual"\n'
            f'change_worktree_root = {json.dumps(str(worktrees))}\n'
            'profile_type = "acp"\n'
            f'profile_acp_command = [{json.dumps(sys.executable)}, {json.dumps(str(fake_acp))}]\n'
            'profile_timeout_seconds = 30\n'
            '[targets.fixture]\n'
            'scope = "advanced"\n'
            'kind = "extension"\n'
            '[targets.distro-fixture]\n'
            'scope = "advanced"\n'
            'kind = "extension"\n'
            '[targets.runtime-fixture]\n'
            'scope = "pro"\n'
            'kind = "runtime"\n'
            '[sources]\n'
            f'fixture = {json.dumps(str(repo))}\n',
            encoding="utf-8",
        )

        probe = home / "run" / "evolution-probe.py"
        probe.write_text(
            "import pathlib, sys\n"
            "raise SystemExit(0 if pathlib.Path(sys.argv[1]).read_text().strip() == sys.argv[2] else 1)\n",
            encoding="utf-8",
        )
        supervisor_state = home / "host-services" / "evolution-supervisor" / "state"
        installer = Path(sys.executable).with_name("tabula-install")
        self.write_json(
            supervisor_state / "targets.json",
            {
                "version": 1,
                "targets": {
                    "fixture": {
                        "kind": "directory",
                        "path": str(live),
                        "stop_argv": ["/usr/bin/true"],
                        "start_argv": ["/usr/bin/true"],
                        "recovery_health_profile": "success",
                        "boot_loop_max_failures": 1,
                        "boot_loop_window_seconds": 300,
                        "health_profiles": {
                            "success": {
                                "startup_timeout_seconds": 1,
                                "observation_seconds": 0,
                                "probes": [
                                    {
                                        "kind": "command",
                                        "argv": [sys.executable, str(probe), str(live / "version.txt"), "good"],
                                    }
                                ],
                            },
                            "failure": {
                                "startup_timeout_seconds": 0.1,
                                "interval_seconds": 0.01,
                                "observation_seconds": 0,
                                "probes": [{"kind": "command", "argv": ["/usr/bin/false"]}],
                            },
                            "base": {
                                "startup_timeout_seconds": 1,
                                "observation_seconds": 0,
                                "probes": [
                                    {
                                        "kind": "command",
                                        "argv": [sys.executable, str(probe), str(live / "version.txt"), "base"],
                                    }
                                ],
                            },
                        },
                    },
                    "distro-fixture": {
                        "kind": "distro",
                        "bootstrap_artifact": str(distro_source),
                        "install_argv": [
                            str(installer), "--home", "{home}", "distro", "install", "{artifact}", "--name", "testbed"
                        ],
                        "recovery_health_profile": "distro-success",
                        "health_profiles": {
                            "distro-success": {
                                "startup_timeout_seconds": 10,
                                "interval_seconds": 0.1,
                                "probes": [
                                    {"kind": "file", "path": str(home / "plugins" / "testbed-echo" / "plugin.toml")}
                                ],
                            },
                            "distro-failure": {
                                "startup_timeout_seconds": 0.1,
                                "interval_seconds": 0.01,
                                "probes": [{"kind": "command", "argv": ["/usr/bin/false"]}],
                            },
                        },
                    },
                    "runtime-fixture": {
                        "kind": "runtime",
                        "active_link": str(runtime_link),
                        "protocol": {"kernel": 3, "worker": 1},
                        "stop_argv": ["/usr/bin/true"],
                        "start_argv": ["/usr/bin/true"],
                        "recovery_health_profile": "runtime-success",
                        "health_profiles": {
                            "runtime-success": {
                                "startup_timeout_seconds": 5,
                                "interval_seconds": 0.1,
                                "probes": [
                                    {"kind": "command", "argv": [str(runtime_link / "bin" / "tabula"), "--version"]},
                                    {"kind": "command", "argv": [str(runtime_link / "bin" / "tabula-runtime"), "--version"]},
                                ],
                            },
                            "runtime-failure": {
                                "startup_timeout_seconds": 0.1,
                                "interval_seconds": 0.01,
                                "probes": [{"kind": "command", "argv": ["/usr/bin/false"]}],
                            },
                        },
                    },
                },
            },
        )
        os.chmod(supervisor_state / "targets.json", 0o600)
        self.write_json(
            supervisor_state / "active.json",
            {"version": 1, "targets": {"fixture": "base", "distro-fixture": "base", "runtime-fixture": "base"}},
        )

    @staticmethod
    def call(client: TestbedClient, tool: str, args: dict, timeout: int = 60, meta: dict | None = None) -> dict:
        result = client.call_tool(tool, args, timeout=timeout, meta=meta)
        if not result.ok:
            raise AssertionError(result.json())
        return result.json()

    def create_change(
        self,
        client: TestbedClient,
        transaction_id: str,
        *,
        allowed_paths: list[str] | None = None,
        protected_paths: list[str] | None = None,
        expected_content: str = "reviewed",
    ) -> dict:
        return self.call(
            client,
            "change_create",
            {
                "transaction_id": transaction_id,
                "source_id": "fixture",
                "objective": "change reviewed source",
                "allowed_paths": allowed_paths or ["src"],
                "protected_paths": protected_paths or ["protected"],
                "required_checks": [f"sh -c 'test $(cat src/value.txt) = {expected_content}'"],
                "required_reviews": ["scope", "acceptance"],
                "ttl_seconds": 3600,
            },
        )["transaction"]

    def assert_tool_error(self, result, message: str) -> None:
        self.assertFalse(result.ok, result.output)
        self.assertIn(message, result.output)

    def reviewed_change(self, client: TestbedClient) -> dict:
        created = self.create_change(client, "installed-change")
        worktree = Path(created["worktree"])
        self.assertFalse(str(worktree).startswith(str(Path(self.tabula_home).resolve())))
        self.call(
            client,
            "evolution_campaign_start",
            {
                "campaign_id": "installed-profile",
                "objective": "exercise strict profile worktree binding",
                "scope": "advanced",
                "authority": "manual",
                "base_release": "base",
                "budget": {"max_candidates": 1, "max_failures": 1, "cooldown_seconds": 0},
            },
        )
        profile = self.call(
            client,
            "evolution_profile_run",
            {
                "campaign_id": "installed-profile",
                "transaction_id": created["id"],
                "profile": "builder",
                "task": "Inspect transaction context and return without changing files.",
            },
            timeout=90,
        )
        self.assertEqual(Path(profile["binding"]["worktree"]).resolve(), worktree.resolve())
        self.assertEqual(profile["binding"]["allowed_paths"], ["src"])
        self.assertEqual(profile["binding"]["allowed_tools"], ["fs_read", "fs_glob", "fs_grep", "fs_edit", "fs_write"])
        (worktree / "src" / "value.txt").write_text("reviewed\n", encoding="utf-8")
        verified = self.call(
            client,
            "change_verify",
            {"transaction_id": created["id"], "check": created["required_checks"][0]},
        )["receipt"]
        self.assertTrue(verified["logs"]["ref"].startswith("artifact://"))
        self.assertEqual(verified["logs"]["sha256"], verified["artifacts"][0]["sha256"])
        for profile in ("scope", "acceptance"):
            reviewed = self.call(
                client,
                "change_review",
                {"transaction_id": created["id"], "profile": profile},
                timeout=90,
            )
            self.assertEqual(reviewed["receipt"]["decision"], "approve")
        receipt = self.call(
            client,
            "change_commit_reviewed",
            {"transaction_id": created["id"], "message": "reviewed installed change"},
        )["receipt"]
        self.assertEqual(self.git(self.repo, "cat-file", "-t", receipt["commit"]), "commit")
        self.assertEqual(self.git(self.repo, "rev-parse", "main"), created["base_revision"])
        return receipt

    def exercise_change_control_attacks(self, client: TestbedClient, external_root: Path) -> None:
        outside_scope = self.create_change(client, "installed-outside-scope")
        Path(outside_scope["worktree"]).joinpath("outside.txt").write_text("blocked\n", encoding="utf-8")
        self.assert_tool_error(
            client.call_tool(
                "change_verify",
                {"transaction_id": outside_scope["id"], "check": outside_scope["required_checks"][0]},
            ),
            "outside allowed scope",
        )

        protected = self.create_change(
            client,
            "installed-protected",
            allowed_paths=["src", "protected"],
        )
        Path(protected["worktree"]).joinpath("protected", "policy.txt").write_text("blocked\n", encoding="utf-8")
        self.assert_tool_error(
            client.call_tool(
                "change_verify",
                {"transaction_id": protected["id"], "check": protected["required_checks"][0]},
            ),
            "protected",
        )

        symlink = self.create_change(client, "installed-symlink")
        escaped = external_root / "escaped"
        escaped.mkdir()
        Path(symlink["worktree"]).joinpath("src", "escape").symlink_to(escaped, target_is_directory=True)
        (escaped / "value.txt").write_text("blocked\n", encoding="utf-8")
        self.assert_tool_error(
            client.call_tool(
                "change_verify",
                {"transaction_id": symlink["id"], "check": symlink["required_checks"][0]},
            ),
            "symlink escape",
        )

        stale = self.create_change(client, "installed-stale", expected_content="first")
        stale_worktree = Path(stale["worktree"])
        stale_worktree.joinpath("src", "value.txt").write_text("first\n", encoding="utf-8")
        self.call(
            client,
            "change_verify",
            {"transaction_id": stale["id"], "check": stale["required_checks"][0]},
        )
        first_review = None
        for profile in ("scope", "acceptance"):
            reviewed = self.call(
                client,
                "change_review",
                {"transaction_id": stale["id"], "profile": profile},
                timeout=90,
            )
            self.assertEqual(reviewed["binding"]["allowed_tools"], ["__change_review_no_tools__"])
            first_review = first_review or reviewed
        stale_worktree.joinpath("src", "value.txt").write_text("second\n", encoding="utf-8")
        self.assert_tool_error(
            client.call_tool(
                "change_commit_reviewed",
                {"transaction_id": stale["id"], "message": "must remain blocked"},
            ),
            "gates incomplete",
        )
        stale_state = self.call(client, "change_get", {"transaction_id": stale["id"]})["transaction"]
        self.assertEqual(stale_state["checks"], {})
        self.assertEqual(stale_state["reviews"], {})

        victim = self.create_change(client, "installed-reviewer-victim")
        Path(victim["worktree"]).joinpath("src", "value.txt").write_text("reviewed\n", encoding="utf-8")
        assert first_review is not None
        with TestbedClient(self.url, name="evolution-reviewer-attack") as reviewer:
            reviewer.connect()
            reviewer.create_session(first_review["job"]["session"], tenant_id="default")
            self.assert_tool_error(
                reviewer.call_tool(
                    "change_abort",
                    {"transaction_id": victim["id"], "reason": "reviewer attempted mutation"},
                ),
                "not in allowed_tools",
            )
        self.assertNotEqual(
            self.call(client, "change_get", {"transaction_id": victim["id"]})["transaction"]["phase"],
            "aborted",
        )

    def activate_candidate(
        self,
        client: TestbedClient,
        *,
        campaign_id: str,
        parent_release: str,
        payload: Path,
        health_profile: str,
        target_id: str = "fixture",
        scope: str = "advanced",
        restart_class: str = "reload",
        protocol: dict | None = None,
        expected_error: str = "",
    ) -> dict:
        self.call(
            client,
            "evolution_campaign_start",
            {
                "campaign_id": campaign_id,
                "objective": f"activate {campaign_id}",
                "scope": scope,
                "authority": "manual",
                "base_release": parent_release,
                "budget": {"max_candidates": 2, "max_failures": 1, "cooldown_seconds": 0},
            },
        )
        self.call(
            client,
            "evolution_attach_change",
            {"campaign_id": campaign_id, "transaction_id": "installed-change"},
        )
        candidate = self.call(
            client,
            "evolution_candidate_seal",
            {
                "campaign_id": campaign_id,
                "target_id": target_id,
                "payload_path": str(payload),
                "parent_release": parent_release,
                "bundle_lock_sha256": "1" * 64,
                "distro_lock_sha256": "2" * 64,
                "protocol": protocol or {"kernel": 3, "worker": 1},
                "restart_class": restart_class,
                "health_profile": health_profile,
            },
        )["candidate"]
        self.call(
            client,
            "evolution_candidate_approve",
            {"campaign_id": campaign_id, "approver": "spoofed-owner"},
            meta={"actor": "owner/evolution-testbed"},
        )
        result = client.call_tool("evolution_activate", {"campaign_id": campaign_id}, timeout=90)
        if expected_error:
            self.assertFalse(result.ok, result.output)
            self.assertIn(expected_error, result.output)
            return {"error": result.output}
        if not result.ok:
            self.fail(result.json())
        campaign = result.json()["campaign"]
        self.assertEqual(campaign["supervisor_receipt"]["candidate_id"], candidate["id"])
        return campaign

    def test_reviewed_campaign_activation_and_external_rollback(self) -> None:
        home = Path(self.tabula_home).resolve()
        external_root = Path(tempfile.mkdtemp(prefix="tabula-evolution-", dir=str(home.parent))).resolve()
        self.addCleanup(shutil.rmtree, external_root, True)
        self.repo = external_root / "source"
        self.repo.mkdir()
        self.git(self.repo, "init", "-b", "main")
        self.git(self.repo, "config", "user.name", "Testbed")
        self.git(self.repo, "config", "user.email", "testbed@example.test")
        (self.repo / "src").mkdir()
        (self.repo / "src" / "value.txt").write_text("base\n", encoding="utf-8")
        (self.repo / "protected").mkdir()
        (self.repo / "protected" / "policy.txt").write_text("fixed\n", encoding="utf-8")
        self.git(self.repo, "add", ".")
        self.git(self.repo, "commit", "-m", "initial")
        worktrees = external_root / "worktrees"
        live = external_root / "live"
        live.mkdir()
        (live / "version.txt").write_text("base\n", encoding="utf-8")
        good_payload = external_root / "good-payload"
        good_payload.mkdir()
        (good_payload / "version.txt").write_text("good\n", encoding="utf-8")
        bad_payload = external_root / "bad-payload"
        bad_payload.mkdir()
        (bad_payload / "version.txt").write_text("broken\n", encoding="utf-8")

        distro_base = external_root / "distro-base"
        distro_base.mkdir()
        shutil.copy2(home / "distrib" / "testbed" / "distro.toml", distro_base / "distro.toml")
        shutil.copytree(home / "distrib" / "testbed" / "templates", distro_base / "templates")
        distro_candidate = external_root / "distro-candidate"
        shutil.copytree(distro_base, distro_candidate)
        with (distro_candidate / "distro.toml").open("a", encoding="utf-8") as handle:
            handle.write(
                '\n[[bundles]]\nname = "test-fixtures"\n'
                'source = "source:tabula-bundles#path=test-fixtures"\n'
                'components = ["testbed-echo"]\n'
            )
        distro_broken = external_root / "distro-broken"
        shutil.copytree(distro_candidate, distro_broken)
        (distro_broken / "broken-candidate.txt").write_text("broken\n", encoding="utf-8")

        runtime_base = home / "host-services" / "evolution-supervisor" / "state" / "runtime-releases" / "runtime-fixture" / "base" / "payload"
        (runtime_base / "bin").mkdir(parents=True)
        shutil.copy2(home / "bin" / "tabula", runtime_base / "bin" / "tabula")
        shutil.copy2(home / "bin" / "tabula-runtime", runtime_base / "bin" / "tabula-runtime")
        runtime_link = external_root / "runtime-active"
        runtime_link.symlink_to(runtime_base)
        runtime_candidate = external_root / "runtime-candidate"
        (runtime_candidate / "bin").mkdir(parents=True)
        tabula_root = Path(os.environ["TABULA_ROOT"]).resolve()
        for command, output in (
            ("./cmd/tabula", runtime_candidate / "bin" / "tabula"),
            ("./cmd/tabula-runtime", runtime_candidate / "bin" / "tabula-runtime"),
        ):
            subprocess.run(
                ["go", "build", "-o", str(output), command],
                cwd=tabula_root,
                check=True,
                text=True,
                capture_output=True,
                timeout=300,
            )
        runtime_broken = external_root / "runtime-broken"
        (runtime_broken / "bin").mkdir(parents=True)
        for name in ("tabula", "tabula-runtime"):
            binary = runtime_broken / "bin" / name
            binary.write_text("#!/bin/sh\nexit 1\n", encoding="utf-8")
            binary.chmod(0o755)

        self.configure(self.repo, worktrees, live, distro_base, runtime_link)
        supervisor_state = home / "host-services" / "evolution-supervisor" / "state"
        self.run_service("reconcile", "--adapter", "process")
        self.addCleanup(self.run_service, "remove", "evolution-supervisor", "--purge", check=False)
        status = json.loads(self.run_service("status", "evolution-supervisor", "--json").stdout)
        self.assertTrue(status["running"], status)
        kernel = json.loads(
            subprocess.run(
                [str(home / "bin" / "tabula"), "status", "--json"],
                env={**os.environ, "TABULA_HOME": str(home)},
                text=True,
                capture_output=True,
                check=True,
                timeout=10,
            ).stdout
        )
        self.assertNotEqual(status["pid"], kernel["kernel"]["pid"])

        tools = {
            "change_create",
            "change_get",
            "change_abort",
            "change_verify",
            "change_review",
            "change_commit_reviewed",
            "evolution_campaign_start",
            "evolution_campaign_get",
            "evolution_profile_run",
            "evolution_attach_change",
            "evolution_candidate_seal",
            "evolution_candidate_approve",
            "evolution_activate",
        }
        with TestbedClient(self.url, name="evolution-installed") as client:
            client.connect()
            client.create_session("evolution-installed", tenant_id="default")

            self.exercise_change_control_attacks(client, external_root)
            self.reviewed_change(client)
            self.call(
                client,
                "evolution_campaign_start",
                {
                    "campaign_id": "installed-noop",
                    "objective": "exercise durable no-op breaker",
                    "scope": "advanced",
                    "authority": "manual",
                    "base_release": "base",
                    "budget": {
                        "max_candidates": 2,
                        "max_failures": 1,
                        "max_noops": 1,
                        "cooldown_seconds": 0,
                    },
                },
            )
            self.call(
                client,
                "evolution_attach_change",
                {"campaign_id": "installed-noop", "transaction_id": "installed-change"},
            )
            noop_candidate = {
                "campaign_id": "installed-noop",
                "target_id": "fixture",
                "payload_path": str(good_payload),
                "parent_release": "base",
                "bundle_lock_sha256": "1" * 64,
                "distro_lock_sha256": "2" * 64,
                "protocol": {"kernel": 3, "worker": 1},
                "restart_class": "reload",
                "health_profile": "success",
            }
            self.call(client, "evolution_candidate_seal", noop_candidate)
            repeated = client.call_tool("evolution_candidate_seal", noop_candidate)
            self.assertFalse(repeated.ok, repeated.output)
            self.assertIn("identical", repeated.output)
            noop_campaign = self.call(
                client,
                "evolution_campaign_get",
                {"campaign_id": "installed-noop"},
            )["campaign"]
            self.assertEqual(noop_campaign["usage"]["noops"], 1)
            self.assertEqual(noop_campaign["phase"], "paused")
            self.assertEqual(noop_campaign["pause_reason"], "no-op budget exhausted")

            success = self.activate_candidate(
                client,
                campaign_id="installed-success",
                parent_release="base",
                payload=good_payload,
                health_profile="success",
            )
            good_release = success["supervisor_receipt"]["active_release"]
            self.assertEqual(success["phase"], "absorbed")
            self.assertEqual((live / "version.txt").read_text(encoding="utf-8").strip(), "good")

            failed = self.activate_candidate(
                client,
                campaign_id="installed-failure",
                parent_release=good_release,
                payload=bad_payload,
                health_profile="failure",
            )
            self.assertEqual(failed["supervisor_receipt"]["outcome"], "rolled_back")
            self.assertEqual(failed["supervisor_receipt"]["active_release"], good_release)
            self.assertEqual((live / "version.txt").read_text(encoding="utf-8").strip(), "good")

            self.activate_candidate(
                client,
                campaign_id="installed-boot-loop-blocked",
                parent_release=good_release,
                payload=good_payload,
                health_profile="success",
                expected_error="boot-loop breaker open",
            )
            self.assertEqual((live / "version.txt").read_text(encoding="utf-8").strip(), "good")

            distro_success = self.activate_candidate(
                client,
                campaign_id="installed-distro-success",
                parent_release="base",
                payload=distro_candidate,
                health_profile="distro-success",
                target_id="distro-fixture",
            )
            distro_release = distro_success["supervisor_receipt"]["active_release"]
            self.assertEqual(distro_success["phase"], "absorbed")


            self.assertEqual(self.call(client, "testbed_echo", {"text": "evolved"})["text"], "evolved")

            distro_failed = self.activate_candidate(
                client,
                campaign_id="installed-distro-failure",
                parent_release=distro_release,
                payload=distro_broken,
                health_profile="distro-failure",
                target_id="distro-fixture",
            )
            self.assertEqual(distro_failed["supervisor_receipt"]["outcome"], "rolled_back")
            self.assertTrue((home / "plugins" / "testbed-echo" / "plugin.toml").is_file())

            runtime_success = self.activate_candidate(
                client,
                campaign_id="installed-runtime-success",
                parent_release="base",
                payload=runtime_candidate,
                health_profile="runtime-success",
                target_id="runtime-fixture",
                scope="pro",
                restart_class="runtime",
            )
            runtime_release = runtime_success["supervisor_receipt"]["active_release"]
            self.assertEqual(runtime_success["phase"], "absorbed")
            self.assertEqual(runtime_link.resolve(), supervisor_state / "runtime-releases" / "runtime-fixture" / runtime_release / "payload")
            self.assertNotEqual(runtime_link.resolve(), runtime_base)

            runtime_failed = self.activate_candidate(
                client,
                campaign_id="installed-runtime-failure",
                parent_release=runtime_release,
                payload=runtime_broken,
                health_profile="runtime-failure",
                target_id="runtime-fixture",
                scope="pro",
                restart_class="runtime",
            )
            self.assertEqual(runtime_failed["supervisor_receipt"]["outcome"], "rolled_back")
            self.assertEqual(runtime_link.resolve(), supervisor_state / "runtime-releases" / "runtime-fixture" / runtime_release / "payload")
            self.activate_candidate(
                client,
                campaign_id="installed-runtime-protocol-mismatch",
                parent_release=runtime_release,
                payload=runtime_candidate,
                health_profile="runtime-success",
                target_id="runtime-fixture",
                scope="pro",
                restart_class="runtime",
                protocol={"kernel": 4, "worker": 1},
                expected_error="protocol mismatch",
            )
            self.assertEqual(runtime_link.resolve(), supervisor_state / "runtime-releases" / "runtime-fixture" / runtime_release / "payload")

        retained = supervisor_state / "known-good" / "fixture" / good_release / "payload"
        self.run_service("stop", "evolution-supervisor")
        (live / "version.txt").write_text("interrupted\n", encoding="utf-8")
        active = json.loads((supervisor_state / "active.json").read_text(encoding="utf-8"))
        active["targets"]["fixture"] = "interrupted-candidate"
        self.write_json(supervisor_state / "active.json", active)
        self.write_json(
            supervisor_state / "journals" / "installed-interrupted.json",
            {
                "version": 1,
                "request_id": "installed-interrupted",
                "candidate_id": "interrupted-candidate",
                "candidate_path": str(home / "evolution" / "candidates" / "interrupted-candidate"),
                "target_id": "fixture",
                "previous_release": good_release,
                "previous_artifact": str(retained),
                "phase": "switched",
            },
        )
        self.run_service("start", "evolution-supervisor", "--adapter", "process")
        self.assertEqual((live / "version.txt").read_text(encoding="utf-8").strip(), "good")
        recovered = json.loads((supervisor_state / "receipts" / "installed-interrupted.json").read_text(encoding="utf-8"))
        self.assertEqual(recovered["outcome"], "rolled_back")
        self.assertEqual(json.loads((supervisor_state / "active.json").read_text(encoding="utf-8"))["targets"]["fixture"], good_release)

        self.assertTrue((retained / "version.txt").is_file())
        self.assertTrue((supervisor_state / "receipts").is_dir())
        self.assertTrue((home / "evolution" / "candidates").is_dir())
        self.assertFalse((home / "plugins" / "reflection").exists())
        self.assertFalse((home / "plugins" / "initiative").exists())


def main() -> int:
    parser = argparse.ArgumentParser(description="Run installed evolution campaign test")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="http://127.0.0.1:8091/metrics")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    EvolutionInstalled.url = args.url
    EvolutionInstalled.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(
        unittest.defaultTestLoader.loadTestsFromTestCase(EvolutionInstalled)
    )
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

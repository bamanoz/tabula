#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
import signal
import subprocess
import sys
import threading
import time
import uuid
import unittest
from dataclasses import dataclass, replace
from pathlib import Path
from typing import Any, Callable

from tabula_testbed import TestbedClient, restart_kernel, restart_runtime, runtime_pid
from tabula_testbed.client import ProtocolError

DRIVER = "testbed-recovery-driver"
SPEC = "testbed:installed-crash-recovery"
TERMINAL_STATES = {"completed", "failed", "cancelled", "discarded"}


@dataclass(frozen=True)
class DiagnosticIDs:
    session_id: str
    input_id: str = ""
    turn_id: str = ""
    attempt_id: str = ""
    generation: int = 0
    cursor: str = ""

    def render(self) -> str:
        return (
            f"session={self.session_id or '-'} input={self.input_id or '-'} "
            f"turn={self.turn_id or '-'} attempt={self.attempt_id or '-'} "
            f"generation={self.generation or '-'} cursor={self.cursor or '-'}"
        )


class InstalledCrashRecovery(unittest.TestCase):
    url = "ws://localhost:8089/ws"
    tabula_home = ""

    @classmethod
    def setUpClass(cls) -> None:
        cls.home = Path(cls.tabula_home).resolve()
        cls.driver_root = cls.home / "state" / DRIVER
        cls._assert_installed_paths()

    @classmethod
    def _assert_installed_paths(cls) -> None:
        tabula_bin = Path(os.environ["TABULA_TESTBED_TABULA_BIN"]).resolve()
        runtime_bin = tabula_bin.with_name("tabula-runtime" + tabula_bin.suffix).resolve()
        driver_manifest = (cls.home / "plugins" / DRIVER / "plugin.toml").resolve()
        for label, path in (
            ("kernel", tabula_bin),
            ("runtime", runtime_bin),
            ("recovery driver", driver_manifest),
        ):
            if not path.is_file() or not path.is_relative_to(cls.home):
                raise AssertionError(f"installed {label} path is invalid: {path}")

    def _session(self, scenario: str) -> str:
        return f"testbed-recovery-{scenario}-{uuid.uuid4().hex[:10]}"

    def _client(self, name: str) -> TestbedClient:
        return TestbedClient(self.url, name=name, meta={"tabula.client_role": "user"})

    def _state_dir(self, tenant_id: str, session_id: str) -> Path:
        return self.driver_root / tenant_id / session_id

    def _control(self, tenant_id: str, session_id: str, kind: str, name: str, value: Any = "") -> Path:
        path = self._state_dir(tenant_id, session_id) / kind / name
        path.parent.mkdir(parents=True, exist_ok=True)
        if isinstance(value, (dict, list)):
            text = json.dumps(value)
        else:
            text = str(value)
        path.write_text(text, encoding="utf-8")
        return path

    def _observed(self, tenant_id: str, session_id: str) -> list[dict[str, Any]]:
        path = self._state_dir(tenant_id, session_id) / "observed.jsonl"
        if not path.is_file():
            return []
        records: list[dict[str, Any]] = []
        for line in path.read_text(encoding="utf-8").splitlines():
            try:
                value = json.loads(line)
            except json.JSONDecodeError:
                continue
            if isinstance(value, dict):
                records.append(value)
        return records

    def _wait_observed(
        self,
        tenant_id: str,
        ids: DiagnosticIDs,
        predicate: Callable[[dict[str, Any]], bool],
        *,
        timeout: float = 45,
    ) -> dict[str, Any]:
        deadline = time.monotonic() + timeout
        last: list[dict[str, Any]] = []
        while time.monotonic() < deadline:
            last = self._observed(tenant_id, ids.session_id)
            for record in last:
                if predicate(record):
                    return record
            time.sleep(0.05)
        self.fail(f"timed out waiting for recovery-driver observation; {ids.render()} observed={last[-12:]!r}")

    def _snapshot(self, tenant_id: str, session_id: str) -> dict[str, Any]:
        with self._client(f"snapshot-{session_id}") as client:
            return client.get_session(session_id, tenant_id=tenant_id)

    def _turn(self, snapshot: dict[str, Any], turn_id: str) -> dict[str, Any]:
        data = snapshot.get("data", {})
        projection = data.get("projection", {})
        for turn in projection.get("turns", []):
            if turn.get("turn_id") == turn_id:
                return turn
        self.fail(f"turn missing from snapshot: turn={turn_id} snapshot={snapshot!r}")

    def _wait_turn(
        self,
        tenant_id: str,
        ids: DiagnosticIDs,
        states: set[str],
        *,
        timeout: float = 45,
    ) -> tuple[dict[str, Any], dict[str, Any]]:
        deadline = time.monotonic() + timeout
        last: dict[str, Any] = {}
        while time.monotonic() < deadline:
            try:
                snapshot = self._snapshot(tenant_id, ids.session_id)
                turn = self._turn(snapshot, ids.turn_id)
                last = {"snapshot": snapshot, "turn": turn}
                if turn.get("state") in states:
                    return snapshot, turn
            except (OSError, ProtocolError, TimeoutError, AssertionError) as exc:
                last = {"error": repr(exc)}
            time.sleep(0.1)
        self.fail(f"timed out waiting for turn states={sorted(states)}; {ids.render()} last={last!r}")

    def _create(self, client: TestbedClient, tenant_id: str, session_id: str) -> None:
        client.create_session(session_id, tenant_id=tenant_id, driver_component_id=DRIVER, agent_spec_revision=SPEC)
        client.session = session_id
        client.tenant_id = tenant_id

    def _submit(self, client: TestbedClient, tenant_id: str, ids: DiagnosticIDs, text: str) -> tuple[dict[str, Any], DiagnosticIDs]:
        self.assertEqual(client.tenant_id, tenant_id, ids.render())
        self.assertEqual(client.session, ids.session_id, ids.render())
        accepted = client.submit_input(ids.input_id, {"type": "text", "text": text})
        data = accepted["data"]
        return accepted, replace(ids, turn_id=str(data["turn_id"]), cursor=str(data["cursor"]))

    def _command(self, client: TestbedClient, tenant_id: str, session_id: str, op: str, data: dict[str, Any]) -> dict[str, Any]:
        return client._request("command", op, data, tenant_id=tenant_id, session_id=session_id)

    def _permit_ids(self, tenant_id: str, ids: DiagnosticIDs, *, generation_gt: int = 0) -> DiagnosticIDs:
        record = self._wait_observed(
            tenant_id,
            ids,
            lambda item: item.get("event") == "permit-observed"
            and item.get("turn_id") == ids.turn_id
            and int(item.get("generation") or 0) > generation_gt,
        )
        return replace(
            ids,
            attempt_id=str(record["attempt_id"]),
            generation=int(record["generation"]),
            cursor=f"cur_{record['cursor']}",
        )

    def _complete(self, tenant_id: str, ids: DiagnosticIDs) -> None:
        self._control(tenant_id, ids.session_id, "actions", "complete")
        self._wait_turn(tenant_id, ids, {"completed"})

    def _delete(self, tenant_id: str, ids: DiagnosticIDs) -> None:
        try:
            snapshot = self._snapshot(tenant_id, ids.session_id)
        except Exception:
            return
        projection = snapshot["data"]["projection"]
        with self._client(f"cleanup-{ids.session_id}") as client:
            for turn in projection.get("turns", []):
                if turn.get("state") not in TERMINAL_STATES:
                    try:
                        self._command(client, tenant_id, ids.session_id, "turn.cancel", {"turn_id": turn["turn_id"]})
                    except ProtocolError:
                        pass
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                snapshot = client.get_session(ids.session_id, tenant_id=tenant_id)
                projection = snapshot["data"]["projection"]
                if all(turn.get("state") in TERMINAL_STATES for turn in projection.get("turns", [])):
                    break
                time.sleep(0.05)
            try:
                self._command(
                    client,
                    tenant_id,
                    ids.session_id,
                    "session.delete",
                    {"expected_session_version": int(projection["session_version"])},
                )
            except ProtocolError:
                pass

    def test_01_input_before_driver_startup(self) -> None:
        if os.name == "nt" or not hasattr(signal, "SIGSTOP"):
            self.skipTest("deterministic pre-start barrier requires POSIX SIGSTOP")
        tenant_id = "default"
        ids = DiagnosticIDs(self._session("prestart"), input_id=f"input-{uuid.uuid4().hex}")
        pid = runtime_pid()
        os.kill(pid, signal.SIGSTOP)
        creator = self._client("gateway-prestart-creator")
        submitter = self._client("gateway-prestart-submitter")
        result: dict[str, BaseException | dict[str, Any]] = {}
        try:
            creator.connect()
            submitter.connect()

            def create() -> None:
                try:
                    result["create"] = creator.create_session(
                        ids.session_id,
                        tenant_id=tenant_id,
                        driver_component_id=DRIVER,
                        agent_spec_revision=SPEC,
                    )
                except BaseException as exc:
                    result["create"] = exc

            thread = threading.Thread(target=create, daemon=True)
            thread.start()
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                try:
                    submitter.get_session(ids.session_id, tenant_id=tenant_id)
                    break
                except ProtocolError as exc:
                    if exc.code != "not_found":
                        raise
                    time.sleep(0.02)
            else:
                self.fail(f"session commit not visible before driver startup; {ids.render()}")
            _, ids = self._submit(submitter, tenant_id, ids, "accepted before driver startup")
            snapshot, turn = self._wait_turn(tenant_id, ids, {"queued"}, timeout=2)
            self.assertEqual(turn["state"], "queued", f"{ids.render()} snapshot={snapshot!r}")
        finally:
            os.kill(pid, signal.SIGCONT)
            creator.close()
            submitter.close()
        thread.join(12)
        if isinstance(result.get("create"), BaseException):
            raise result["create"]  # type: ignore[misc]
        ids = self._permit_ids(tenant_id, ids)
        self._complete(tenant_id, ids)
        self._delete(tenant_id, ids)

    def test_02_runtime_restart_while_input_is_queued(self) -> None:
        tenant_id = "default"
        ids = DiagnosticIDs(self._session("runtime-restart"), input_id=f"input-{uuid.uuid4().hex}")
        barrier = self._control(tenant_id, ids.session_id, "barriers", "hold-before-prepared")
        with self._client("gateway-runtime-restart") as client:
            self._create(client, tenant_id, ids.session_id)
            _, ids = self._submit(client, tenant_id, ids, "queued across runtime restart")
        assigned = self._wait_observed(tenant_id, ids, lambda item: item.get("op") == "assign" and item.get("direction") == "inbound")
        old_generation = int(assigned["generation"])
        restart_runtime(timeout=20)
        barrier.unlink(missing_ok=True)
        ids = self._permit_ids(tenant_id, ids, generation_gt=old_generation)
        self._complete(tenant_id, ids)
        self._delete(tenant_id, ids)

    def test_03_driver_crash_before_and_after_permit_with_stale_takeover(self) -> None:
        tenant_id = "default"
        for phase, action, expected_state in (
            ("before", "crash-before-prepared", "completed"),
            ("after", "crash-after-permit", "recovery_required"),
        ):
            with self.subTest(phase=phase):
                ids = DiagnosticIDs(self._session(f"crash-{phase}"), input_id=f"input-{uuid.uuid4().hex}")
                action_path = self._control(tenant_id, ids.session_id, "actions", action)
                with self._client(f"gateway-crash-{phase}") as client:
                    self._create(client, tenant_id, ids.session_id)
                    _, ids = self._submit(client, tenant_id, ids, f"driver crash {phase} permit")
                crashed = self._wait_observed(tenant_id, ids, lambda item: item.get("event") == "crash-requested")
                old_generation = int(crashed["desired_generation"])
                old_attempt = next(
                    (str(item.get("attempt_id")) for item in self._observed(tenant_id, ids.session_id) if item.get("attempt_id")),
                    "",
                )
                action_path.unlink(missing_ok=True)
                if phase == "before":
                    self._control(tenant_id, ids.session_id, "actions", "complete")
                    ids = self._permit_ids(tenant_id, ids, generation_gt=old_generation)
                    self.assertNotEqual(ids.attempt_id, old_attempt, ids.render())
                    stale_meta = {
                        "turn_id": ids.turn_id,
                        "attempt_id": old_attempt,
                        "driver_instance_id": crashed["driver_instance_id"],
                        "lease_id": crashed.get("lease_id") or "stale-lease",
                        "driver_generation": old_generation,
                        "turn_correlation_id": ids.turn_id,
                    }
                    with self._client("gateway-stale-generation") as stale:
                        stale.session = ids.session_id
                        stale.tenant_id = tenant_id
                        with self.assertRaises(ProtocolError) as caught:
                            stale.call_tool("testbed_fail", {"message": "must be fenced"}, meta=stale_meta)
                    self.assertEqual(caught.exception.code, "stale_driver", ids.render())
                snapshot, turn = self._wait_turn(tenant_id, ids, {expected_state}, timeout=45)
                self.assertEqual(turn["state"], expected_state, f"{ids.render()} snapshot={snapshot!r}")
                self._delete(tenant_id, ids)

    def test_04_kernel_restart_with_queued_and_active_turns(self) -> None:
        tenant_id = "default"
        queued = DiagnosticIDs(self._session("kernel-queued"), input_id=f"input-{uuid.uuid4().hex}")
        active = DiagnosticIDs(self._session("kernel-active"), input_id=f"input-{uuid.uuid4().hex}")
        queued_barrier = self._control(tenant_id, queued.session_id, "barriers", "hold-before-prepared")
        active_barrier = self._control(tenant_id, active.session_id, "barriers", "hold-after-permit")
        with self._client("gateway-kernel-restart-queued") as queued_client:
            self._create(queued_client, tenant_id, queued.session_id)
            _, queued = self._submit(queued_client, tenant_id, queued, "queued across kernel restart")
        with self._client("gateway-kernel-restart-active") as active_client:
            self._create(active_client, tenant_id, active.session_id)
            _, active = self._submit(active_client, tenant_id, active, "active across kernel restart")
        self._wait_observed(tenant_id, queued, lambda item: item.get("barrier") == "hold-before-prepared")
        active = self._permit_ids(tenant_id, active)
        old_active_generation = active.generation
        restart_kernel(timeout=25)
        queued_snapshot, queued_turn = self._wait_turn(tenant_id, queued, {"preparing", "queued"}, timeout=5)
        active_snapshot, active_turn = self._wait_turn(tenant_id, active, {"executing"}, timeout=5)
        self.assertIn(queued_turn["state"], {"preparing", "queued"}, f"{queued.render()} {queued_snapshot!r}")
        self.assertEqual(active_turn["state"], "executing", f"{active.render()} {active_snapshot!r}")
        queued_barrier.unlink(missing_ok=True)
        active_barrier.unlink(missing_ok=True)
        self._control(tenant_id, queued.session_id, "actions", "complete")
        queued = self._permit_ids(tenant_id, queued, generation_gt=0)
        self._wait_turn(tenant_id, queued, {"completed"}, timeout=45)
        self._wait_turn(tenant_id, active, {"recovery_required"}, timeout=45)
        self.assertGreater(
            max(int(item.get("desired_generation") or 0) for item in self._observed(tenant_id, active.session_id)),
            old_active_generation,
            active.render(),
        )
        self._delete(tenant_id, queued)
        self._delete(tenant_id, active)

    def test_05_duplicate_submit_from_two_gateways(self) -> None:
        tenant_id = "default"
        ids = DiagnosticIDs(self._session("duplicate-submit"), input_id=f"input-{uuid.uuid4().hex}")
        barrier = self._control(tenant_id, ids.session_id, "barriers", "hold-after-permit")
        first = self._client("gateway-duplicate-a")
        second = self._client("gateway-duplicate-b")
        try:
            first.connect()
            second.connect()
            self._create(first, tenant_id, ids.session_id)
            second.get_session(ids.session_id, tenant_id=tenant_id)
            responses: list[dict[str, Any]] = []
            errors: list[BaseException] = []

            def submit(client: TestbedClient) -> None:
                try:
                    response, _ = self._submit(client, tenant_id, ids, "same durable input")
                    responses.append(response)
                except BaseException as exc:
                    errors.append(exc)

            threads = [threading.Thread(target=submit, args=(client,)) for client in (first, second)]
            for thread in threads:
                thread.start()
            for thread in threads:
                thread.join(12)
            self.assertFalse(errors, f"{ids.render()} errors={errors!r}")
            self.assertEqual(len(responses), 2, ids.render())
            accepted = [response["data"] for response in responses]
            self.assertEqual({item["turn_id"] for item in accepted}, {accepted[0]["turn_id"]}, ids.render())
            self.assertEqual({item["cursor"] for item in accepted}, {accepted[0]["cursor"]}, ids.render())
            ids = replace(ids, turn_id=str(accepted[0]["turn_id"]), cursor=str(accepted[0]["cursor"]))
        finally:
            first.close()
            second.close()
        ids = self._permit_ids(tenant_id, ids)
        barrier.unlink(missing_ok=True)
        self._complete(tenant_id, ids)
        replay = self._snapshot_events(tenant_id, ids.session_id)
        accepted_events = [event for event in replay if event.get("type") == "input.accepted" and event.get("data", {}).get("input_id") == ids.input_id]
        self.assertEqual(len(accepted_events), 1, f"{ids.render()} events={accepted_events!r}")
        self._delete(tenant_id, ids)

    def _snapshot_events(self, tenant_id: str, session_id: str, after_cursor: str = "cur_0") -> list[dict[str, Any]]:
        with self._client(f"replay-{session_id}") as client:
            return client.subscribe(session_id, tenant_id=tenant_id, after_cursor=after_cursor)["data"]["events"]

    def test_06_reconnect_snapshot_plus_cursor_replay(self) -> None:
        tenant_id = "default"
        ids = DiagnosticIDs(self._session("reconnect"), input_id=f"input-{uuid.uuid4().hex}")
        barrier = self._control(tenant_id, ids.session_id, "barriers", "hold-after-permit")
        first = self._client("gateway-reconnect-first")
        first.connect()
        self._create(first, tenant_id, ids.session_id)
        _, ids = self._submit(first, tenant_id, ids, "recover through snapshot and cursor replay")
        accepted_cursor = ids.cursor
        first.close()
        ids = self._permit_ids(tenant_id, ids)
        snapshot = self._snapshot(tenant_id, ids.session_id)
        replay = self._snapshot_events(tenant_id, ids.session_id, accepted_cursor)
        self.assertEqual(snapshot["data"]["cursor"], snapshot["data"]["projection"]["cursor"], ids.render())
        self.assertTrue(any(event.get("type") == "turn.state_changed" for event in replay), f"{ids.render()} replay={replay!r}")
        barrier.unlink(missing_ok=True)
        self._complete(tenant_id, ids)
        self._delete(tenant_id, ids)

    def test_07_cancellation_completion_race(self) -> None:
        tenant_id = "default"
        ids = DiagnosticIDs(self._session("cancel-complete"), input_id=f"input-{uuid.uuid4().hex}")
        barrier = self._control(tenant_id, ids.session_id, "barriers", "hold-after-permit")
        with self._client("gateway-cancel-race") as client:
            self._create(client, tenant_id, ids.session_id)
            _, ids = self._submit(client, tenant_id, ids, "cancel completion race")
            ids = self._permit_ids(tenant_id, ids)
            self._control(tenant_id, ids.session_id, "actions", "complete")
            cancel_error: list[BaseException] = []

            def cancel() -> None:
                try:
                    self._command(client, tenant_id, ids.session_id, "turn.cancel", {"turn_id": ids.turn_id})
                except BaseException as exc:
                    cancel_error.append(exc)

            thread = threading.Thread(target=cancel)
            thread.start()
            barrier.unlink(missing_ok=True)
            thread.join(12)
        if cancel_error and not (isinstance(cancel_error[0], ProtocolError) and cancel_error[0].code == "invalid_transition"):
            raise cancel_error[0]
        _, turn = self._wait_turn(tenant_id, ids, {"completed", "cancelled"})
        events = self._snapshot_events(tenant_id, ids.session_id)
        terminal = [
            event for event in events
            if event.get("type") == "turn.state_changed"
            and event.get("data", {}).get("turn_id") == ids.turn_id
            and event.get("data", {}).get("state") in {"completed", "cancelled"}
        ]
        self.assertEqual(len(terminal), 1, f"{ids.render()} final={turn!r} terminal={terminal!r}")
        self._delete(tenant_id, ids)

    def test_08_tool_failure_during_an_attempt(self) -> None:
        tenant_id = "default"
        ids = DiagnosticIDs(self._session("tool-failure"), input_id=f"input-{uuid.uuid4().hex}")
        barrier = self._control(tenant_id, ids.session_id, "barriers", "hold-after-permit")
        with self._client("gateway-tool-failure") as client:
            self._create(client, tenant_id, ids.session_id)
            _, ids = self._submit(client, tenant_id, ids, "tool failure remains attempt-scoped")
            ids = self._permit_ids(tenant_id, ids)
            permit = self._wait_observed(
                tenant_id,
                ids,
                lambda item: item.get("event") == "permit-observed" and item.get("attempt_id") == ids.attempt_id,
            )
            fence = permit["fence"]
            result = client.call_tool(
                "testbed_fail",
                {"message": "installed attempt tool failure"},
                meta={
                    "turn_id": ids.turn_id,
                    "attempt_id": ids.attempt_id,
                    "driver_instance_id": fence["driver_instance_id"],
                    "lease_id": fence["lease_id"],
                    "driver_generation": ids.generation,
                    "turn_correlation_id": ids.turn_id,
                },
            )
            self.assertEqual(result.json(), {"ok": False, "error": "installed attempt tool failure"}, ids.render())
        barrier.unlink(missing_ok=True)
        self._complete(tenant_id, ids)
        self._delete(tenant_id, ids)

    def test_09_attempt_scoped_before_turn_failure(self) -> None:
        tenant_id = "default"
        blocked_text = "installed before_turn failure"
        blocked = DiagnosticIDs(self._session("hook-failure"), input_id=f"input-{uuid.uuid4().hex}")
        follow_up = DiagnosticIDs(blocked.session_id, input_id=f"input-{uuid.uuid4().hex}")

        with self._client("gateway-hook-failure") as client:
            client.session = blocked.session_id
            client.tenant_id = tenant_id
            client.call_tool("testbed_hook_blocker_configure", {"block_turn_text": blocked_text})
            try:
                self._create(client, tenant_id, blocked.session_id)
                _, blocked = self._submit(client, tenant_id, blocked, blocked_text)
                _, blocked_turn = self._wait_turn(tenant_id, blocked, {"preparing"})
                denied = client.call_tool("testbed_hook_blocker_events", {}).json()["blocked_turns"]
                self.assertEqual(len(denied), 1, blocked.render())
                self.assertEqual(denied[0]["tenant_id"], tenant_id, blocked.render())
                self.assertEqual(denied[0]["session_id"], blocked.session_id, blocked.render())
                self.assertEqual(denied[0]["turn_id"], blocked.turn_id, blocked.render())
                self.assertEqual(denied[0]["attempt_id"], blocked_turn["attempt_id"], blocked.render())
                self.assertEqual(denied[0]["turn_correlation_id"], blocked.turn_id, blocked.render())
                self.assertTrue(denied[0]["driver_instance_id"], blocked.render())
                self.assertTrue(denied[0]["lease_id"], blocked.render())
                self.assertGreater(denied[0]["driver_generation"], 0, blocked.render())
                self.assertFalse(
                    any(item.get("event") == "permit-observed" and item.get("turn_id") == blocked.turn_id for item in self._observed(tenant_id, blocked.session_id)),
                    blocked.render(),
                )
            finally:
                client.call_tool("testbed_hook_blocker_reset", {})

            _, follow_up = self._submit(client, tenant_id, follow_up, "hook failure recovery")
            blocked = self._permit_ids(tenant_id, blocked)
            self._control(tenant_id, blocked.session_id, "actions", "complete", uuid.uuid4().hex)
            self._wait_turn(tenant_id, blocked, {"completed"})
            follow_up = self._permit_ids(tenant_id, follow_up)
            self._control(tenant_id, follow_up.session_id, "actions", "complete", uuid.uuid4().hex)
            self._wait_turn(tenant_id, follow_up, {"completed"})
        self._delete(tenant_id, blocked)

    def test_10_tenant_isolation_and_session_persistence(self) -> None:
        tenant_id = f"recovery-{uuid.uuid4().hex[:8]}"
        workspace = self.home / "workspaces" / tenant_id
        workspace.mkdir(parents=True)
        installer = self.home / ".venv" / ("Scripts/tabula-install.exe" if os.name == "nt" else "bin/tabula-install")
        subprocess.run(
            [
                str(installer), "--home", str(self.home), "tenant", "install", str(self.home / "generated-testbed"),
                "--id", tenant_id, "--root", str(workspace),
            ],
            env=os.environ.copy(),
            check=True,
            timeout=180,
        )
        tenant_driver = (self.home / "tenants" / tenant_id / "plugins" / DRIVER / "plugin.toml").resolve()
        self.assertTrue(tenant_driver.is_file() and tenant_driver.is_relative_to(self.home), tenant_driver)
        restart_runtime(timeout=25)
        shared_session = self._session("tenant-persistence")
        default_ids = DiagnosticIDs(shared_session, input_id=f"input-default-{uuid.uuid4().hex}")
        tenant_ids = DiagnosticIDs(shared_session, input_id=f"input-tenant-{uuid.uuid4().hex}")
        for scope, ids in (("default", default_ids), (tenant_id, tenant_ids)):
            self._control(scope, ids.session_id, "actions", "complete")
            with self._client(f"gateway-{scope}") as client:
                self._create(client, scope, ids.session_id)
                _, updated = self._submit(client, scope, ids, f"persisted input for {scope}")
            updated = self._permit_ids(scope, updated)
            self._wait_turn(scope, updated, {"completed"})
            if scope == "default":
                default_ids = updated
            else:
                tenant_ids = updated
        restart_kernel(timeout=25)
        default_snapshot = self._snapshot("default", shared_session)
        tenant_snapshot = self._snapshot(tenant_id, shared_session)
        default_turn = self._turn(default_snapshot, default_ids.turn_id)
        tenant_turn = self._turn(tenant_snapshot, tenant_ids.turn_id)
        self.assertEqual(default_turn["input_id"], default_ids.input_id, default_ids.render())
        self.assertEqual(tenant_turn["input_id"], tenant_ids.input_id, tenant_ids.render())
        self.assertNotEqual(default_ids.turn_id, tenant_ids.turn_id, f"default={default_ids.render()} tenant={tenant_ids.render()}")
        self._delete("default", default_ids)
        self._delete(tenant_id, tenant_ids)


def main() -> int:
    parser = argparse.ArgumentParser(description="Run installed protocol-v4 crash recovery testbed suite")
    parser.add_argument("--url", default="ws://localhost:8089/ws")
    parser.add_argument("--observer-url", default="")
    parser.add_argument("--home", default=os.environ.get("TABULA_HOME", ""))
    args = parser.parse_args()
    InstalledCrashRecovery.url = args.url
    InstalledCrashRecovery.tabula_home = args.home
    result = unittest.TextTestRunner(verbosity=2).run(
        unittest.defaultTestLoader.loadTestsFromTestCase(InstalledCrashRecovery)
    )
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())

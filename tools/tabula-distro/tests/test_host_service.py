from __future__ import annotations

import json
import os
import tempfile
import time
import unittest
from pathlib import Path
from unittest import mock

from tabula_distro import host_service
from tabula_distro import install as installmod


def _write(path: Path, content: str, *, executable: bool = False) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")
    if executable:
        path.chmod(0o755)


def _service(root: Path, marker: str, *, ready: bool = True) -> Path:
    service = root / "probe"
    readiness = 'kind = "file"\npath = "run/probe.ready"\ntimeout_seconds = 1\n'
    _write(
        service / "service.toml",
        '[service]\n'
        'id = "probe"\n'
        'entry = "run.sh"\n'
        f'args = ["{marker}"]\n'
        'platforms = ["darwin", "linux"]\n\n'
        '[readiness]\n'
        + readiness
        + '\n[shutdown]\ntimeout_seconds = 1\n',
    )
    ready_line = 'touch "$TABULA_HOME/run/probe.ready"\n' if ready else ""
    _write(
        service / "run.sh",
        "#!/bin/sh\n"
        "set -eu\n"
        "rm -f \"$TABULA_HOME/run/probe.ready\"\n"
        "printf '%s\\n' \"$1\" > \"$TABULA_HOST_SERVICE_STATE_DIR/version\"\n"
        + ready_line
        + "trap 'rm -f \"$TABULA_HOME/run/probe.ready\"; exit 0' TERM INT\n"
        + "while :; do sleep 1; done\n",
        executable=True,
    )
    return service


def _distro(root: Path, bundle: Path) -> Path:
    distro = root / "distro"
    _write(
        distro / "distro.toml",
        '[distro]\nid = "tabula.demo"\nname = "demo"\n'
        f'\n[[bundles]]\nname = "services"\nsource = "local:{bundle}"\n',
    )
    _write(distro / "templates" / "SYSTEM.md", "demo\n")
    return distro


class HostServiceTests(unittest.TestCase):
    def test_install_pins_host_service_artifact(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            bundle = root / "bundle"
            _write(bundle / "bundle.toml", '[bundle]\nname = "services"\ncomponents = ["probe"]\n')
            service = _service(bundle, "v1")

            result = installmod.install(_distro(root, bundle), root / "home")

            entry = result.lock.host_services["probe"]
            self.assertEqual(entry.artifact_sha256, host_service.artifact_digest(service))
            self.assertEqual(entry.platforms, ("darwin", "linux"))
            self.assertTrue((result.distro_path / "host-services" / "probe" / "service.toml").is_file())

    def test_no_start_auto_materializes_without_persistent_adapter(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = root / "installed"
            _service(distro / "host-services", "v1")
            with mock.patch.object(host_service.sys, "platform", "linux"):
                receipt = host_service.reconcile(home, distro, start=False, adapter="auto")[0]
            self.assertEqual(receipt["status"], "installed")
            self.assertEqual(receipt["adapter"], "")
            self.assertFalse(host_service.status(home, "probe")["running"])

    def test_process_adapter_install_restart_stop_and_purge(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = root / "installed"
            _service(distro / "host-services", "v1")
            try:
                receipt = host_service.reconcile(home, distro, adapter="process")[0]
                self.assertEqual(receipt["status"], "ready")
                self.assertTrue(host_service.status(home, "probe")["running"])
                self.assertEqual((home / "host-services" / "probe" / "state" / "version").read_text().strip(), "v1")

                receipt = host_service.restart(home, "probe", adapter="process")
                self.assertEqual(receipt["status"], "ready")
                self.assertTrue(host_service.status(home, "probe")["running"])

                host_service.stop(home, "probe")
                self.assertFalse(host_service.status(home, "probe")["running"])
                service_root = home / "host-services" / "probe"
                state = service_root / "state" / "version"
                self.assertTrue(state.is_file())
                receipt = host_service.remove(home, "probe")
                self.assertEqual(receipt["status"], "removed")
                self.assertTrue(state.is_file())
                self.assertIn('"status": "removed"', (service_root / "receipts.jsonl").read_text())
                host_service.reconcile(home, distro, adapter="process")
                host_service.remove(home, "probe", purge=True)
                self.assertFalse(service_root.exists())
            finally:
                try:
                    host_service.remove(home, "probe", purge=True)
                except host_service.HostServiceError:
                    pass

    def test_process_stop_terminates_service_process_group(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            distro = root / "installed"
            service = _service(distro / "host-services", "v1")
            script = service / "run.sh"
            script.write_text(
                script.read_text(encoding="utf-8").replace(
                    "trap '",
                    "sleep 60 &\nprintf '%s\\n' \"$!\" > \"$TABULA_HOST_SERVICE_STATE_DIR/child-pid\"\ntrap '",
                ),
                encoding="utf-8",
            )
            script.chmod(0o755)
            try:
                host_service.reconcile(home, distro, adapter="process")
                service_root = home / "host-services" / "probe"
                parent = int((service_root / "run" / "pid").read_text().strip())
                child = int((service_root / "state" / "child-pid").read_text().strip())

                host_service.stop(home, "probe")

                deadline = time.monotonic() + 2
                while time.monotonic() < deadline and (
                    host_service._process_group_exists(parent) or host_service._process_exists(child)
                ):
                    time.sleep(0.05)
                self.assertFalse(host_service._process_group_exists(parent))
                self.assertFalse(host_service._process_exists(child))
            finally:
                try:
                    host_service.remove(home, "probe", purge=True)
                except host_service.HostServiceError:
                    pass

    def test_failed_upgrade_rolls_back_running_release(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            first = root / "v1" / "installed"
            second = root / "v2" / "installed"
            _service(first / "host-services", "v1")
            _service(second / "host-services", "v2", ready=False)
            try:
                host_service.reconcile(home, first, adapter="process")
                with self.assertRaisesRegex(host_service.HostServiceError, "did not become ready"):
                    host_service.reconcile(home, second, adapter="process")

                deadline = time.monotonic() + 2
                while time.monotonic() < deadline:
                    if (home / "host-services" / "probe" / "state" / "version").read_text().strip() == "v1":
                        break
                    time.sleep(0.05)
                current = host_service.links.resolve_reference(home / "host-services" / "probe" / "current")
                self.assertIsNotNone(current)
                self.assertEqual(current.name, host_service.artifact_digest(first / "host-services" / "probe"))
                self.assertTrue(host_service.status(home, "probe")["running"])
                receipts = (home / "host-services" / "probe" / "receipts.jsonl").read_text(encoding="utf-8")
                self.assertIn('"status": "rolled_back"', receipts)
            finally:
                try:
                    host_service.remove(home, "probe", purge=True)
                except host_service.HostServiceError:
                    pass

    def test_reconcile_rejects_service_owned_by_another_distro(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            first = root / "first"
            second = root / "second"
            _service(first / "host-services", "v1")
            _service(second / "host-services", "v2")
            try:
                host_service.reconcile(home, first, adapter="process")
                with self.assertRaisesRegex(host_service.HostServiceError, "owned by distro 'first'"):
                    host_service.reconcile(home, second, adapter="process")
            finally:
                try:
                    host_service.remove(home, "probe", purge=True)
                except host_service.HostServiceError:
                    pass

    def test_interrupted_switch_recovers_previous_release(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            first = root / "v1" / "installed"
            second = root / "v2" / "installed"
            first_service = _service(first / "host-services", "v1")
            second_service = _service(second / "host-services", "v2")
            try:
                host_service.reconcile(home, first, adapter="process")
                service_root = home / "host-services" / "probe"
                previous = host_service.links.resolve_reference(service_root / "current")
                self.assertIsNotNone(previous)
                candidate = host_service._materialize_release(
                    home, second_service, host_service.load_manifest(second_service)
                )
                host_service.stop(home, "probe")
                host_service.links.replace_directory_reference(service_root / "previous", previous)
                host_service.links.replace_directory_reference(service_root / "current", candidate)
                host_service._write_json(service_root / "transaction.json", {
                    "version": 1,
                    "phase": "switched",
                    "candidate": candidate.name,
                    "previous": previous.name,
                    "was_running": True,
                })

                host_service.reconcile(home, first, adapter="process")

                current = host_service.links.resolve_reference(service_root / "current")
                self.assertEqual(current, previous)
                self.assertTrue(host_service.status(home, "probe")["running"])
                self.assertEqual((service_root / "state" / "version").read_text().strip(), "v1")
                self.assertFalse((service_root / "transaction.json").exists())
            finally:
                try:
                    host_service.remove(home, "probe", purge=True)
                except host_service.HostServiceError:
                    pass

    def test_launchd_adapter_writes_and_bootstraps_user_agent(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            release = _service(root / "release", "v1")
            manifest = host_service.load_manifest(release)
            plist = root / "agent.plist"
            completed = mock.Mock(returncode=0, stdout="state = running\npid = 123\n", stderr="")
            with mock.patch.object(host_service.sys, "platform", "darwin"), \
                    mock.patch.object(host_service, "_launchd_plist_path", return_value=plist), \
                    mock.patch.object(host_service.subprocess, "run", return_value=completed) as run:
                host_service._launchd_start(home, release, manifest)
                self.assertTrue(host_service._launchd_running(manifest.service_id))

            payload = host_service.plistlib.loads(plist.read_bytes())
            self.assertEqual(payload["ProgramArguments"][0], str(release / "run.sh"))
            self.assertIn(
                ["launchctl", "bootstrap", f"gui/{os.getuid()}", str(plist)],
                [call.args[0] for call in run.call_args_list],
            )

    def test_launchd_failed_upgrade_restores_previous_plist(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            first_source = _service(root / "first", "v1")
            second_source = _service(root / "second", "v2")
            first_manifest = host_service.load_manifest(first_source)
            second_manifest = host_service.load_manifest(second_source)
            first = host_service._materialize_release(home, first_source, first_manifest)
            second = host_service._materialize_release(home, second_source, second_manifest)
            service_root = home / "host-services" / "probe"
            host_service.links.replace_directory_reference(service_root / "current", first)
            (service_root / "adapter").write_text("launchd\n", encoding="utf-8")
            plist = root / "agent.plist"
            running = mock.Mock(returncode=0, stdout="state = running\npid = 123\n", stderr="")
            stopped = mock.Mock(returncode=0, stdout="", stderr="")
            failed = mock.Mock(returncode=1, stdout="", stderr="bad candidate")
            calls = [running, stopped, stopped, failed, stopped, stopped, running]
            with mock.patch.object(host_service.sys, "platform", "darwin"), \
                    mock.patch.object(host_service, "_launchd_plist_path", return_value=plist), \
                    mock.patch.object(host_service.subprocess, "run", side_effect=calls):
                with self.assertRaisesRegex(host_service.HostServiceError, "launchctl bootstrap"):
                    host_service._activate(
                        home,
                        second,
                        second_manifest,
                        owner="installed",
                        start=True,
                        adapter="launchd",
                    )

            payload = host_service.plistlib.loads(plist.read_bytes())
            self.assertEqual(payload["ProgramArguments"], [str((first / "run.sh").resolve()), "v1"])
            self.assertEqual(host_service.links.resolve_reference(service_root / "current"), first.resolve())
            self.assertFalse((service_root / "transaction.json").exists())

    def test_remove_fails_closed_when_process_ownership_is_unknown(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            service_root = home / "host-services" / "probe"
            release = _service(service_root / "releases" / "v1", "v1")
            host_service.links.replace_directory_reference(service_root / "current", release)
            (service_root / "run").mkdir(parents=True, exist_ok=True)
            (service_root / "run" / "pid").write_text(f"{os.getpid()}\n", encoding="utf-8")
            (service_root / "adapter").write_text("process\n", encoding="utf-8")

            with self.assertRaisesRegex(host_service.HostServiceError, "refusing to stop"):
                host_service.remove(home, "probe", purge=True)

            self.assertTrue(release.is_dir())
            self.assertTrue((service_root / "current").exists())

    def test_start_refuses_unowned_live_pid(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            source = _service(root / "source", "v1")
            manifest = host_service.load_manifest(source)
            release = host_service._materialize_release(home, source, manifest)
            service_root = home / "host-services" / "probe"
            host_service.links.replace_directory_reference(service_root / "current", release)
            (service_root / "run").mkdir(parents=True, exist_ok=True)
            (service_root / "run" / "pid").write_text(f"{os.getpid()}\n", encoding="utf-8")

            service_status = host_service.status(home, "probe")
            self.assertFalse(service_status["running"])
            self.assertTrue(service_status["ownership_mismatch"])
            with self.assertRaisesRegex(host_service.HostServiceError, "refusing to reuse"):
                host_service.start(home, "probe", adapter="process")

    def test_manifest_rejects_non_executable_entry(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            _write(root / "service.toml", '[service]\nid="probe"\nentry="run.sh"\n')
            _write(root / "run.sh", "#!/bin/sh\n")
            with self.assertRaisesRegex(host_service.HostServiceError, "not executable"):
                host_service.load_manifest(root)

    def test_cli_receipt_is_json_serializable(self) -> None:
        receipt = {"id": "probe", "status": "removed"}
        self.assertEqual(json.loads(json.dumps(receipt)), receipt)


if __name__ == "__main__":
    unittest.main()

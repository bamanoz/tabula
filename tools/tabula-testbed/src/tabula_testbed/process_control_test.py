from __future__ import annotations

import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from . import process_control


class ProcessControlTest(unittest.TestCase):
    def test_restart_kernel_uses_runner_command_and_updates_pid_file(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            root = Path(raw)
            pid_file = root / "kernel.pid"
            pid_file.write_text("41\n", encoding="utf-8")
            env = {
                "TABULA_TESTBED_KERNEL_PID_FILE": str(pid_file),
                "TABULA_TESTBED_KERNEL_COMMAND": json.dumps(["tabula", "serve"]),
                "TABULA_TESTBED_KERNEL_OUT": str(root / "out.log"),
                "TABULA_TESTBED_KERNEL_ERR": str(root / "err.log"),
            }
            process = mock.Mock(pid=84)
            with mock.patch.dict(os.environ, env, clear=False), \
                    mock.patch.object(process_control, "_stop_process") as stop, \
                    mock.patch.object(process_control.subprocess, "Popen", return_value=process) as popen, \
                    mock.patch.object(process_control, "_wait_for_kernel") as wait_kernel, \
                    mock.patch.object(process_control, "_wait_for_runtime") as wait_runtime:
                self.assertEqual(process_control.restart_kernel(), 84)

            stop.assert_called_once_with(41, timeout=5)
            popen.assert_called_once()
            wait_kernel.assert_called_once_with(20)
            wait_runtime.assert_called_once_with(20)
            self.assertEqual(pid_file.read_text(encoding="utf-8"), "84\n")

    def test_restart_runtime_waits_for_new_pid(self) -> None:
        with mock.patch.object(process_control, "runtime_pid", side_effect=[41, 41, 84]), \
                mock.patch.object(process_control, "_stop_process") as stop, \
                mock.patch.object(process_control.time, "sleep"):
            self.assertEqual(process_control.restart_runtime(timeout=1), 84)
        stop.assert_called_once_with(41, timeout=5)


if __name__ == "__main__":
    unittest.main()

from __future__ import annotations

import importlib.util
import json
import os
import shutil
import subprocess
import tempfile
from pathlib import Path

import pytest


ROOT = Path(__file__).resolve().parents[1]


def _load_guardian_boot_module():
    boot_path = ROOT / "distrib" / "guardian" / "boot.py"
    spec = importlib.util.spec_from_file_location("tabula_guardian_boot", boot_path)
    mod = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(mod)
    return mod


def test_guardian_boot_exposes_single_tool_and_no_builtins(monkeypatch):
    monkeypatch.setenv("TABULA_HOME", str(ROOT))
    boot = _load_guardian_boot_module()
    config = json.loads(subprocess.check_output([str(ROOT / ".venv" / "bin" / "python3"), str(ROOT / "distrib" / "guardian" / "boot.py")], env={**os.environ, "TABULA_HOME": str(ROOT)}))
    assert config["kernel_tools"] == []
    assert [tool["name"] for tool in config["tools"]] == ["execute_code"]


@pytest.mark.skipif(shutil.which("docker") is None, reason="docker not installed")
def test_guardian_runtime_persists_scratchpad_and_state():
    with tempfile.TemporaryDirectory() as tmp:
        home = Path(tmp)
        session = "guardian-test"
        env = os.environ.copy()
        env["TABULA_HOME"] = str(home)
        env["PYTHONPATH"] = str(ROOT)

        code = (
            f"import sys\nsys.path.insert(0, {json.dumps(str(ROOT / 'distrib' / 'guardian' / 'skills' / 'guardian-lib'))})\n"
            "from runtime import reset_guardian_turn, execute_guardian_code, read_guardian_scratchpad, shutdown_sandbox_container\n"
            f"reset_guardian_turn('{session}', {json.dumps(tmp)})\n"
            f"print(execute_guardian_code(\"counter = globals().get('counter', 0) + 1\\nscratchpad['value'] = counter\\nprint(counter)\", session='{session}', workspace_root={json.dumps(tmp)}))\n"
            f"print(execute_guardian_code(\"counter = globals().get('counter', 0) + 1\\nprint(counter)\", session='{session}', workspace_root={json.dumps(tmp)}))\n"
            f"print(read_guardian_scratchpad('{session}'))\n"
            f"shutdown_sandbox_container('{session}')\n"
        )
        out = subprocess.check_output([str(ROOT / ".venv" / "bin" / "python3"), "-c", code], env=env, text=True)
        assert "('1', False)" in out
        assert "('2', False)" in out
        assert "'value': 1" in out


def test_guardian_ws_answer_requires_verify_and_fields():
    with tempfile.TemporaryDirectory() as tmp:
        answer_file = Path(tmp) / "answer.json"
        tracking_file = Path(tmp) / "tracking.json"
        tracking_file.write_text("{}", encoding="utf-8")
        code = (
            "import json, sys\n"
            f"sys.path.insert(0, {json.dumps(str(ROOT / 'distrib' / 'guardian' / 'skills' / 'execute-code' / 'sandbox'))})\n"
            "from workspace import GuardianWorkspace\n"
            f"ws = GuardianWorkspace(workspace_root={json.dumps(tmp)}, answer_file={json.dumps(str(answer_file))}, tracking_file={json.dumps(str(tracking_file))})\n"
            "scratchpad = {'answer': 'ok', 'outcome': 'OUTCOME_OK', 'refs': []}\n"
            "try:\n"
            "    ws.answer(scratchpad, lambda sp: False)\n"
            "except Exception as exc:\n"
            "    print(type(exc).__name__)\n"
        )
        out = subprocess.check_output([str(ROOT / ".venv" / "bin" / "python3"), "-c", code], text=True)
        assert "ValueError" in out
        assert not answer_file.exists()


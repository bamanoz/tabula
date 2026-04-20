from __future__ import annotations

from pathlib import Path


ROOT = Path(__file__).resolve().parent

PYTHON_E2E_FILES = {
    "test_hooks_e2e.py",
    "test_mcp_e2e.py",
    "test_mock_driver_e2e.py",
    "test_observer.py",
    "test_openai_subagent_e2e.py",
    "test_subagent_e2e.py",
}

PYTHON_SMOKE_FILES = {
    "test_runtime_smoke.py",
}

PYTHON_MANUAL_FILES = {
    "test_hooks_real.py",
    "test_mock_driver_diag.py",
    "test_openai_subagent_diag.py",
    "test_real_subagent.py",
    "test_subagent_diag.py",
}

PYTHON_CONTRACT_FILES = {
    "skills/hook-permissions/test_permissions.py",
    "skills/lib/test_protocol.py",
}


def pytest_configure(config):
    config.addinivalue_line("markers", "unit: fast logic-only coverage")
    config.addinivalue_line("markers", "smoke: lightweight runtime boot/connect checks")
    config.addinivalue_line("markers", "e2e: heavier runtime and integration flows")
    config.addinivalue_line("markers", "contract: protocol and extension contract checks")
    config.addinivalue_line("markers", "manual: helper or real-env test not part of CI matrix")


def pytest_collection_modifyitems(config, items):
    for item in items:
        path = Path(str(item.fspath)).resolve()
        rel = path.relative_to(ROOT).as_posix()
        name = path.name

        if rel in PYTHON_CONTRACT_FILES:
            item.add_marker("contract")
            continue

        if name in PYTHON_MANUAL_FILES:
            item.add_marker("manual")
            continue

        if name in PYTHON_SMOKE_FILES:
            item.add_marker("smoke")
            continue

        if name in PYTHON_E2E_FILES:
            item.add_marker("e2e")
            continue

        item.add_marker("unit")

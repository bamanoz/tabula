from __future__ import annotations

from pathlib import Path


ROOT = Path(__file__).resolve().parent

PYTHON_CONTRACT_FILES = {
    "skills/_lib/test_protocol.py",
}


def pytest_configure(config):
    config.addinivalue_line("markers", "unit: fast logic-only coverage")
    config.addinivalue_line("markers", "contract: protocol and extension contract checks")


def pytest_collection_modifyitems(config, items):
    for item in items:
        path = Path(str(item.fspath)).resolve()
        rel = path.relative_to(ROOT).as_posix()
        if rel in PYTHON_CONTRACT_FILES:
            item.add_marker("contract")
        else:
            item.add_marker("unit")

#!/usr/bin/env python3
"""Tests for service/launcher PATH propagation into spawned skills."""

from __future__ import annotations

from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def test_launchd_service_sets_tabula_path_with_venv_first():
    plist = (ROOT / "service" / "com.tabula.kernel.plist").read_text(encoding="utf-8")

    assert "<key>TABULA_PATH</key>" in plist
    assert "__TABULA_HOME__/.venv/bin:__TABULA_HOME__/bin" in plist


def test_tabula_server_exports_tabula_path_with_venv_first():
    script = (ROOT / "bin" / "tabula-server").read_text(encoding="utf-8")

    assert 'export TABULA_PATH="${TABULA_PATH:-$TABULA_HOME/.venv/bin:$TABULA_HOME/bin:$PATH}"' in script


def test_install_dev_persists_tabula_path_for_future_shells():
    script = (ROOT / "scripts" / "install-dev.sh").read_text(encoding="utf-8")

    assert 'TABULA_PATH_VALUE="$TABULA_HOME/.venv/bin:$TABULA_HOME/bin:$PATH"' in script
    assert 'TABULA_PATH_LINE=' in script
    assert 'export TABULA_PATH=' in script
    assert 'echo "$TABULA_PATH_LINE" >> "$SHELL_RC"' in script

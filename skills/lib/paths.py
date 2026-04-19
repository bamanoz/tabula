#!/usr/bin/env python3
"""Shared TABULA_HOME path conventions for skills.

Conventions for skill-owned files:
- config/skills/<skill>.toml for declarative scalar config
- config/skills/<skill>/... for extra structured config files
- data/<skill>/... for durable mutable records
- state/<skill>/... for rebuildable indexes/caches
- run/<skill>/... for pid files and runtime endpoints
- logs/<skill>/... for logs

Some bootstrap-owned files still live in legacy locations because boot.py reads
them directly and the kernel/core layer is intentionally untouched for now.
"""

from __future__ import annotations

import os
from pathlib import Path


def tabula_home() -> Path:
    return Path(os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula")))


def secrets_file() -> Path:
    return tabula_home() / "secrets.json"


def global_config_file() -> Path:
    return tabula_home() / "config" / "global.toml"


def skill_config_toml(skill_id: str) -> Path:
    return tabula_home() / "config" / "skills" / f"{skill_id}.toml"


def skill_config_dir(skill_id: str) -> Path:
    return tabula_home() / "config" / "skills" / skill_id


def skill_data_dir(skill_id: str) -> Path:
    return tabula_home() / "data" / skill_id


def skill_state_dir(skill_id: str) -> Path:
    return tabula_home() / "state" / skill_id


def skill_run_dir(skill_id: str) -> Path:
    return tabula_home() / "run" / skill_id


def skill_logs_dir(skill_id: str) -> Path:
    return tabula_home() / "logs" / skill_id


def ensure_parent(path: Path) -> Path:
    path.parent.mkdir(parents=True, exist_ok=True)
    return path

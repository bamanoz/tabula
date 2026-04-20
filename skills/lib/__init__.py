"""Shared runtime helpers for Tabula skills."""

import os

from .config import SkillConfigError, get_tabula_home, load_global_config, load_skill_config


def load_env() -> None:
    """Load $TABULA_HOME/env into os.environ (skip comments and blanks)."""
    home = os.environ.get("TABULA_HOME", os.path.expanduser("~/.tabula"))
    env_file = os.path.join(home, ".env")
    if not os.path.isfile(env_file):
        return
    with open(env_file) as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            key, _, value = line.partition("=")
            if key:
                os.environ.setdefault(key.strip(), value.strip())

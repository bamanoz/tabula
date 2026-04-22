"""Shared runtime helpers for Tabula skills."""

from .config import SkillConfigError, get_tabula_home, load_global_config, load_skill_config

# Kernel + lib version. Source of truth: <repo>/VERSION (synced by release tooling).
# Distros declare a [requires].kernel constraint that is checked against this
# value at install time by tabula-distro.
__version__ = "0.8.0"

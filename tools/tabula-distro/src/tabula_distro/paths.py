"""Shared TABULA_HOME path conventions for Tabula skills and plugins.

All functions return ``pathlib.Path`` values with ``~`` expanded. None of the
helpers create directories — call :func:`ensure_runtime_dirs` (or
:func:`ensure_parent`) explicitly when a caller needs the directory to exist.

``TABULA_HOME`` is read from the environment on every call so tests that
mutate ``os.environ`` work as expected. Tests that need to pin a value
without touching the environment can call :func:`set_for_tests`; pass
``None`` to clear the override.

Symlinks are intentionally **not** resolved here. The Go side mirrors this
behaviour: paths are made absolute but ``filepath.EvalSymlinks`` is not
applied. This keeps macOS ``/var`` vs ``/private/var`` and other symlink
chains from leaking into config values, log paths, and test expectations.
"""

from __future__ import annotations

import os
import sys
import warnings
from pathlib import Path

_DEFAULT_HOME = "~/.tabula"

_test_override: Path | None = None


def _expand(path: Path) -> Path:
    """Return ``path`` with ``~`` expanded and made absolute.

    Does not call :meth:`Path.resolve` — see module docstring for why we
    avoid symlink resolution.
    """
    expanded = Path(os.path.expanduser(str(path)))
    if not expanded.is_absolute():
        expanded = expanded.absolute()
    return expanded


def tabula_home() -> Path:
    """Return the ``TABULA_HOME`` root.

    Honours ``$TABULA_HOME`` if set and non-empty, otherwise falls back to
    ``~/.tabula``. Read from the environment on every call.
    """
    if _test_override is not None:
        return _test_override
    raw = os.environ.get("TABULA_HOME", "").strip() or _DEFAULT_HOME
    return _expand(Path(raw))


def set_for_tests(path: Path | str | None) -> None:
    """Override the ``TABULA_HOME`` value for unit tests.

    Pass ``None`` to clear the override so the next call re-reads
    ``$TABULA_HOME`` from the environment.
    """
    global _test_override
    if path is None:
        _test_override = None
        return
    _test_override = _expand(Path(path))


# --- top-level subdirectories -----------------------------------------------


def config_dir() -> Path:
    return tabula_home() / "config"


def state_dir() -> Path:
    return tabula_home() / "state"


def data_dir() -> Path:
    return tabula_home() / "data"


def cache_dir() -> Path:
    return tabula_home() / "cache"


def run_dir() -> Path:
    return tabula_home() / "run"


def logs_dir() -> Path:
    return tabula_home() / "logs"


def plugins_dir() -> Path:
    return tabula_home() / "plugins"


def skills_dir() -> Path:
    return tabula_home() / "skills"


def tenants_dir() -> Path:
    return tabula_home() / "tenants"


def testing_skills_dir() -> Path:
    return tabula_home() / "testing" / "skills"


def templates_dir() -> Path:
    return tabula_home() / "templates"


# --- tenant resolution ------------------------------------------------------


def tenant_root() -> Path | None:
    raw = os.environ.get("TABULA_TENANT_DIR", "").strip()
    if raw:
        return _expand(Path(raw))
    return None


def tenant_dir(tenant_id: str | None = None) -> Path:
    """Return the tenant directory.

    If ``$TABULA_TENANT_DIR`` is set it wins. Otherwise, when ``tenant_id``
    is provided, resolve to ``tenants_dir() / tenant_id``. Raises
    ``RuntimeError`` if neither input is available.
    """
    explicit = tenant_root()
    if explicit is not None:
        return explicit
    if tenant_id:
        return tenants_dir() / tenant_id
    raise RuntimeError(
        "tenant_dir() requires TABULA_TENANT_DIR or an explicit tenant_id",
    )


def tenant_root_or_home(kind: str) -> Path:
    explicit = tenant_root()
    if explicit is not None:
        return explicit
    warnings.warn(
        f"TABULA_TENANT_DIR is not set; falling back to global path for {kind}",
        RuntimeWarning,
        stacklevel=2,
    )
    return tabula_home()


# --- well-known files -------------------------------------------------------


def secrets_path() -> Path:
    return tabula_home() / "secrets.json"


# Backwards-compatible alias retained for existing call sites within the
# same release. Prefer ``secrets_path()`` in new code.
def secrets_file() -> Path:
    return secrets_path()


def global_config_file() -> Path:
    return config_dir() / "global.toml"


def runtime_config_file() -> Path:
    """Return ``$TABULA_HOME/config/runtime.toml``.

    The single source of truth for plugin/skill layout, kernel endpoints,
    and the active distro (after issue 004/007). Written by ``tabula-install``
    and read by the kernel and runtime.
    """
    return config_dir() / "runtime.toml"


# --- per-component config / state / logs -----------------------------------


def skill_config_toml(skill_id: str) -> Path:
    return config_dir() / "skills" / f"{skill_id}.toml"


def skill_config_dir(skill_id: str) -> Path:
    return config_dir() / "skills" / skill_id


def app_config_toml(app_id: str) -> Path:
    return config_dir() / "apps" / app_id / "config.toml"


def plugin_config_toml(plugin_id: str) -> Path:
    return config_dir() / "plugins" / plugin_id / "config.toml"


def skill_data_dir(skill_id: str) -> Path:
    return data_dir() / skill_id


def skill_state_dir(skill_id: str) -> Path:
    return tenant_root_or_home("skill_state_dir") / "state" / "skills" / skill_id


def plugin_state_dir(plugin_id: str) -> Path:
    return tenant_root_or_home("plugin_state_dir") / "state" / "plugins" / plugin_id


def plugin_logs_dir(plugin_id: str) -> Path:
    return logs_dir() / "plugins" / plugin_id


def app_state_dir(app_id: str) -> Path:
    return state_dir() / "apps" / app_id


def app_logs_dir(app_id: str) -> Path:
    return logs_dir() / "apps" / app_id


def plugin_run_dir(plugin_id: str) -> Path:
    return run_dir() / "plugins" / plugin_id


def skill_run_dir(skill_id: str) -> Path:
    return run_dir() / skill_id


def skill_logs_dir(skill_id: str) -> Path:
    return logs_dir() / skill_id


# --- runtime helpers --------------------------------------------------------


def ensure_parent(path: Path) -> Path:
    path.parent.mkdir(parents=True, exist_ok=True)
    return path


def ensure_runtime_dirs() -> None:
    """Create the standard top-level subdirectories under ``TABULA_HOME``.

    Safe to call repeatedly. Does not touch ``tenants/`` contents — those
    are owned by the installer / tenant materializer.
    """
    for d in (
        config_dir(),
        state_dir(),
        data_dir(),
        cache_dir(),
        run_dir(),
        logs_dir(),
        plugins_dir(),
        skills_dir(),
        tenants_dir(),
    ):
        d.mkdir(parents=True, exist_ok=True)


def bootstrap_sys_path() -> None:
    """Prepend the standard Tabula import roots to ``sys.path`` idempotently.

    Adds, in order:

    - ``$TABULA_HOME/packages/python/src`` (declared package exports).
    - ``$TABULA_HOME`` itself (so distro boot modules sitting directly under
      ``$TABULA_HOME`` are importable).

    Each entry is added at most once — repeated calls are no-ops.
    """
    home = tabula_home()
    # Insert in reverse priority so the final order is
    # [packages/python/src, home, ...existing].
    candidates = [home, home / "packages" / "python" / "src"]
    for candidate in candidates:
        entry = str(candidate)
        if entry not in sys.path:
            sys.path.insert(0, entry)

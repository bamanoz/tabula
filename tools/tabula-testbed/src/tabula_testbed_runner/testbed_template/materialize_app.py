#!/usr/bin/env python3
from __future__ import annotations

import os
from pathlib import Path
import tomllib


def toml_string(value: str) -> str:
    return '"' + value.replace('\\', '\\\\').replace('"', '\\"') + '"'


def main() -> int:
    tenant_dir = Path(os.environ["TABULA_TENANT_DIR"])
    values_path = Path(os.environ["TABULA_APP_VALUES"])
    values = tomllib.loads(values_path.read_text(encoding="utf-8")) if values_path.is_file() else {}
    workspace = values.get("workspace") if isinstance(values.get("workspace"), dict) else {}
    project = Path(str(workspace.get("path") or tenant_dir)).absolute()

    fs_config = tenant_dir / "config" / "plugins" / "fs"
    fs_config.mkdir(parents=True, exist_ok=True)
    (fs_config / "config.toml").write_text(
        f"roots = [{toml_string(str(project))}]\nfollow_symlinks = false\n",
        encoding="utf-8",
    )

    exec_config = tenant_dir / "config" / "plugins" / "exec"
    exec_config.mkdir(parents=True, exist_ok=True)
    (exec_config / "config.toml").write_text(
        f"cwd_default = {toml_string(str(project))}\n"
        "timeout_default_seconds = 5\n"
        f'env_extra = {{ TABULA_TENANT_ID = {toml_string(os.environ["TABULA_APP_ID"])} }}\n',
        encoding="utf-8",
    )

    permissions_config = tenant_dir / "config" / "plugins" / "hook-permissions"
    permissions_config.mkdir(parents=True, exist_ok=True)
    (permissions_config / "config.toml").write_text('default = "allow"\ndeny_untyped = true\n', encoding="utf-8")

    audit_dir = tenant_dir / "config"
    audit_dir.mkdir(parents=True, exist_ok=True)
    (audit_dir / "materializer.txt").write_text(
        "\n".join([
            f"app={os.environ['TABULA_APP_ID']}",
            f"phase={os.environ.get('TABULA_APP_PHASE', '')}",
            f"dry_run={os.environ.get('TABULA_APP_DRY_RUN', '')}",
            f"values={values_path}",
            f"lock={os.environ.get('TABULA_APP_LOCK', '')}",
            f"lock_json={os.environ.get('TABULA_APP_LOCK_JSON', '')}",
        ]) + "\n",
        encoding="utf-8",
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

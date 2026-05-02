# Tech Context

## execute-remote-runtime-program — CREATIVE notes (2026-05-02)

- Primary implementation language in `tabula`: Go.
- Runtime daemon binary target: `cmd/tabula-runtime`.
- Kernel/runtime packages introduced under `internal/runtime/...`.
- Cross-repo build implications:
  - `tabula-bundles` owns reusable plugin/skill SDK migration and workspace `fs`/`exec` plugins.
  - `tabula-distrib` owns distro policy and gateway/plugin migration.
- Transport technologies selected by accepted ADR/program direction:
  - Local Runtime API: unix socket at `$TABULA_HOME/run/runtime.sock`.
  - Remote Runtime API: WSS over HTTP server path `/runtime`.
  - SSH backend: out-of-process system `ssh` to `tabula-runtime stdio`.
  - Runtime-to-worker protocol: stdio NDJSON.
- Config technologies:
  - TOML for `$TABULA_HOME/config/global.toml`, `$TABULA_HOME/config/runtime.toml`, and `$TABULA_HOME/tenants/<id>/tenant.toml`.
  - JSON for Runtime API frames and status output.
- Testing posture:
  - Focused Go/Python tests first.
  - Installed-layout testbed gates for runtime/install/fan-out behavior.
  - Cross-repo grep guards for atomic deletion gates.

# SECURITY Scope — skill-plugin-architecture / Attempt 1

Date: 2026-04-27
Task base commit: `88e6320107d46bb0c15119abc7b4dd83aeca3420`

## BUILD delta reviewed

`git diff --stat` from the working tree shows the BUILD is currently represented as unstaged repo changes (no post-base commits on `HEAD`). Scope was identified from the Memory Bank BUILD log plus the working-tree diff.

Primary modified/added areas audited:

- `cmd/tabula/main.go`, `cmd/tabula/main_test.go`
  - Boot config `skills`/legacy `tools` handling.
  - `plugins` boot entries and `hub.LoadPlugins` call path.
  - `/internal/snapshot/plugins` HTTP diagnostic endpoint.
  - WebSocket origin hardening remains present.
- `internal/kernel/**`
  - Removal of LLM-visible kernel builtin dispatch for `shell_exec`, `process_spawn`, `process_kill`, `process_list`.
  - Unified `toolDispatch` for skill and plugin tools.
  - Plugin registry, hook subscriber adapter, plugin tool dispatch, plugin protocol handling, snapshots.
  - Plugin runtime, manifest parser, supervisor/backoff, process-group helpers.
  - Dynamic-skill hook replacement tests and plugin live tests.
- `examples/plugin-hello/**`, `examples/plugin-sdk-python/**`, `scripts/test-plugin-hello.sh`
  - Reference Python plugin, minimal in-tree Python plugin SDK, live smoke wrapper.
- `tools/tabula-distro/src/tabula_distro/**`, `tools/tabula-distro/tests/test_install.py`
  - Mixed skills/plugins install, `bundle.toml` components, standalone plugins, lock v2 with `plugins`.
- `docs/**`
  - Plugin authoring docs, skill/distro/architecture docs updates.

Large removed docs under `docs/plans/*` appear to be planning-material cleanup and were not part of runtime security surface except as source-doc context.

## New dependencies introduced

- Go: `github.com/BurntSushi/toml v1.5.0` added in `go.mod`/`go.sum` for `plugin.toml` parsing.
- No new npm lockfile/package dependency was added.
- No pinned Python runtime dependency file changes were detected in the audited diff. The in-tree reference SDK uses only Python stdlib.

## Audit commands executed

- `git diff --stat`
- `git diff --name-only`
- `git diff -- go.mod go.sum`
- `npm audit --json` → degraded: failed with `ENOLOCK` because no npm lockfile exists.
- `pip-audit --format=json` → degraded: `pip-audit` command not available.
- `grep -rEn "(api[_-]?key|secret|token|password|bearer)" internal/kernel cmd/tabula tools/tabula-distro/src examples/plugin-hello examples/plugin-sdk-python --exclude-dir=__pycache__`
- `grep -rEn "(console\.log|logger\.|slog\.|fmt\.Print|print\()" internal/kernel cmd/tabula tools/tabula-distro/src examples/plugin-hello examples/plugin-sdk-python --exclude-dir=__pycache__`

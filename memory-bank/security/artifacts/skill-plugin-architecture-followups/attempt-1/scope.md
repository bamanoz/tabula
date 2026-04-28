# Security Scope — skill-plugin-architecture-followups — Attempt 1

Date: 2026-04-27

## Source of scope

- Active task: `skill-plugin-architecture-followups`
- Task base commit from `memory-bank/activeContext.md`: `48f84f8aa981c575259d42353557b48a305b3a0a`
- BUILD handoff states the repo-local safe hardening lane is complete and the D1.11(b) / Phase 6 deletion lanes remain external-evidence-gated.
- Current reopened BUILD handoff states Agents 1–4 performed focused regression/evidence verification only; no evidence-safe deletion of D1.11(b) spawn bridges or Phase 6 SDK/lib compatibility paths was available.
- Because BUILD changes are present in the working tree rather than committed on top of the base, audit scope used `git diff --stat`, `git diff --name-only`, Memory Bank BUILD logs, and direct reads of changed files.

## BUILD changes audited

Product/source files in scope:

- `README.md`
- `cmd/tabula/kernel.tools.json`
- `cmd/tabula/main.go`
- `cmd/tabula/main_test.go`
- `docs/ARCHITECTURE.md`
- `docs/PLUGIN_AUTHORING.md`
- `docs/SKILL_AUTHORING.md`
- `docs/plans/SKILL_PLUGIN_ARCHITECTURE.md`
- `internal/kernel/plugin/catalog_validation.go`
- `internal/kernel/plugin/handle.go`
- `internal/kernel/plugin/handle_test.go`
- `internal/kernel/plugin/runtime.go`
- `internal/kernel/plugin/runtime_test.go`
- `internal/kernel/plugin_runtime.go`
- `internal/kernel/plugin_runtime_test.go`
- `internal/kernel/plugin_tools.go`
- `internal/kernel/plugin_tools_test.go`
- `skills/_pylib/SKILL.md`
- `skills/_pylib/protocol.py`
- `skills/_pylib/test_protocol.py`
- `skills/_tslib/src/paths.ts`
- `skills/_tslib/src/protocol.ts`
- `skills/_tslib/tests/protocol.test.ts`
- `tests/README.md`
- `tools/tabula-distro/src/tabula_distro/install.py`
- `tools/tabula-distro/tests/test_install.py`

Memory Bank / evidence files in scope:

- `memory-bank/tasks.md`
- `memory-bank/activeContext.md`
- `memory-bank/progress.md`
- `memory-bank/projectbrief.md`
- `memory-bank/creative/creative-plugin-runtime.md`
- `memory-bank/creative/creative-sdk-and-distro.md`
- `memory-bank/qa/artifacts/skill-plugin-architecture-followups/external-bundle-migration-matrix.md`

## Dependency changes

- `git diff -- package.json package-lock.json requirements.txt pyproject.toml go.mod go.sum skills/_tslib/package.json skills/_tslib/bun.lock tools/tabula-distro/pyproject.toml examples/plugin-sdk-python/pyproject.toml .opencode/package.json .opencode/package-lock.json` produced no output.
- No new package dependencies were introduced by BUILD.
- `npm audit --json` was still run under `.opencode/` because a lockfile exists there; see `audit.log`.
- `pip-audit --format=json` was attempted from repo root and was unavailable; no Python dependency manifests changed in BUILD.

## Key security-sensitive deltas

- `/internal/snapshot/plugins` is now guarded by loopback `RemoteAddr` plus loopback/local `Host` checks before returning plugin operational metadata.
- Boot skill descriptors and distro `SKILL.md` advertised tools now require non-empty `name` and `exec`, with duplicate rejection.
- Dynamic plugin `register` / `update_tools` catalogs now validate non-empty tool names, duplicate names, non-negative deadlines, and subscription event names before unsafe mutation.
- Malformed plugin `event_reply` / `tool_result` paths now reject or release pending calls without synthesizing permissive outcomes.
- Active docs / temporary SDK surfaces were aligned to remove stable generic `api.spawn`, common `TABULA_SPAWN_TOKEN`, and populated default-kernel-tool contracts.
- External migration rows remain `unknown/blocker`; D1.11(b) spawn bridge deletion and Phase 6 SDK/lib physical removal were not performed.

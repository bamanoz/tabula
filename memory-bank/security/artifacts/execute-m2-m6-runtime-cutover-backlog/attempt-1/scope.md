# SECURITY Scope — execute-m2-m6-runtime-cutover-backlog

- Date: 2026-05-03
- Attempt: 1 / 3
- Task base commit: `3c65cc15253d0db5fb5e63b1248951b521716e64`
- Audit basis: `git diff --stat 3c65cc15253d0db5fb5e63b1248951b521716e64`

## BUILD changes audited

- Runtime auth/attach and kernel routing: `cmd/tabula/main.go`, `cmd/tabula/local_runtime.go`, `internal/kernel/runtime_attach.go`, `internal/kernel/runtime_registry.go`, `internal/kernel/runtime_async.go`, `internal/kernel/tool_dispatch.go`, `internal/kernel/tool_service.go`, `internal/kernel/snapshot.go`
- Runtime daemon/config/dialer: `cmd/tabula-runtime/config/config.go`, `cmd/tabula-runtime/dialer/dialer.go`, `cmd/tabula-runtime/daemon/handler.go`, `cmd/tabula-runtime/main.go`
- Runtime worker/runtime protocols: `internal/runtime/wire/types.go`, `internal/runtime/conn/conn.go`, `internal/runtime/worker/wire/types.go`, `cmd/tabula-runtime/policy/bare/bare.go`, `cmd/tabula-runtime/pool/pool.go`
- Packaging/install/testbed surfaces: `.goreleaser.yaml`, `Makefile`, `scripts/install*.sh`, `scripts/install*.ps1`, `tools/tabula-testbed/src/tabula_testbed_runner/runner.py`
- Supporting tests: runtime/kernel/daemon/pool/dialer/manifest test files listed by the diff

## Diff summary

- 49 files changed
- 4059 insertions / 197 deletions

## New dependencies introduced

- None detected. No dependency manifests or lockfiles changed (`go.mod`, `go.sum`, `package.json`, `package-lock.json`, `requirements*.txt`, `pyproject.toml` were unchanged in the BUILD diff).

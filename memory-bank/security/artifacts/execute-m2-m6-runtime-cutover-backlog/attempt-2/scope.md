# Scope — execute-m2-m6-runtime-cutover-backlog / attempt 2

- Base commit: `3c65cc15253d0db5fb5e63b1248951b521716e64`
- Audit target: current working tree diff from base commit (`git diff --stat 3c65cc15253d0db5fb5e63b1248951b521716e64`)
- Changed files in scope: 50
- Diff summary: 4,122 insertions / 197 deletions

## Scope summary

- Runtime auth / attach / registry / snapshots:
  - `internal/kernel/runtime_attach.go`
  - `internal/kernel/runtime_async.go`
  - `internal/kernel/runtime_registry.go`
  - `internal/kernel/snapshot.go`
- Managed local runtime supervision / config:
  - `cmd/tabula/main.go`
  - `cmd/tabula/local_runtime.go`
  - `cmd/tabula-runtime/config/config.go`
- Runtime daemon / worker pool / protocol:
  - `cmd/tabula-runtime/daemon/handler.go`
  - `cmd/tabula-runtime/pool/pool.go`
  - `cmd/tabula-runtime/policy/bare/bare.go`
  - `internal/runtime/conn/conn.go`
  - `internal/runtime/wire/types.go`
  - `internal/runtime/worker/wire/types.go`
- Installed-layout / packaging smoke:
  - `.goreleaser.yaml`
  - `Makefile`
  - `scripts/install*.{sh,ps1}`
  - `tools/tabula-testbed/src/tabula_testbed_runner/runner.py`

## Dependency delta

- No dependency manifests changed in this diff (`go.mod`, `go.sum`, `package*.json`, `requirements*.txt`, `pyproject.toml` unchanged).
- New dependencies introduced: none.

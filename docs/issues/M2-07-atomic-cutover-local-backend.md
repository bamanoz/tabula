# M2-07 — Atomic cutover: local backend + delete stdio + bootstrap wiring

Status: open
Phase: M2
Type: AFK (review carefully — largest single change)
Repo: tabula
Labels: needs-triage, area/runtime, area/kernel, phase/m2

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M2)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§10)

## What to build

The atomic switchover described in Q10 / ADR §10. In one
change:

1. Add the `local` backend to the kernel.
2. Delete the entire stdio plugin transport from the kernel.
3. Wire `tabula serve` to fork `tabula-runtime` as a managed
   child.
4. Update `bootstrap.sh` to use the new flow and `tabula
   status --json` for readiness checks.

Skill execution intentionally remains via `process_manager.go`
in this slice (Q-seq-a intermediate state); M3 deletes that.

### Kernel changes

- New `internal/runtime/backend/local/`:
  - `Backend.Connect(ctx)` forks `tabula-runtime` via
    `exec.CommandContext` with `Cmd.Cancel`.
  - Sets up unix socket listener at
    `$TABULA_HOME/run/runtime.sock` BEFORE forking.
  - Forked runtime dials in within timeout (configurable, default
    10s); Connect returns the established `RuntimeConn`.
  - Supervises: on runtime exit, restart with backoff; expose
    last-restart info via Hub introspection.
- `cmd/tabula/serve.go` (or wherever serve lives): orchestrates
  socket listener + local backend + Hub initialization.
- `Hub`: gains `RuntimeRegistry` field; `RouteToolCall` for
  plugin tools goes through the registry's `RuntimeConn.Invoke`.
- **Delete** `internal/kernel/plugin/runtime.go` spawn / pipe /
  supervisor logic. Keep only protocol message types if not
  already moved to `internal/runtime/wire/`.
- **Delete** `internal/kernel/plugin/registry.go` /
  `supervisor.go` / `handle.go` portions that assumed kernel
  ownership. Migrate any retained semantic logic to
  `internal/runtime/`.
- **Delete** all callers of the deleted APIs; replace with
  `RuntimeConn.Invoke` calls through the Hub.
- Skill exec via `process_manager.go` is **untouched** in this
  slice. Deliberate — M3's job.

### Bootstrap script changes (`tabula/scripts/bootstrap.sh`)

- Sanity-check `tabula` and `tabula-runtime` binaries are
  installed.
- Read `tabula.project.toml` for `project.name` and
  `kernel.mode`.
- Tenant create-if-missing using `tabula tenant list --json`
  (M4 introduces real `tabula tenant create`; for M2 there is
  one implicit tenant — script only logs the name).
- Local mode: `tabula serve &`, then poll `tabula status --json`
  until `kernel.running == true && len(runtimes) > 0` or 10s
  timeout.
- Print "ready" with hint to launch CLI/TUI.

### Test impact

- All kernel unit tests that previously mocked `Runtime.Spawn`
  switch to `mock.RuntimeConn` from M1-05. Any test that
  required real subprocess spawn must move to integration tier
  (testbed in M2-08).
- No kernel test may invoke `os/exec` for plugins; enforced by
  CI grep guard (optional, recommend).

## Acceptance criteria

- [ ] `go build ./...` clean across the kernel.
- [ ] `go test -race ./...` green.
- [ ] `internal/kernel/plugin/runtime.go`'s spawn logic is
      physically deleted (file may remain only with shared
      types not yet relocated).
- [ ] `grep -r "exec.Command" internal/kernel/` returns only
      `process_manager.go` (skill exec, intentional intermediate
      state) and nothing related to plugins.
- [ ] Manual: `tabula serve` starts, runtime auto-attaches,
      `gateway-telegram` (or any migrated bundle plugin) handles
      a tool call end-to-end.
- [ ] `bootstrap.sh` runs cleanly on a fresh `$TABULA_HOME`,
      ends with "ready" in <15s.
- [ ] Kernel restart: runtime detects disconnect, exits cleanly
      (managed-child mode does not auto-restart on its own —
      kernel re-forks on its restart).
- [ ] Plugin worker crash: kernel sees `internal_error` from
      Invoke, surfaces to caller, runtime spawns fresh worker
      on next Invoke.
- [ ] CI green.

## Blocked by

- M2-01 (transport)
- M2-02 (binary)
- M2-03 (worker spawn)
- M2-04 (plugins migrated; without this they cannot answer
  Invoke from runtime)
- M2-05 (auth)
- M2-06 (`tabula status` for bootstrap.sh readiness check)

## Notes

- This is the riskiest change in the program. Recommend
  staging on a feature branch with a minimum 1-day soak before
  merging to main.
- Skills continue working through the legacy
  `process_manager.go` path — keep that code untouched. M3
  removes it.
- The deletion is irreversible: there is no `embedded` backend
  to fall back to (Q10).
- After this lands, prod behavior changes: every plugin tool
  call now goes through Runtime API → unix socket → runtime
  daemon → worker. Latency adds ~0.1ms (negligible).

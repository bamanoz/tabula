# M1-04 — PluginExecPolicy interface (runtime-side)

Status: done
Phase: M1
Type: AFK
Labels: needs-triage, area/runtime, phase/m1

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (§1.7, §2.4)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§3, §4)

## What to build

Define the runtime-side interfaces that govern how a worker is
spawned and how the runtime daemon talks to it. Per ADR §3,
`PluginExecPolicy` is the seam where `bare`/`cgroup`/`sandbox`
implementations plug in. Per ADR §4, plugin and skill execution
share the same `Worker` interface; only lifecycle policy differs.

This slice creates the placeholder package and defines the
contracts. No real spawning happens yet (M2 ships `bare`).

Interfaces:

- `PluginExecPolicy` (`internal/runtime/host/policy/policy.go`):
  - `Spawn(ctx, SpawnReq) (Worker, error)`

- `Worker` (`internal/runtime/host/policy/worker.go`):
  - `Init(ctx, WorkerInit) error` — sends WorkerInit, awaits ack.
  - `Call(ctx, WorkerCall) (WorkerResult, error)` — synchronous
    one call, error if Worker is cold and call_id reused.
  - `Shutdown(ctx) error` — cooperative shutdown
    (WorkerShutdown → wait → SIGTERM if unresponsive).
  - `Wait() (exitInfo, error)` — blocks until process exit.
  - `IsAlive() bool`.

- `SpawnReq` struct: kernel_id, tenant_id, target_id, manifest,
  env, working dir, mode (cold|warm).

- `Worker` lifecycle states documented (spawned → initialized →
  ready → calling → idle → shutting_down → exited).

Stub implementation `barePolicy` in
`internal/runtime/host/policy/bare/`:

- `Spawn` returns `errNotImplemented`.
- Provided so other slices can wire a `PluginExecPolicy` value
  end-to-end without nil checks.

The `cmd/tabula-runtime/` directory is created as a package skeleton; no `main.go` yet (binary lands in M2).

## Acceptance criteria

- [ ] `PluginExecPolicy` and `Worker` interfaces declared with
      full docstrings: lifecycle, blocking semantics,
      cold-vs-warm contract.
- [ ] Lifecycle state diagram in package doc comment.
- [ ] `SpawnReq` and supporting structs declared.
- [ ] `barePolicy` stub returns `errNotImplemented`.
- [ ] Compile-time assertion `var _ PluginExecPolicy = (*barePolicy)(nil)`.
- [ ] Test that `barePolicy.Spawn` returns the stub error.
- [ ] `go build ./...` clean.
- [ ] `go test ./internal/runtime/host/policy/...` green.

## Blocked by

- M1-02 (uses worker protocol types for `Worker` method signatures)

## Notes

- `PluginExecPolicy` lives entirely on the runtime side. Kernel
  never imports this package.
- Cold-vs-warm contract: a cold worker rejects a second `Call`
  with an explicit error; the runtime daemon must `Shutdown` and
  `Spawn` again. Warm workers accept many `Call`s sequentially.
- Sandbox implementations (`cgroup`, `sandbox`, `nested-docker`)
  are deferred past M5 per Q3.4. M1 only declares the seam.

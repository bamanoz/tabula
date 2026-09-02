# Remove kernel-owned process supervision

**Type:** AFK  
**Status:** proposed

## What to build

Remove `process.Supervisor`, `Hub.RegisterSpawn`, process snapshots, and crash broadcasts from kernel where they are not backed by a live production caller. Runtime remains the sole owner of worker process creation, shutdown, isolation, and lifecycle reporting.

Kernel consumes authenticated runtime lifecycle observations only when they affect authoritative driver coordination.

## Evidence

- `Hub.RegisterSpawn` has no production caller outside its own implementation.
- `process.Supervisor` is retained mainly by snapshot, shutdown, and tests.
- Runtime pool already owns worker lifecycle and reports driver lifecycle through typed Runtime API frames.

## Acceptance criteria

- [ ] Kernel no longer starts, tracks, signals, or snapshots arbitrary child processes.
- [ ] Kernel shutdown stops agent lifecycle services and runtime connections without a second process supervisor.
- [ ] Driver process lifecycle remains runtime-owned and runtime-attested.
- [ ] Any user-visible process diagnostics are produced by runtime or a bundle, not legacy kernel state.
- [ ] Dead source, tests, comments, and snapshot fields are removed in the same change.
- [ ] Installed runtime shutdown and worker-crash scenarios still pass.

## Blocked by

None - can start immediately.

# Bound per-key lifecycle serialization state

**Type:** AFK  
**Status:** proposed

## What to build

Replace indefinitely growing maps of per-session and per-runtime mutexes with lifecycle-bound serialization primitives whose entries are removed when no operation can still reference them.

Cover at least `ExecutionCoordinator.locks` and `Hub.runtimeLifecycleLock`. Prefer ownership by the durable aggregate/runtime entry itself where possible; otherwise use a ref-counted keyed lock with race-safe deletion.

## Evidence

- `internal/kernel/execution_coordinator.go` retains one mutex for every observed `agent.SessionKey`.
- `internal/kernel/runtime_attach.go` retains one mutex for every observed runtime ID.
- Neither map removes entries.

## Acceptance criteria

- [ ] Completed/deleted sessions and detached runtimes do not leave permanent lock entries.
- [ ] Entry deletion cannot allow concurrent operations for the same key to use different locks.
- [ ] Session cancellation, delete, driver takeover, runtime detach, and reattach remain serialized correctly.
- [ ] Stress tests exercise high-cardinality transient session and runtime IDs under `-race`.
- [ ] Tests assert bounded steady-state bookkeeping after operations complete.
- [ ] No generic cache or timer-based eviction obscures ownership correctness.

## Blocked by

None - can start immediately.

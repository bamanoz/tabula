# Runtime scheduler for execution groups

Type: Runtime / Feature

Priority: P1

Status: Planned

Repos: `tabula`

## Parent

`docs/issues/parallel-runtime/README.md`

## Blocked by

- `001-tool-execution-policy-metadata.md`
- `002-concurrent-warm-worker-transport.md`
- `003-python-plugin-sdk-concurrent-tool-calls.md`

## Problem

Once transport and SDK allow concurrent tool calls, the runtime still needs a
policy-aware scheduler. It must know when to start a call immediately, when to
queue it behind an active conflicting execution group, and when to release the
queue after completion.

## What to build

For each warm worker entry, maintain scheduler state:

- inflight count
- active execution groups
- queued calls waiting on conflicts
- hard safety caps for inflight calls

Dispatch rules:

- start immediately when the tool's conflict groups are inactive and caps allow
- otherwise queue the call
- on completion, release active groups and drain the queue

Defaults must preserve old serial behavior when no plugin opts in.

## Acceptance criteria

- [ ] Parallel tools in a non-conflicting execution group can run together.
- [ ] Serial tools block appropriately within their conflict set.
- [ ] One failing call releases its execution group state correctly.
- [ ] Old plugins without policy metadata still behave serially.
- [ ] Runtime logs or metrics make inflight scheduling visible enough to debug.

## Files

- Edit: `internal/runtime/host/pool/pool.go`
- Edit: warm worker entry / registry state in `internal/runtime/host/pool/...`
- Add/Edit: scheduler tests in `internal/runtime/host/pool/pool_test.go`

## Verify

```bash
go test ./internal/runtime/host/pool
```

## Risk

High.

This changes the runtime execution model for warm plugins. Bad scheduler state
can cause starvation, deadlocks, or accidental parallel writes.

## Notes

- Keep hooks out of this scheduler for first pass.
- Runtime may still enforce internal hard caps even when a plugin says
  `concurrency = "parallel"`.

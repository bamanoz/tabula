# Concurrent warm worker transport

Type: Runtime / Feature

Priority: P1

Status: Planned

Repos: `tabula`

## Parent

`docs/issues/parallel-runtime/README.md`

## Blocked by

- `001-tool-execution-policy-metadata.md`

## Problem

Warm worker transport is effectively single-flight. One call is sent, the
runtime waits for one result, then sends the next call. Even if a plugin later
opts into parallel execution, the worker transport cannot carry multiple
inflight calls safely.

## What to build

Make warm worker transport concurrency-safe:

- allow multiple concurrent `Call(...)` requests on one worker connection
- correlate replies strictly by `call_id`
- keep a single reader loop that demultiplexes results to waiting callers
- serialize worker stdin writes with a mutex so frames never interleave

This issue should not add scheduling policy yet. It only makes the transport
capable of carrying multiple inflight calls.

## Acceptance criteria

- [ ] Two concurrent `Call(...)` requests can be in flight on one warm worker.
- [ ] Out-of-order results are routed to the correct waiter.
- [ ] One call timing out or failing does not corrupt the other waiter.
- [ ] Worker shutdown cleans up all pending calls predictably.

## Files

- Edit: warm worker transport in `internal/runtime/worker/...`
- Edit: runtime host pool worker wrapper as needed
- Add/Edit: transport-focused tests in runtime worker / pool packages

## Verify

```bash
go test ./internal/runtime/worker/... ./internal/runtime/host/pool
```

## Risk

Medium.

This is the first place where broken multiplexing can corrupt unrelated calls,
so reply routing and shutdown behavior need strong tests.

## Notes

- Keep hooks out of scope here. This issue is only about tool call transport.

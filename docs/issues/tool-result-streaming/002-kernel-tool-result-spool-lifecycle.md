# Kernel tool result spool lifecycle

Type: Runtime / Feature

Priority: P1

Status: Completed

Repos: `tabula`

## Parent

`docs/issues/tool-result-streaming/README.md`

## Problem

Streaming transport alone is not enough. The kernel needs a durable temporary
place to assemble large results, plus explicit timeout and cleanup semantics so
broken or abandoned streams do not leave infinite pending calls or orphaned
spool files.

## What to build

Add a kernel-side spool manager for streamed tool results.

The kernel should:

- create a temp spool for a streamed result,
- append bounded deltas as they arrive,
- track bytes, preview text, and terminal status,
- fail and clean up if the stream stalls, disconnects, or violates protocol,
- surface an explicit undeliverable result error if no later rewrite makes the
  completed result small enough to send.

This slice should end with a stable internal source abstraction that later hooks
can read.

## Acceptance criteria

- [x] The kernel spools large streamed tool results without keeping the full
      payload in memory.
- [x] Each pending streamed result ends in exactly one terminal state and is
      cleaned up.
- [x] Idle timeout, overall lifetime timeout, cancel, and runtime disconnect all
      clear pending state and remove temp spools.
- [x] Startup or periodic cleanup removes stale orphaned spool files.
- [x] Without a rewrite hook, an oversized completed result returns an explicit
      delivery error instead of silent truncation.

## Files

- Edit: `internal/runtime/api.go`
- Edit: `internal/kernel/tool_service.go`
- Edit: `internal/kernel/tool_dispatch_test.go`
- Edit: `internal/kernel/message.go`
- Edit: `internal/kernel/broadcast.go`
- Add or edit: `internal/kernel/...` spool helper files as needed

## Verify

```bash
go test ./internal/kernel ./internal/runtime/... -count=1
```

## Risk

High.

The kernel must now manage streamed pending state, file cleanup, and timeout
behavior correctly under cancellation and disconnect races.

## Notes

- Make timeout and cleanup behavior explicit in code comments and tests.
- Late deltas after terminal cleanup must not resurrect state.
- Prefer temp spool refs over handing raw filesystem paths directly to future
  hooks.

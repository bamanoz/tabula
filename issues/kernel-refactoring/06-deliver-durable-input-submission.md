# Deliver durable idempotent input submission end to end

**Type:** AFK  
**Status:** completed

Implemented by `internal/agent/input_processor.go`; transport-specific gateway migration remains issue 14.

## What to build

Add the protocol v4 client command that durably accepts an input and creates one queued turn. Route it through the domain state machine and `SessionRepository`; return acceptance only after commit.

Deliver a minimal client SDK/test client path that submits an input and reads the authoritative session projection. Do not dispatch to a driver yet.

## Acceptance criteria

- [x] `input.submit` requires a client-generated `input_id` and command ID.
- [x] Acceptance is sent only after durable repository commit.
- [x] Repeating the same ID and payload returns the existing accepted result, including across a different command ID.
- [x] Reusing an ID with different payload returns typed `ErrInputConflict`.
- [x] Multiple gateways can submit concurrently without lost updates or duplicate turns.
- [x] Per-session FIFO ordering follows committed aggregate order and stable monotonic turn positions.
- [x] Gateway disconnect before or after acceptance does not alter the durable turn; retry/query recovers the result.
- [x] Snapshot/query exposes queued turns and stable IDs through `InputProcessor.Get`.
- [x] Focused race tests and SQLite reopen persistence tests pass.

## Blocked by

Issues 03, 04, and 05.

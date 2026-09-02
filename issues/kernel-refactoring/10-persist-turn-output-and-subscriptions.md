# Persist sequenced turn output and resumable subscriptions

**Type:** AFK  
**Status:** completed

## What to build

Make turn output an ordered, durable event stream tied to a turn/attempt. Kernel commits output before notifying observers and exposes cursor-based session subscriptions so gateways can reconnect without reconstructing state from live WebSocket timing.

Large artifacts remain outside kernel; this issue persists only bounded protocol output and references/metadata allowed by the approved model.

## Acceptance criteria

- [x] Output events contain turn, attempt, driver generation, and monotonic sequence.
- [x] Duplicate or stale output cannot reorder or mutate the aggregate.
- [x] Commit-before-publish ordering is tested.
- [x] A reconnecting client can resume from a cursor without missed or duplicated committed events.
- [x] Snapshot plus cursor replay produces the same visible session projection.
- [x] Stream, reasoning, usage, provider retry/error, compaction, and tool-result events have a documented v4 mapping.
- [x] Backpressure and retention limits are explicit; kernel does not become an unbounded artifact store.
- [x] Focused race tests and an installed reconnect test pass.

## Blocked by

Issues 04 and 09.

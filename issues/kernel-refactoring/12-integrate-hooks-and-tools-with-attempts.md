# Integrate hooks and tools with durable attempts

**Type:** AFK  
**Status:** completed

Implemented by the kernel attempt/fence validator, attempt-correlated hook audit and tool lifecycle records, identity-bound exchange continuations, and cancellation-aware fenced runtime result streaming.

## What to build

Map existing before/after turn hooks, tool calls, approvals, exchanges, and tool-result streaming onto the durable attempt lifecycle. Preserve hook and tool ownership outside the kernel while making their correlation and failure transitions authoritative.

Do not redesign product-specific hook policy here. Make unavailable, timeout, denied, cancelled, and completed outcomes explicit in the attempt model.

## Acceptance criteria

- [x] `before_message`, `before_turn`, and `after_turn` have deterministic attempt correlations and terminal behavior.
- [x] Hook disconnect, timeout, deny, modify, and fail-open/fail-closed policy are explicit.
- [x] Tool calls cannot outlive or mutate a fenced/stale attempt.
- [x] Tool result streaming is ordered and cancellation-aware.
- [x] Exchange choose/approve flows remain identity-bound or are replaced by an approved typed interaction contract.
- [x] Provider retry/error and compaction events map to attempt state without kernel provider semantics.
- [x] Existing security policy remains enforced at the actor and operation boundary.
- [x] Focused hook/tool tests and installed failure/restart tests pass.

## Blocked by

Issues 09 and 11.

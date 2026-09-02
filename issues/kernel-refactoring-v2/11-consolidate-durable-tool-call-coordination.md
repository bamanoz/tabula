# Consolidate durable tool-call coordination

**Type:** AFK  
**Status:** proposed

## What to build

Consolidate protocol-v4 tool-call ownership, attempt fencing, suspension, pending delivery, and terminal result correlation behind one coordinator with thin client/runtime adapters.

Remove parallel state paths where the same logical tool call may appear in `activeCalls`, `pendingCalls`, `v4PendingCalls`, and `runtimePendingCalls`. The coordinator must expose explicit states and one terminal cleanup path.

## Evidence

- `internal/kernel/tool_service.go` owns five parallel bookkeeping maps.
- `client_v4_tool.go`, `tool_service.go`, `tool_attempt.go`, `tool_lifecycle.go`, and `tool_result.go` split one attempt-scoped lifecycle across modules.

## Acceptance criteria

- [ ] One keyed record represents each active or suspended tool call.
- [ ] Client-v4, driver execution, runtime invocation, hooks, approvals, and result delivery use one state machine.
- [ ] Fence validation occurs before dispatch, after modifying hooks, and before terminal delivery.
- [ ] Duplicate, late, stale-generation, cancellation, disconnect, approval, and runtime-crash outcomes have explicit behavior.
- [ ] Every terminal path removes coordinator state and records one bounded lifecycle result.
- [ ] Existing tool contracts and external bundle policy remain unchanged.
- [ ] Focused race tests and installed attempt-scoped tool failure tests pass.

## Blocked by

Issue 04.

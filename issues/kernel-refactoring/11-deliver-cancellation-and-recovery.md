# Deliver cancellation, interruption, and explicit recovery

**Type:** AFK  
**Status:** completed

## What to build

Implement authoritative cancellation and recovery commands for queued, prepared, permitted, running, and uncertain attempts.

Expose explicit `resume`, `retry`, `discard`, and `cancel` outcomes for `recovery_required`. Gateway observes these states; it does not infer them from missing events or retry joins.

## Acceptance criteria

- [x] Cancel is a durable intent with a correlated outcome.
- [x] Driver receives cancellation for the current generation and attempt.
- [x] Cancel/completion races commit exactly one terminal state.
- [x] Queued and pre-permit attempts can be safely cancelled or retried.
- [x] Permitted attempts cannot be silently retried after uncertain external effects.
- [x] Recovery commands are idempotent and authorization-checked.
- [x] Kernel restart preserves cancellation and recovery state.
- [x] UI-visible terminal status always replaces optimistic pending state.
- [x] Focused race tests cover disconnect, cancellation, completion, and restart boundaries.

## Blocked by

Issues 09 and 10.

# Centralize authoritative transition commits

**Type:** AFK  
**Status:** proposed

## What to build

Introduce one deep internal agent transition executor for the repeated `Load -> authorize/validate -> Decide -> ApplyAll -> Commit -> retry ErrVersionConflict` protocol.

Callers provide the command, optional state authorization, and optional outbox construction. The executor owns digest calculation, projection replay, bounded CAS retries, duplicate handling, and consistent error context.

## Evidence

The same commit loop is repeated across `execution_service.go`, `output_service.go`, `recovery_service.go`, `lease_service.go`, and `input_processor.go`; `execution_service.go` alone contains four copies.

## Acceptance criteria

- [ ] One internal module owns transition digesting, replay, commit, conflict retry, and duplicate semantics.
- [ ] Input, lease, execution, output, and recovery services use that module.
- [ ] Authorization checks run against the exact state version being committed.
- [ ] Outbox payloads can be derived from the accepted projection without creating a second transition path.
- [ ] Error identities and original protocol acceptance boundaries remain unchanged.
- [ ] Existing concurrency, idempotency, cancellation-race, and fencing tests pass with `-race -count=1`.
- [ ] The abstraction reduces repeated code without hiding command-specific invariants.

## Blocked by

Issue 03.

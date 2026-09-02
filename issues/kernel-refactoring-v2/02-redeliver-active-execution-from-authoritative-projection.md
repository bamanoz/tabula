# Redeliver active execution from authoritative projection

**Type:** AFK  
**Status:** proposed

## What to build

Make assignment and execution-permit redelivery reconstruct their payloads from the current durable session projection instead of scanning retained outbox history from cursor zero.

The outbox remains the atomic delivery record, but active execution recovery must not require historical payload retention. The projection must contain every field needed to reproduce the stable assignment or permit for the current fenced attempt.

## Evidence

- `internal/kernel/execution_coordinator.go`: `assignmentFromOutbox`, `permitFromOutbox`, and `readOutboxPayload` scan from cursor zero.
- `internal/agent/sqlite_repository.go` and `memory_repository.go`: cursors older than the retention floor return `CursorExpiredError`.
- ADR 0026 bounds event/outbox retention to the newest 4096 combined cursor positions.
- Protocol v4 promises committed assignment/permit redelivery after delivery failure.

## Acceptance criteria

- [ ] Assigned and prepared attempts can reconstruct the identical stable assignment from projection state.
- [ ] Permitted attempts can reconstruct the identical stable permit from projection state.
- [ ] Recovery does not call `ReadOutbox` to rediscover active execution payloads.
- [ ] Existing outbox records remain transactional delivery notifications with stable IDs.
- [ ] Memory and SQLite tests force retention expiry while an attempt remains assigned, prepared, and permitted, then prove redelivery succeeds.
- [ ] Kernel restart after retention expiry exercises the real runtime delivery path.
- [ ] Protocol and ADR documentation state which projection fields make redelivery durable.

## Blocked by

None - can start immediately.

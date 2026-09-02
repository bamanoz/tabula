# Add the default SQLite SessionRepository adapter

**Type:** AFK  
**Status:** completed

Implemented in `internal/agent/sqlite_repository.go` with pure-Go SQLite, WAL, full synchronous durability, transactional schema migration, corruption diagnostics, and crash/reopen coverage.

## What to build

Implement the first production `SessionRepository` adapter using tenant-local SQLite with WAL mode. SQLite is a default deployment choice behind the repository interface, not part of the domain contract.

Define schema ownership, migrations, durability settings, connection lifecycle, corruption diagnostics, backup behavior, and safe concurrent access from kernel command processors.

## Acceptance criteria

- [x] Adapter passes the complete repository conformance suite unchanged.
- [x] Events, projection, command/input deduplication, and outbox commit atomically.
- [x] Compare-and-swap conflicts are reliable under concurrent writers.
- [x] Reopen after process crash reconstructs identical aggregate state and event cursors.
- [x] Schema migrations are transactional and versioned.
- [x] Tenant isolation is enforced by repository keys and uses no hardcoded `~/.tabula` paths.
- [x] Startup reports actionable corruption or migration failures instead of silently resetting state.
- [x] Existing observational session files are explicitly retired from authoritative v4 state; no migration is performed.
- [x] Focused tests pass with `-race -count=1`, including crash/reopen subprocess tests (40 tests).

## Blocked by

Issue 04.

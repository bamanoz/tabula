# Define SessionRepository and its conformance suite

**Type:** AFK  
**Status:** completed

## What to build

Define the backend-neutral persistence interface for authoritative session aggregates. The interface must expose domain commit semantics, not SQL, files, or generic CRUD.

Provide an in-memory reference implementation and a reusable conformance suite for every future adapter.

## Required semantics

A commit must atomically provide:

- expected-version compare-and-swap;
- command and input idempotency;
- ordered domain event append;
- current aggregate projection update;
- transactional outbox append;
- new aggregate version and stable event cursor.

The repository persists decisions produced by the domain model. It must not reinterpret turn or driver policy.

## Acceptance criteria

- [x] Kernel-facing interface contains no SQLite-, Postgres-, filesystem-, or object-store-specific types.
- [x] Load, commit, event/outbox reading, listing, and error contracts are documented.
- [x] Conflicts, duplicate commands, missing sessions, deleted sessions, and corrupt state have typed errors.
- [x] In-memory adapter passes the reusable conformance suite.
- [x] Conformance tests cover atomic events plus outbox, CAS conflicts, deduplication, event ordering, replay, defensive copies, and concurrent commits.
- [x] The interface can support SQLite and Postgres without weakening guarantees.
- [x] Artifacts remain outside this interface and outside kernel ownership.
- [x] Focused tests pass with `-race -count=1` (24 tests across aggregate and repository packages).

## Blocked by

Issues 01 and 03.

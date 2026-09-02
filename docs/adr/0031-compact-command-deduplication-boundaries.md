# ADR 0031 - Compact command deduplication boundaries

Date: 2026-08-09
Status: Accepted
Supersedes: nothing
Superseded by: nothing

## Context

A repository commit previously serialized its complete `CommitResult` into each
SQLite `commands.result` row. That result included the full current session
projection and committed outbox entries. Session projections grow with inputs,
turns, attempts, and output metadata, so every later command copied an
increasing aggregate into another permanent row. Database growth was therefore
near-quadratic in session history size even though command deduplication needs
only the boundary committed by that command.

Command retry still needs two distinct views. Services need the current durable
projection so they do not resume from stale aggregate state, while protocol
acceptance must report the version and cursor originally returned for the
retried command.

## Decision

Persist only this command boundary in command-deduplication state:

```json
{"version": 42, "cursor": 57}
```

Memory and SQLite repositories use the same semantics. A duplicate command with
the same digest:

- bypasses expected-version comparison;
- loads and returns the current session projection;
- returns the original command version and cursor separately;
- returns no historical outbox entries;
- sets `Duplicate`.

A command ID reused with another digest remains a conflict. Commands have one
stored representation: compact boundary JSON. Any other shape is corrupt state
and fails closed.

## Consequences

Positive:

- command metadata grows linearly with command count instead of repeatedly
  snapshotting the growing aggregate;
- duplicate retries preserve their original protocol acceptance boundary;
- services receive current aggregate state after concurrent later commits;
- duplicate lookup cannot replay historical outbox messages.

Negative:

- callers must distinguish current `Record` state from `CommandVersion` and
  `CommandCursor`;
- databases containing another command-result shape are rejected rather than
  translated.

## Verification

Implementation must prove:

- memory and SQLite conformance after a duplicate retry that follows later
  commits;
- duplicate result contains current projection, original command boundary, and
  empty outbox;
- SQLite rejects any non-boundary command-result shape;
- newly written `commands.result` rows remain bounded independently of session
  projection size;
- live repeated turns do not reproduce projection-sized command rows.

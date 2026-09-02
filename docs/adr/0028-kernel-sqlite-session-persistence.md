# ADR 0028 - Kernel SQLite is the sole session persistence authority

Date: 2026-08-08
Status: Accepted
Supersedes: ADR 0008 completed-turn evidence source; ADR 0014 subagent ledger persistence statements
Superseded by: nothing

## Context

Protocol v4 makes session state transitions durable and serializable in the
kernel repository. It also provides stable committed events and cursors for
replay. Older bundle behavior separately wrote and read session
`history.jsonl` and `ledger.jsonl`, which created a second persistence authority
with different ordering, retention, and recovery semantics.

The kernel still needs a generic place for bounded opaque evidence that is
associated with a session but must not mutate the aggregate. Examples include
hook dispatch audit, tool lifecycle diagnostics, edit diffs, and subagent
lifecycle metadata.

## Decision

The kernel SQLite repository at `$TABULA_HOME/state/kernel/sessions.db` is the
sole persistence authority for session aggregates, turns, attempts, committed
events, outbox state, and auxiliary session records.

Protocol-v4 clients use:

- `session.get` for an aggregate projection and cursor;
- `session.subscribe` for ordered committed events after a cursor;
- `session.record.append` and `session.record.list` for bounded opaque auxiliary
  records.

Committed events are the only source for transcript and aggregate replay. A
consumer that receives `cursor_expired` fetches `session.get` and resumes from
the snapshot cursor.

Auxiliary records have producer-owned kinds and JSON payloads. Appending a
record does not change the aggregate version or committed-event cursor. Records
cannot drive aggregate state, replace committed transcript events, or store
unbounded artifact content.

Plugin caches and domain state remain under tenant plugin state. Artifact bytes
remain under their component owner. The kernel does not shape producer payloads
or acquire plugin semantics.

Remove session JSONL writers/readers and the `tabula_session_sdk` /
`tabula_ledger` persistence surfaces without compatibility aliases or shims.

## Consequences

Positive:

- every gateway and lifecycle consumer observes one ordered durable session
  history;
- restart, replay, and cursor recovery use one repository contract;
- technical evidence remains durable without mutating session state;
- kernel remains a generic persistence and routing boundary.

Negative:

- consumers must speak protocol v4 instead of reading local files;
- retained-event expiry requires snapshot-and-resubscribe handling;
- auxiliary records require explicit bounded queries and producer-owned schemas.

## Verification

Implementation must prove:

- session records persist in SQLite and do not advance aggregate version/cursor;
- gateways replay only committed events and recover retained cursors;
- lifecycle consumers reconstruct turns from committed events;
- edit, hook, tool, and subagent diagnostics use auxiliary records where needed;
- no active source, test, mutable doc, or installed guide reads session
  `history.jsonl` or `ledger.jsonl`.

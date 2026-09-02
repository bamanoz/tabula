# ADR 0026 - Bounded durable output retention

Date: 2026-08-07
Status: Accepted
Supersedes: nothing
Superseded by: nothing

## Context

ADR 0025 made committed attempt output and cursor replay part of the authoritative session model. The initial implementation capped each output payload but retained every payload in `Attempt.Outputs` and every event/outbox record for the lifetime of the session. Long-running sessions therefore had unbounded projection and replay storage, and the documented `cursor_expired` recovery path could not occur.

Artifacts remain outside kernel ownership. The kernel still needs enough bounded output history for snapshots, retries, diagnostics, and reconnect without becoming an artifact store.

## Decision

Apply fixed generic retention limits at the durable repository boundary and in the aggregate projection:

- each output payload remains capped at 64 KiB;
- each attempt projection retains the newest 256 outputs or 1 MiB of payload, whichever limit is reached first;
- each session projection retains the newest 1024 outputs or 4 MiB of payload across attempts;
- repositories retain event and outbox records whose cursors are within the latest 4096 combined cursor positions for the session.

Projection eviction removes the oldest output payloads first. Per-attempt ordering remains authoritative because the next expected producer sequence is derived from the newest retained output, not from retained slice length. Command deduplication and output conflict semantics are unchanged.

The repository advances its retained cursor boundary atomically with commit. `ReadEvents` and `ReadOutbox` accept a cursor at that boundary and return a typed `cursor expired` error for an older cursor. Protocol v4 maps that error to `cursor_expired` with `snapshot_required: true`. The client fetches `session.get` and resumes `session.subscribe` from the snapshot cursor.

Large provider and tool artifacts remain external. Projection and replay retention apply only to bounded protocol payloads and references.

## Consequences

Positive:

- session projections and replay logs have explicit output-related bounds;
- memory and SQLite adapters expose the same cursor-expiry contract;
- long-running attempts continue sequence validation after older payload eviction;
- reconnect has a tested snapshot fallback instead of relying on infinite history.

Negative:

- old output payloads are unavailable from the session projection after eviction;
- clients that remain disconnected beyond the retained cursor window must refresh their snapshot;
- operators needing longer artifact history must use the owning artifact service rather than kernel output storage.

## Verification

Implementation must prove:

- per-attempt count and byte limits;
- per-session count and byte limits with oldest-first eviction;
- output sequence continuity after projection eviction;
- identical memory and SQLite cursor-expiry behavior;
- protocol v4 `cursor_expired` responses require a snapshot;
- snapshot cursor subscription succeeds after expiration.

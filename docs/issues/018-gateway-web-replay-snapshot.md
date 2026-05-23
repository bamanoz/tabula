# Gateway Web Replay Snapshot

Priority: Medium

Repos: `tabula-bundles`

## Problem

Gateway-web sends raw replay events and the frontend reconstructs user-visible
state from them. This spreads replay invariants across daemon and UI code and
makes it hard to distinguish empty sessions, busy sessions, pending approvals,
and replay failures.

## Evidence

- `transcript_replay` merges durable history, transient events, and pending
  exchange requests into a raw event list.
- `applyReplay` in `main.tsx` reconstructs messages, tool cards, usage, stream
  state, and pending approvals with local heuristics.
- The generic `Ready` fallback appears when replay data is missing or rejected.

## Impact

The replay interface is shallow: callers must know event ordering and recovery
rules. It is easy to create mismatches between live events and reconstructed
state.

## Proposed Fix

- Introduce a structured replay payload alongside raw events.
- Include explicit `messages`, `tools`, `pending_exchanges`, `usage`, `busy`,
  and `empty` fields.
- Keep raw events initially for compatibility, but move UI rendering toward the
  structured snapshot.
- Document which fields are authoritative.

## Acceptance Criteria

- Replay distinguishes successful empty session from failed/missing replay.
- Pending `ask_user` requests are restored without scanning raw event lists.
- Context usage and busy state are restored from explicit fields.
- Tests cover snapshot generation from durable history plus transient pending
  events.

# Gateway Web Frontend Session Store

Priority: High

Repos: `tabula-bundles`

## Problem

The frontend keeps timeline, pending approvals, stream state, context usage, and
responding state as one global active-session state in `main.tsx`. This makes
switching destructive and prevents instant switching between previously loaded
sessions.

## Evidence

- `chooseSession` clears active state before replay arrives.
- `applyReplay` reconstructs the entire visible timeline into one global
  `items` array.
- Pending approvals and stream ids are not naturally scoped by session.

## Impact

Switching feels slow, and events from one session can interfere with another.
The main frontend module is shallow: maintainers must understand many unrelated
state invariants to change switching behavior.

## Proposed Fix

- Extract a deep `SessionStore` module in the gateway-web frontend.
- Store session snapshots by `{tenant_id, session}`.
- Move replay and live-event application into the store.
- Let the UI select a cached snapshot immediately, then refresh it from replay.

## Acceptance Criteria

- Previously visited sessions render immediately on click.
- Each session has isolated timeline, usage, pending approvals, stream state, and
  switch status.
- `main.tsx` no longer directly owns per-session replay reconstruction details.
- Unit tests cover `SessionStore.applyReplay`, `applyEvent`, and switching
  between cached sessions.

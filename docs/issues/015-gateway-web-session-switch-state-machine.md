# Gateway Web Session Switch State Machine

Priority: High

Repos: `tabula-bundles`

## Problem

The frontend currently treats session switching as a series of side effects:
clear some state, call `resume`, wait for replay, and hope the active timeline is
rebuilt. When replay is slow or fails, the UI can display the generic empty
`Ready` fallback even though a session is still loading or has failed.

## Evidence

- Users observed `Ready / Session is ready` after switching, while the target
  session had messages.
- Bootstrap and replay errors can leave the frontend with no `recent_sessions`
  and no visible error state.
- `main.tsx` stores active timeline state globally and clears it immediately on
  switch.

## Impact

The UI lies about state. A loading or failed switch looks like an empty session,
which makes it difficult to distinguish real empty history from gateway failure.

## Proposed Fix

- Introduce an explicit frontend switch state machine: `booting`, `idle`,
  `switching`, `refreshing`, and `error`.
- Display `Loading session <name>...` when no cached snapshot exists yet.
- Display cached content with a subtle refresh indicator when revisiting a
  previously loaded session.
- Display retry/error UI when bootstrap, join, or replay fails.
- Reserve the empty `Ready` fallback only for a successful replay of a truly
  empty session.

## Acceptance Criteria

- Slow replay never clears the UI to the generic `Ready` state.
- Bootstrap or replay failures show an actionable error and retry path.
- Switching to an already loaded session shows cached content immediately.
- Tests cover empty session, loading session, replay error, and cached switch
  states.

# Gateway Web Switch Id Protocol

Priority: High

Repos: `tabula-bundles`

## Problem

Session switch responses and replay events are not correlated with a particular
switch request. Fast switches can allow late replay or joined events from an
older selection to mutate the currently selected session UI.

## Evidence

- The frontend applies `session.replay` globally.
- Many kernel-originated events do not carry enough session identity for the UI
  to safely reject stale updates.
- Rapid session switching has been observed to leave the chat visually attached
  to a previous session.

## Impact

Late events can overwrite the timeline, usage, pending approvals, or status for
the wrong active session.

## Proposed Fix

- Include a monotonic `switch_id` in browser `join` actions.
- Echo `switch_id` on `session.replay`, `session.joined`, and switch error
  events.
- Store the active switch id in the frontend and discard stale switch responses.
- Log stale switch responses with tenant, session, client id, and switch id.

## Acceptance Criteria

- Fast A -> B -> A switching cannot apply B's delayed replay to A.
- Stale `session.replay` and `session.joined` events are ignored and logged.
- Tests simulate out-of-order switch responses.
- The protocol documentation mentions `switch_id` semantics.

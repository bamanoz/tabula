# Gateway Web Multi Tab Ownership

Priority: Medium

Repos: `tabula-bundles`

## Problem

Gateway-web has a partial multi-tab policy. A newer browser tab can supersede an
older tab for the same `{tenant, session}`, but the user-facing ownership model
is not complete.

## Evidence

- Two tabs on the same session previously caused a WebSocket reconnect loop.
- A `session.superseded` event was added to stop reconnecting, but switching from
  a superseded tab to another session still needs a clear ownership story.
- Kernel `/sessions` can show different tabs owning different active sessions.

## Impact

Users can be unsure which tab is writable. Superseded tabs may look broken rather
than intentionally inactive.

## Proposed Fix

- Define the first supported policy: one writable tab per `{tenant, session}`.
- Display a clear superseded/read-only banner in displaced tabs.
- Allow a superseded tab to switch to another session and acquire ownership
  there.
- Avoid reconnect loops and avoid silently dropping user input.

## Acceptance Criteria

- Two tabs on the same session do not reconnect-loop.
- Superseded tabs do not auto-reconnect to fight the owner.
- Superseded tabs can switch to another session and become writable.
- Tests cover same-session tabs and switch-away-from-superseded behavior.

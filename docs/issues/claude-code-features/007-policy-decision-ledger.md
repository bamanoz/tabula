# Policy Decision Ledger

Type: AFK

Priority: P2

Repos: `tabula`, `tabula-bundles`, optionally `tabula-distrib`

## Parent

`docs/competitors/claude-code.md`

## What to build

Make tool permission outcomes explicit and replayable. Every tool call should
produce a policy decision record that states whether the call was allowed,
denied, or asked, which policy target made the decision, and whether the session
was interactive or headless.

This issue does not need to redesign permissions. It should make the existing
hook-permissions and hook-approvals path observable and deterministic enough to
debug.

## Acceptance criteria

- [ ] Tool dispatch records a policy decision for allowed, denied, and ask flows.
- [ ] Missing or unavailable policy hooks are represented explicitly rather than
      disappearing from logs.
- [ ] Headless/background behavior is documented and covered by tests.
- [ ] Decision records avoid logging secrets or full command payloads when unsafe.
- [ ] A diagnostic command or tool can show recent policy decisions for a session.

## Blocked by

- `001-session-ledger-harness-events.md`

# Kernel Client Auth And Hook Reply Identity

Priority: Critical

Status: Implemented

Repos: `tabula`, `tabula-bundles`, `tabula-distrib`

## Problem

Kernel `/ws` accepts protocol clients without authentication. A client can
self-declare `sends`, `receives`, `receives_global`, and hook subscriptions.
Also, `hook_reply` bypasses normal send/session policy and is matched only by
hook id, not by the subscriber that received the hook.

## Evidence

- `internal/tabula/app.go`: `/ws` upgrades and calls `kernel.NewClient`.
- `internal/kernel/connect.go`: connect plan accepts client-declared
  capabilities.
- `internal/kernel/policy.go`: `CanConnect` rejects old non-empty spawn tokens
  but otherwise allows connection.
- `internal/kernel/message_router.go`: `hook_reply` is handled before
  `policy.CanSend`.
- `internal/kernel/hook_engine.go`: pending hooks are `id -> chan`, with no
  responder identity.

## Impact

Any process that can reach the kernel listener can join sessions, send messages
or tool calls if it declares those capabilities, subscribe to hooks, and answer
pending hooks if it learns the hook id. This undermines tool approval and hook
security semantics.

## Proposed Fix

- Add an explicit kernel client trust boundary.
- Either require authenticated clients for `/ws`, or make unauthenticated `/ws`
  strictly loopback-only and document that guarantee.
- Track connection identity/capability source instead of trusting arbitrary
  self-declared capabilities from untrusted clients.
- Change pending hooks to store responder identity, event, session, and tenant.
- Accept `hook_reply` only from the exact subscriber that received the hook.
- Route `hook_reply` through a dedicated policy check rather than bypassing all
  checks.

## Acceptance Criteria

- A remote/non-authorized client cannot complete `connect` on `/ws`.
- A client that did not receive a hook cannot satisfy its `hook_reply`.
- Existing trusted gateway/plugin flows still work.
- Tests cover unauthorized connect, wrong-client `hook_reply`, and valid
  subscriber `hook_reply`.
- Security docs describe the kernel client auth model.

## Implementation Notes

- Kernel clients now authenticate with `auth_token` in `connect`.
- `tabula serve` writes `$TABULA_HOME/run/kernel-client-token` and exports
  `TABULA_KERNEL_TOKEN` to child processes.
- `hook_reply` is accepted only from the WebSocket client or runtime hook
  subscriber that received the hook.

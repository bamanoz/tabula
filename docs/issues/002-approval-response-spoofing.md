# Approval Response Spoofing

Priority: Critical

Repos: `tabula-bundles`, optionally `tabula`

## Problem

`hook-approvals` broadcasts approval prompts through `status` messages and then
accepts any global `status` message containing a matching `ask_response.id`.
The response is not bound to a specific gateway, browser client, or user
channel.

## Evidence

- `base/hook-approvals/run.py`: `request_approval` creates `request_id`, sends
  `ask_request`, and accepts any matching `ask_response` from the kernel stream.
- `base/hook-approvals/run.py`: approval decisions can become persistent
  `allow_always` or `deny_always` rules.
- Kernel clients can self-declare `receives_global` and `sends` today.

## Impact

A connected client that sees or guesses an approval id can approve or deny tool
calls on behalf of the user. This is especially dangerous for `exec_run`,
`exec_run_background`, MCP server management, and future privileged tools.

## Proposed Fix

- Bind each approval request to the intended UI/gateway/client identity.
- Accept a response only from that same responder identity.
- Use a larger nonce and keep it private to the intended responder when
  possible.
- Avoid using global `status` as the authorization channel unless kernel-level
  identity is enforced.
- Consider a first-class approval protocol primitive rather than ad-hoc status
  payloads.

## Acceptance Criteria

- A second client connected to the same kernel cannot answer another client's
  approval request.
- `allow always` and `deny always` can only be saved after an authenticated,
  intended responder decision.
- Tests cover spoof attempt, valid approval, timeout/disconnect, and persistent
  rule save.

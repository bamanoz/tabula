# ADR 0003 — Kernel WebSocket protocol v3 envelopes and topics

Date: 2026-05-21
Status: Accepted
Supersedes: previous v2 kernel WebSocket message shape
Superseded by: nothing

## Context

The kernel WebSocket protocol originally grew by adding one top-level `type` per
domain behavior. User chat, streaming, tool calls, tool results, usage updates,
approvals, cancellations, hooks, and gateway-specific flows all used different
ad-hoc message shapes.

That made three things hard:

- adding interactive request/reply flows without creating new spoofable pairs;
- distinguishing transport routing from domain semantics;
- validating first-party clients because tests could keep using legacy aliases.

The most important security bug was interactive approval spoofing. Approval
requests were effectively carried as `status` payloads and responses were matched
by ids in opaque metadata. The kernel could not reliably enforce that only the
chosen responder replied.

## Decision

Adopt kernel WebSocket protocol `v: 3` as the only supported kernel-client wire
protocol.

### 1. Handshake

The first client frame is `hello`:

```json
{
  "v": 3,
  "type": "hello",
  "data": {
    "name": "gateway-cli-main",
    "auth_token": "ktk_...",
    "send_topics": ["message.user"],
    "receive_topics": ["session.init", "turn.done"]
  }
}
```

The kernel replies with `hello_ack`. Clients must send `v: 3` on every incoming
frame. The kernel rejects missing or mismatched versions. There is no v2 fallback.

### 2. Transport envelopes

Domain operations use a small transport enum plus exact topics:

- `event` for one-way events.
- `request` for correlated requests.
- `reply` for correlated responses.
- `hook` for kernel-dispatched hook calls.
- `hook_reply` for hook responses.

Examples:

- `event topic=message.user`
- `event topic=stream.delta`
- `event topic=usage.update`
- `event topic=turn.done`
- `event topic=turn.cancel`
- `request topic=tool.call`
- `reply topic=tool.result`
- `request/reply topic=exchange.choose`
- `request/reply topic=exchange.approve`

Capability declarations use exact topic names in `send_topics`,
`receive_topics`, and `global_topics`. Wildcards such as `stream.*` are not part
of the accepted contract.

### 3. Trusted metadata

The kernel overwrites `meta.kernel` on routed session/global deliveries. Client
metadata may be preserved in other namespaces, but incoming `meta.kernel` is not
trusted.

### 4. Identity-bound replies

`hook_reply` is accepted only from the client or runtime hook subscriber that
received the matching `hook` frame.

`exchange.*` replies are accepted only from the responder selected by the kernel
for that exchange id. This is the canonical approval and ask-user transport.

### 5. Browser gateways

Gateway-local browser WebSocket APIs should also avoid old kernel-wire names when
they surface protocol concepts. `gateway-web` uses topic-like browser events and
actions such as `message.user`, `tool.call`, `usage.update`, `turn.done`, and
`turn.cancel`. This browser API is still gateway-local; the kernel contract is
the protocol described above.

## Consequences

- First-party Python and TypeScript SDKs must emit v3 frames.
- Testbed clients and kernel tests must use real v3 envelopes/topics, not legacy
  aliases such as `message`, `tool_use`, `tool_result`, `status`, `done`, or
  `cancel`.
- Installed suites must exercise the installed client/plugin paths, especially
  `ask-user` and `hook-approvals`, because those flows depend on exchange
  responder identity.
- Historical issue documents may keep describing the old behavior as problem
  statements, but current reference docs must describe v3 only.
- Any future breaking kernel WebSocket change requires a new protocol version and
  corresponding SDK/testbed update.

## References

- `docs/PROTOCOL.md`
- `docs/KERNEL_PROTOCOL_EXAMPLES.md`
- `docs/KERNEL_PROTOCOL_VNEXT.md`
- `internal/kernel/protocol.go`
- `internal/kernel/exchange.go`

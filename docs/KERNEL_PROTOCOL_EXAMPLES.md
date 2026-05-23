# Kernel Protocol And Examples

This document describes the current Tabula kernel WebSocket protocol used by
gateways, drivers, observers, testbed clients, and helper clients.

It does not describe the Runtime API / worker protocol between `tabula-runtime`
and plugin workers.

Current kernel WebSocket protocol version: `3`.

Primary source files:

- `internal/kernel/protocol.go`
- `internal/kernel/message.go`
- `internal/kernel/connect.go`
- `internal/kernel/join_flow.go`
- `internal/kernel/message_router.go`
- `internal/kernel/exchange.go`

## Transport

Kernel clients connect to `TABULA_URL`, usually:

```text
ws://localhost:8089/ws
```

The first frame after WebSocket upgrade must be `hello`.

## Envelope

All messages are JSON objects.

Common fields:

| Field | JSON | Meaning |
|---|---|---|
| `V` | `v` | Required protocol version. Must be `3` on every client->kernel frame. |
| `Type` | `type` | One of `hello`, `hello_ack`, `join`, `joined`, `event`, `request`, `reply`, `hook`, `hook_reply`, `error`. |
| `Topic` | `topic` | Required for `event`, `request`, and `reply`. |
| `ID` | `id` | Correlation id for `request`, `reply`, and `hook_reply`. |
| `Session` | `session` | Optional explicit target session. Defaults to sender session. |
| `TenantID` | `tenant_id` | Optional tenant/app id on `join` and routed deliveries. |
| `Name` | `name` | Client name on `hello`, tool name on `tool.call` / `tool.result`, hook event name on `hook`. |
| `Data` | `data` | Structured payload for `hello`, `event`, `request`, and `reply`. |
| `Input` | `input` | Tool input payload on `request topic=tool.call`. |
| `Output` | `output` | Tool output string on `reply topic=tool.result`. |
| `Context` | `context` | Session prompt context on `event topic=session.init`. |
| `Tools` | `tools` | Tool catalog on `event topic=session.init`. |
| `Meta` | `meta` | Opaque metadata; routed deliveries also include kernel-controlled `meta.kernel`. |
| `Payload` | `payload` | Hook payload on `hook` / `hook_reply`. |
| `Action` | `action` | Hook reply action: `pass`, `modify`, `block`, or `claim`. |
| `Reason` | `reason` | Optional human-readable reason on `hook_reply` or `error`. |

## Handshake

Client -> kernel:

```json
{
  "v": 3,
  "type": "hello",
  "data": {
    "name": "gateway-cli-main",
    "auth_token": "ktk_...",
    "send_topics": ["message.user", "turn.cancel"],
    "receive_topics": ["session.init", "stream.start", "stream.delta", "stream.end", "turn.done", "error"],
    "global_topics": []
  }
}
```

Kernel -> client:

```json
{
  "v": 3,
  "type": "hello_ack",
  "data": {
    "client_id": "c1",
    "server_protocol": 3
  }
}
```

Notes:

- The kernel rejects missing or mismatched `v`.
- `auth_token` must match `$TABULA_HOME/run/kernel-client-token` or `TABULA_KERNEL_TOKEN`.
- Capabilities are declared as topic names under `send_topics`, `receive_topics`, and `global_topics`.

## Session Join

Client -> kernel:

```json
{
  "v": 3,
  "type": "join",
  "session": "main",
  "tenant_id": "default"
}
```

Kernel -> client:

```json
{
  "type": "joined",
  "session": "main",
  "tenant_id": "default"
}
```

If the client can receive `session.init`, the kernel then sends:

```json
{
  "type": "event",
  "topic": "session.init",
  "context": "You are Tabula.",
  "tools": [
    {"name": "exec_run"},
    {"name": "fs_read"}
  ],
  "meta": {
    "workspace": {"path": "/repo"}
  }
}
```

Other session members may receive:

```json
{
  "type": "event",
  "topic": "session.member_joined",
  "name": "gateway-cli-main",
  "session": "main"
}
```

## User Message Flow

Client -> kernel:

```json
{
  "v": 3,
  "type": "event",
  "topic": "message.user",
  "data": {"text": "Review this diff"},
  "meta": {"source": "gateway-cli"}
}
```

Routed delivery example:

```json
{
  "type": "event",
  "topic": "message.user",
  "data": {"text": "Review this diff"},
  "meta": {
    "source": "gateway-cli",
    "kernel": {
      "sender": {"name": "gateway-cli-main"},
      "route": {"scope": "session", "session": "main", "tenant_id": "default"}
    }
  }
}
```

Assistant streaming back:

```json
{"type":"event","topic":"stream.start"}
{"type":"event","topic":"stream.delta","data":{"text":"Looking at the diff..."}}
{"type":"event","topic":"stream.end"}
```

Reasoning stream uses the same envelope with topics:

- `reasoning.start`
- `reasoning.delta`
- `reasoning.end`

Compaction notifications use:

- `compaction.start`
- `compaction.end`
- `compaction.error`

## Tool Calls

Driver -> kernel:

```json
{
  "v": 3,
  "type": "request",
  "topic": "tool.call",
  "id": "call-1",
  "name": "exec_run",
  "input": {"command": "pwd"}
}
```

Kernel -> session observers / helpers:

```json
{
  "type": "request",
  "topic": "tool.call",
  "id": "call-1",
  "name": "exec_run",
  "input": {"command": "pwd"}
}
```

Kernel -> driver when the tool finishes:

```json
{
  "type": "reply",
  "topic": "tool.result",
  "id": "call-1",
  "name": "exec_run",
  "output": "/repo"
}
```

## Exchanges

Interactive selection requests use `exchange.choose` or `exchange.approve`.

Approval example:

```json
{
  "v": 3,
  "type": "request",
  "topic": "exchange.approve",
  "id": "approve-1",
  "data": {
    "question": "Approve tool exec_run?",
    "options": ["allow once", "allow always", "deny once", "deny always"]
  }
}
```

Reply:

```json
{
  "v": 3,
  "type": "reply",
  "topic": "exchange.approve",
  "id": "approve-1",
  "data": {
    "choice": "allow once",
    "index": 0
  }
}
```

Only the chosen responder may answer a pending exchange id.

## Usage And Turn Completion

Usage updates are sent as events, not `status` frames:

```json
{
  "type": "event",
  "topic": "usage.update",
  "data": {
    "usage": {
      "input_tokens": 2400,
      "output_tokens": 120,
      "reasoning_tokens": 0,
      "cache_read_tokens": 0,
      "cache_write_tokens": 0,
      "context_window": 123456,
      "percent": 2
    }
  }
}
```

Turn completion:

```json
{
  "type": "event",
  "topic": "turn.done",
  "data": {
    "usage": {
      "input_tokens": 2400,
      "output_tokens": 120
    }
  }
}
```

Cancellation request:

```json
{
  "v": 3,
  "type": "event",
  "topic": "turn.cancel"
}
```

## Hooks

Kernel -> hook subscriber:

```json
{
  "type": "hook",
  "id": "hook-1",
  "name": "before_tool_call",
  "session": "main",
  "tenant_id": "default",
  "payload": {
    "tool": "exec_run",
    "id": "call-1",
    "input": {"command": "pwd"}
  }
}
```

Subscriber -> kernel:

```json
{
  "v": 3,
  "type": "hook_reply",
  "id": "hook-1",
  "action": "modify",
  "payload": {
    "tool": "exec_run",
    "id": "call-1",
    "input": {"command": "pwd", "approved": true}
  }
}
```

`hook_reply` is identity-bound to the subscriber that received the matching
`hook` frame.

## Errors

Example:

```json
{
  "type": "error",
  "text": "unsupported protocol version 0 (kernel expects 3)"
}
```

## Compatibility Notes

- There is no legacy fallback for `connect`, `connected`, `message`, `tool_use`,
  `tool_result`, `status`, `done`, or `cancel` on the kernel client WebSocket.
- Current first-party SDKs map high-level helpers onto v3 frames automatically,
  but the wire protocol itself is `hello` + `event` / `request` / `reply`.

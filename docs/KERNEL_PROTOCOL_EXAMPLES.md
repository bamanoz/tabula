# Kernel Protocol v4 Examples

The authoritative contract is `docs/KERNEL_PROTOCOL_V4.md`. These examples show
the client WebSocket boundary only. Runtime API and worker examples remain in
`docs/PROTOCOL.md`.

## Open Connection

```json
{
  "v": 4,
  "kind": "command",
  "op": "connection.open",
  "id": "open-01",
  "data": {
    "name": "gateway-web",
    "auth_token": "ktk_...",
    "meta": {"tabula.client_role": "user"}
  }
}
```

## Create Session

```json
{
  "v": 4,
  "kind": "command",
  "op": "session.create",
  "id": "create-01",
  "tenant_id": "default",
  "session_id": "main",
  "data": {
    "driver_component_id": "driver",
    "agent_spec_revision": "spec-01"
  }
}
```

## Submit Durable Input

```json
{
  "v": 4,
  "kind": "command",
  "op": "input.submit",
  "id": "submit-01",
  "tenant_id": "default",
  "session_id": "main",
  "data": {
    "input_id": "input-01",
    "content": {"text": "Explain the failing build"}
  }
}
```

The successful result uses `op: "input.accepted"` and returns durable input,
turn, session-version, and cursor identifiers.

## Reconnect

1. Query `session.get` for the authoritative projection and current cursor.
2. Query `session.subscribe` with the last committed cursor.
3. Apply returned committed events in cursor order.
4. Repeat from the returned cursor after another disconnect.

## Tool Call

```json
{
  "v": 4,
  "kind": "command",
  "op": "tool.call",
  "id": "tool-01",
  "tenant_id": "default",
  "session_id": "main",
  "data": {
    "name": "exec_run",
    "input": {"cmd": "go test ./..."}
  }
}
```

Streaming frames use `event` envelopes with `tool.result.start`,
`tool.result.delta`, and `tool.result.end`. The terminal response is a `result`
envelope with `op: "tool.result"` and the original command ID.

## Extension Traffic

Non-authoritative hook, exchange, and custom topic messages use
`command op: "extension.send"`. Recipients receive `event op:
"extension.event"`. Extension payloads cannot replace durable client or driver
operations.

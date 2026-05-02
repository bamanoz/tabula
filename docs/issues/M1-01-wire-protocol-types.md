# M1-01 — Wire protocol types (Runtime API)

Status: open
Phase: M1
Type: AFK
Labels: needs-triage, area/runtime, phase/m1

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M1)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md`

## What to build

Define the Go types for every Runtime API frame that travels
between kernel and `tabula-runtime` daemon. Per ADR §4 and plan
§2.4, the kernel speaks one protocol to the runtime regardless of
backend.

This is pure scaffolding — no behavior change in production code,
no transport, no implementation. Just types + JSON
marshal/unmarshal + round-trip tests.

Types to define:

- Envelope with `op` discriminator and `call_id` correlation.
- `Hello` (handshake from runtime to kernel: token, runtime_id,
  capabilities preview).
- `HelloAck` (kernel → runtime, accept/reject).
- `Invoke` (kernel → runtime: tenant_id, target, tool, args,
  timeout). `tenant_id` is mandatory.
- `InvokeResult` (runtime → kernel: ok, data | error{code,
  message, retryable}).
- `Cancel` (kernel → runtime: call_id).
- `CancelAck` (runtime → kernel: call_id).
- `Health` (kernel → runtime, no payload).
- `HealthResp` (runtime → kernel: ok, uptime, worker_count).
- `ListCapabilities` (kernel → runtime).
- `ListCapabilitiesResp` (runtime → kernel: targets[]).
- `Reload` (kernel → runtime: optional target filter).
- `ReloadAck` (runtime → kernel: evicted targets).

Location: new package `internal/runtime/wire/`.

## Acceptance criteria

- [ ] All types declared in `internal/runtime/wire/` with field
      docstrings (what the field means, when it is required).
- [ ] JSON tags consistent (snake_case) and stable.
- [ ] Round-trip test for every op: `marshal → unmarshal → equal`.
- [ ] Test that `Invoke` without `tenant_id` fails decoding with
      a clear protocol error.
- [ ] Test that an unknown `op` value is rejected with a clear
      protocol error (forward compat: do not crash, return
      structured error).
- [ ] Error codes constants defined: `runtime_unavailable`,
      `tool_not_found`, `target_not_authorized`, `tenant_denied`,
      `timeout`, `protocol_error`, `internal_error`.
- [ ] `go build ./...` clean.
- [ ] `go test ./internal/runtime/wire/...` green.

## Blocked by

None — can start immediately.

## Notes

- Use `json.RawMessage` for `Invoke.args` and `InvokeResult.data`
  to avoid coupling wire types to specific tool schemas.
- `call_id` should be opaque string (UUID v4 from kernel), not
  parsed by runtime.
- This slice does NOT define how frames are framed on the wire
  (WebSocket, unix socket, stdio). That is a later milestone.

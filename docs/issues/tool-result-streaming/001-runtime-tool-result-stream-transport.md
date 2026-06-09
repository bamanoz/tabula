# Runtime tool result stream transport

Type: Runtime / Feature

Priority: P1

Status: Completed

Repos: `tabula`

## Parent

`docs/issues/tool-result-streaming/README.md`

## Problem

Runtime invoke responses are currently single terminal frames.
`internal/runtime/codec/codec.go` caps a runtime frame at `1 << 20` bytes, so a
single large successful `InvokeResult.Data` can fail before the kernel has any
chance to rewrite, artifact, or page it.

## What to build

Add streamed tool-result transport from the runtime host to the kernel runtime
connection.

The worker protocol continues to return one `WorkerResult` to the runtime host in
this rollout. That hop is not capped by the Runtime API `MaxFrameBytes` limit;
streaming it can be added as a separate worker-protocol change if process-memory
pressure becomes the next bottleneck.

Use explicit lifecycle frames keyed by `call_id`, analogous to existing stream
topics, but internal to runtime invoke transport.

The first slice should only make large results transport-safe. It does not need
to introduce artifacting or hook rewriting yet.

## Acceptance criteria

- [x] Runtime API wire types support the same streamed invoke result lifecycle.
- [x] Large successful tool output no longer relies on a single `InvokeResult`
      frame staying under `MaxFrameBytes`.
- [x] Runtime-side protocol validation rejects out-of-order or malformed stream
      frames clearly.
- [x] Existing small tool results still use the current simple terminal path.

## Files

- Edit: `internal/runtime/worker/wire/types.go`
- Edit: `internal/runtime/worker/wire/types_test.go`
- Edit: `internal/runtime/wire/types.go`
- Edit: `internal/runtime/wire/types_test.go`
- Edit: `internal/runtime/wire/codec.go`
- Edit: `internal/runtime/host/policy/bare/bare.go`
- Edit: `internal/runtime/host/policy/bare/bare_test.go`
- Edit: `internal/runtime/conn/conn.go`
- Edit: `internal/runtime/conn/conn_test.go`

## Verify

```bash
go test ./internal/runtime/worker/wire ./internal/runtime/wire ./internal/runtime/host/policy/bare ./internal/runtime/conn
```

## Completion Notes

- Runtime API now has `invoke_result_start`, `invoke_result_delta`, and
  `invoke_result_end` lifecycle frames.
- `RuntimeConn.InvokeStream` lets the kernel consume result bytes through a sink
  instead of requiring a single in-memory terminal frame.
- Runtime host automatically streams oversized successful invoke results while
  preserving ordinary small-result behavior.
- Worker-to-runtime-host streaming was kept out of this slice because the first
  production failure was the capped Runtime API frame between host and kernel.

## Risk

Medium.

This changes runtime protocol semantics in a hot path. The main risk is breaking
normal small invoke responses while introducing stream handling.

## Notes

- Keep streamed chunks well below the frame cap so JSON overhead stays safe.
- Do not couple this slice to artifacting policy.
- Preserve the current `Invoke`/`InvokeResult` path for ordinary small results.

# M1-06 — In-memory end-to-end protocol round-trip test

Status: done
Phase: M1
Type: AFK
Labels: needs-triage, area/runtime, area/testing, phase/m1

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M1)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md`

## What to build

Prove that the wire types, worker protocol types, interfaces, and
mock collectively form a coherent contract by writing an
end-to-end test that connects a "kernel-like client" and a
"runtime-like server" through an in-memory pipe and exercises
every Runtime API op.

This is the M0 → M1 acceptance gate: if this test passes, the
contract is ready to grow real transports in M2.

Test architecture:

- `net.Pipe()` produces two `net.Conn`s.
- One side runs a minimal "fake runtime server" that:
  - Reads wire frames from the pipe.
  - Dispatches to programmed handlers (programmable per op).
  - Writes response frames back.
- Other side runs a `RuntimeConn`-style client that:
  - Encodes wire frames from typed Go calls.
  - Reads response frames and decodes into typed results.
  - Provides the `RuntimeConn` interface from M1-03.

Note: this stretches into a thin protocol-codec implementation
that bridges types (M1-01) and the interface (M1-03). That codec
is **internal to the test** for M1; M2 productionizes it.

Scenarios covered:

1. **Handshake.** Client sends `Hello` with token; server returns
   `HelloAck`.
2. **Nominal Invoke.** Client `Invoke` → server returns
   `InvokeResult{ok: true, data: ...}`. Client receives the typed
   result.
3. **Error Invoke.** Server returns `InvokeResult{ok: false,
   error: {code: "tool_not_found"}}`. Client surfaces the error.
4. **Cancel propagation.** Client starts a long Invoke, then
   calls `Cancel(callID)`. Server receives Cancel, replies
   `CancelAck`, sends final `InvokeResult{error: {code:
   "cancelled"}}`. Client's Invoke returns the cancellation
   error.
5. **Health ping.** Client `Health()` → server returns
   `HealthResp`. Round-trip-able under load (10 pings while an
   Invoke is in flight).
6. **ListCapabilities.** Client gets a non-empty target list.
7. **Reload.** Client `Reload(target=fs)` → server replies with
   evicted targets.
8. **Disconnect.** Server closes the pipe mid-Invoke. Client's
   pending Invoke completes with `runtime_unavailable`,
   `retryable: true` (matches Q6 fail-fast semantics).
9. **Protocol error: missing tenant_id.** Client crafts an
   Invoke without tenant_id; server rejects with
   `protocol_error`. Client surfaces it.
10. **Concurrent Invokes.** 50 Invokes in parallel goroutines on
    one connection; all complete correctly. Race detector clean.

## Acceptance criteria

- [ ] All 10 scenarios implemented as Go tests.
- [ ] Test runs in <100ms total (no real network or
      filesystem; pure in-memory).
- [ ] Race-detector clean (`go test -race`).
- [ ] No `time.Sleep` in test logic; all synchronization through
      channels or `context.Context`.
- [ ] CI runs this test as part of `go test ./...`.
- [ ] Test failures produce clear diagnostics (which scenario,
      which assertion, what the actual frame looked like).

## Blocked by

- M1-01 (wire types)
- M1-02 (worker protocol types — used in cancel/health
  scenarios that check serialization symmetry)
- M1-03 (RuntimeConn interface — client side)
- M1-05 (mock can be reused as the server side or as a baseline
  for the test runtime stub)

## Notes

- This slice intentionally creates a thin codec
  (frame-encode/decode over `net.Conn`) inside the test so we can
  exercise the full pipeline. M2 will lift this codec out and
  productionize it on top of WebSocket and unix socket
  transports.
- If the test ergonomics are clean, the codec may be promoted to
  `internal/runtime/codec/` during M2 instead of being rewritten.
  Make this a likely outcome by writing readable, decoupled code.
- The "fake runtime server" in this test is NOT the
  `tabula-runtime` binary. It is a test fixture.

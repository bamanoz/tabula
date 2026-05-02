# M1-05 — Mock RuntimeConn for kernel unit tests

Status: open
Phase: M1
Type: AFK
Labels: needs-triage, area/runtime, area/testing, phase/m1

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M1)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§10)

## What to build

Provide a programmable mock implementation of `RuntimeConn` for
kernel-side unit tests. Per ADR §10, the kernel post-M2 has no
embedded plugin spawn; unit tests must use this mock to simulate
runtime responses without spinning up a real daemon.

Mock features:

- Fluent builder API to program responses by `(tenant_id, target,
  tool)` tuple or by `call_id`.
- Per-call delay injection (block N ms before responding).
- Per-call error injection covering every error code from M1-01
  (`runtime_unavailable`, `tool_not_found`,
  `target_not_authorized`, `tenant_denied`, `timeout`,
  `protocol_error`, `internal_error`).
- Recording: `RecordedInvokes() []InvokeReq` for assertion in
  tests.
- Cancel support: when kernel calls `Cancel(callID)` mid-flight,
  the mock unblocks the corresponding `Invoke` with a structured
  error and records the cancel.
- Health/ListCapabilities/Reload programmable responses.
- `Close()` drains any pending Invokes with `runtime_unavailable`
  (matches Q6 disconnect semantics).

Location: `internal/runtime/mock/`.

Example usage (illustrative, finalize during implementation):

```go
mock := mock.New().
    OnInvoke("tenant-a", "fs", "read_file").Return(`{"data": "..."}`).
    OnInvoke("tenant-a", "exec", "run").DelayMs(50).ReturnError(wire.ErrTimeout).
    OnHealth().Return(wire.HealthResp{Ok: true})

defer mock.Close()

// kernel-side test code uses mock as if it were a real RuntimeConn.
```

## Acceptance criteria

- [ ] `mock.RuntimeConn` implements the `RuntimeConn` interface
      (compile-time asserted).
- [ ] Builder API supports per-tuple response programming.
- [ ] Builder API supports per-tuple error injection for every
      error code constant.
- [ ] Builder API supports per-tuple delay injection.
- [ ] `RecordedInvokes()` returns invokes in chronological order
      with full request payload.
- [ ] Cancel mid-flight: test that a blocked Invoke returns with
      a cancel-induced error after `Cancel(callID)` is called.
- [ ] `Close()` test: pending Invokes complete with
      `runtime_unavailable` error (`retryable: true`).
- [ ] Concurrent Invokes from multiple goroutines work
      correctly (race-detector clean).
- [ ] `go test -race ./internal/runtime/mock/...` green.

## Blocked by

- M1-03 (implements the `RuntimeConn` interface defined there)

## Notes

- Mock is a test-only helper, NOT a production fallback. It does
  not satisfy the no-legacy concern about `embedded` backends —
  it is `internal/runtime/mock/` not `internal/runtime/embedded/`.
- Future tests across kernel packages will depend on this; design
  the API to feel natural in table-driven tests.
- Do not implement transport / framing in the mock. It implements
  the interface directly in-memory.

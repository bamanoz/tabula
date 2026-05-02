# M1-03 — Go interfaces: RuntimeConn and Backend

Status: open
Phase: M1
Type: AFK
Labels: needs-triage, area/runtime, phase/m1

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M1, §1.7, §2.3)
ADR: `docs/adr/0001-runtime-daemon-and-execution-backends.md` (§2)

## What to build

Define the Go interfaces that the kernel uses to talk to a
runtime, and the `Backend` interface that produces a
`RuntimeConn`. These are the seams that all execution backends
(`local`, `attach`, `ssh`, `docker`, …) implement.

Per ADR §2: a backend's only job is to produce a `RuntimeConn`.
Per ADR §10: the kernel has exactly one tool-execution code path
after M2; this interface is that path.

Interfaces:

- `RuntimeConn` (`internal/runtime/api.go`):
  - `Invoke(ctx, InvokeReq) (InvokeResp, error)`
  - `Cancel(ctx, callID string) error`
  - `Health(ctx) (HealthResp, error)`
  - `ListCapabilities(ctx) (ListCapabilitiesResp, error)`
  - `Reload(ctx, ReloadReq) (ReloadResp, error)`
  - `Close() error`

- `Backend` (`internal/runtime/api.go`):
  - `Connect(ctx) (RuntimeConn, error)`

- `Target` and `InvokeReq` / `InvokeResp` request types — Go
  structs that wrap the wire types from M1-01 in
  ergonomic-for-kernel form (e.g. `Target{Kind: "plugin", ID:
  "fs"}`).

Documentation on each method must specify:

- Blocking semantics (what blocks, what returns immediately).
- ctx cancellation behavior (does Close interrupt in-flight
  Invokes? answer per Q6: yes, fail-fast with
  `runtime_unavailable`).
- Concurrency: `Invoke` MUST be safe for concurrent calls from
  multiple goroutines on a single `RuntimeConn`.
- Lifecycle: a closed `RuntimeConn` must reject all subsequent
  ops with a clear error.

Provide a stub implementation `notImplementedConn` that returns
`errNotImplemented` from every method, used by other slices that
need a non-nil `RuntimeConn` placeholder.

## Acceptance criteria

- [ ] `RuntimeConn` and `Backend` interfaces declared with full
      docstrings (per-method semantics, blocking, ctx, concurrency,
      lifecycle).
- [ ] Request/response Go structs (`InvokeReq`, `InvokeResp`,
      `Target`, `HealthResp`, etc.) declared and documented.
- [ ] `notImplementedConn` stub provided.
- [ ] Compile-time assertion `var _ RuntimeConn = (*notImplementedConn)(nil)`.
- [ ] Test that `notImplementedConn` returns `errNotImplemented`
      from every method.
- [ ] `go build ./...` clean.
- [ ] `go test ./internal/runtime/...` green.

## Blocked by

- M1-01 (uses wire types in request/response structs)

## Notes

- This slice does NOT introduce a real implementation. Real
  `RuntimeConn` over unix socket / WSS lands in M2.
- Kernel callers should depend on `RuntimeConn`, never on a
  concrete type.
- `Backend.Connect` returning a `RuntimeConn` is what makes
  backends interchangeable — no other surface differs.

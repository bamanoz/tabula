# App SDK runtime affinity handshake

Type: App SDK / Protocol

Priority: P1

Status: Completed

Repos: `tabula`

## Parent

`docs/issues/app-runtime-affinity/README.md`

## Problem

Even if a runtime has a stable local `runtime_id`, the app process has no
standard way to discover and report that nearby runtime to the kernel.

Without a shared handshake contract, the kernel cannot distinguish:

- an app launched next to runtime `rt-laptop`
- an app launched next to runtime `rt-desktop`

when both attach to the same remote kernel and tenant.

## What to build

Define and implement the app-side affinity handshake contract.

Behavior:

- App SDK reads local runtime metadata if present.
- On client connect/hello, the SDK includes a normalized runtime-affinity hint
  in client metadata.
- The hint should be optional: apps without local runtime metadata still
  connect normally.
- The kernel connect path should preserve this metadata as authenticated client
  metadata so later routing stages can inspect it.

Suggested metadata key:

- `tabula.runtime_id`

or an equivalent kernel-owned normalized key if the connect path rewrites it.

## Acceptance criteria

- [x] A shared SDK helper loads local runtime metadata.
- [x] Connect/hello metadata includes the local runtime id when present.
- [x] Apps without metadata still connect successfully.
- [x] Kernel stores the resulting client metadata without losing the affinity
      hint.
- [x] The contract is documented well enough that non-Go app clients can follow
      it later.

## Files

- Add: `sdk/app/runtime_affinity.go`
- Add: `sdk/app/runtime_affinity_test.go`
- Edit: `internal/kernel/client_meta_test.go`

## Verify

```bash
go test ./sdk/app ./internal/kernel -race -count=1
```

## Risk

Low.

This issue should not change dispatch yet. The main risk is inventing a metadata
shape that later dispatch code cannot consume cleanly.

## Notes

- Treat SDK-provided runtime affinity as untrusted input until the kernel checks
  it against attached runtime state and tenant bindings.
- Keep the on-wire contract narrow: one runtime id hint is enough for the first
  rollout.
- The shared Go contract now lives in `sdk/app`: `LoadLocalRuntimeID()` reads
  `$TABULA_HOME/run/runtime-instance.json`, and `WithRuntimeAffinity(meta)`
  injects `tabula.runtime_id` into hello metadata when a local runtime id is
  present.
- Non-Go clients should follow the same wire contract: send a hello
  `data.meta` JSON object and set `tabula.runtime_id` to the local
  `runtime_id` from `$TABULA_HOME/run/runtime-instance.json` when that file
  exists.

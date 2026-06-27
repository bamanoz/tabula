# Runtime instance metadata

Type: Runtime / Infra

Priority: P1

Status: Completed

Repos: `tabula`

## Parent

`docs/issues/app-runtime-affinity/README.md`

## Problem

Runtime identity already exists at the transport layer, but there is no stable
local metadata contract that both the runtime daemon and co-installed apps can
read.

Today the runtime side still leans on the local placeholder id (`local`) unless
the launcher passes an override explicitly. That is not enough for a remote
kernel deployment where multiple devices each need their own durable runtime
identity and local discovery path.

## What to build

Add a runtime metadata file under `$TABULA_HOME/run` that records the stable
runtime instance identity and related local metadata.

Suggested shape:

```json
{
  "runtime_id": "rt_...",
  "created_at": "2026-06-27T12:00:00Z",
  "version": 1
}
```

Behavior:

- On first startup, create the metadata file if it does not exist.
- On later startups, reuse the stored `runtime_id`.
- Validate the stored `runtime_id` with the existing runtime-id validation
  rules.
- Expose a small loader/helper so launchers and SDKs read the same contract.
- Keep token handling separate; this issue is about stable identity metadata,
  not provisioning workflow.

## Acceptance criteria

- [x] A canonical runtime metadata path exists under `$TABULA_HOME/run`.
- [x] First-run bootstrap creates a stable `runtime_id` when metadata is absent.
- [x] Subsequent startups reuse the same `runtime_id`.
- [x] Invalid or corrupted metadata fails clearly.
- [x] Runtime-side helpers and tests use the shared metadata loader instead of
      duplicating file parsing.

## Files

- Add: `internal/runtime/instance/meta.go`
- Add: `internal/runtime/instance/meta_test.go`
- Edit: `internal/runtime/paths/paths.go`
- Edit: `internal/runtime/host/dialer/dialer.go`
- Edit: `cmd/tabula-runtime/main.go`

## Verify

```bash
go test ./internal/runtime/instance ./internal/runtime/host/dialer ./cmd/tabula-runtime
```

## Risk

Medium.

If metadata bootstrap is sloppy, runtime identity can churn accidentally and
break token/auth expectations. Keep the file format tiny and validation strict.

## Notes

- Do not put secrets in this file.
- Prefer a dedicated helper package instead of sprinkling ad-hoc JSON parsing
  across commands.
- Landed via `internal/runtime/instance`, `internal/runtime/paths`, and
  `cmd/tabula-runtime` runtime-id bootstrap wiring.

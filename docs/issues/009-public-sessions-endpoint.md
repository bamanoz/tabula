# Public Sessions Endpoint

Priority: Medium

Repos: `tabula`, `tabula-bundles`

## Problem

`/sessions` is exposed as an unauthenticated HTTP endpoint. The runtime snapshot
endpoint has a local diagnostics guard, but `/sessions` does not.

## Evidence

- `internal/tabula/app.go`: `/sessions` writes `hub.SnapshotSessions()` without
  `internalDiagnosticsGuard`.
- `internal/tabula/app.go`: `/internal/snapshot/runtimes` is guarded by remote
  address and Host checks.
- `tabula-bundles/base/sessions/run.py` and gateway web code fetch `/sessions`
  directly.

## Impact

Anyone who can reach the kernel listener can list session metadata. If the
kernel is bound to a non-loopback interface, this leaks tenant/session activity
and titles.

## Proposed Fix

- Decide whether `/sessions` is an internal diagnostics endpoint or a public
  local API.
- If internal, protect it with the same local diagnostics guard or kernel auth.
- If public-local, enforce loopback-only access and document the contract.
- Update sessions plugin/gateways to use the protected path or authenticated
  API.

## Acceptance Criteria

- Remote requests to `/sessions` are rejected when listener is wildcard/public.
- Local sessions plugin and web gateway still function.
- Tests mirror runtime snapshot guard cases for `/sessions`.

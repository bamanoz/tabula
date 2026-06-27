# Preferred runtime dispatch and fallback

Type: Kernel / Runtime dispatch

Priority: P1

Status: Completed

Repos: `tabula`

## Parent

`docs/issues/app-runtime-affinity/README.md`

## Problem

The kernel currently resolves runtime-hosted tools by tenant binding and
tool-owned runtime registration, with fallback to the tenant default runtime.
It does not have a dispatch step that says “prefer the runtime associated with
the app that originated this turn, if that runtime is attached and allowed.”

As a result, a shared session across two devices cannot naturally prefer the
runtime next to the active app instance.

## What to build

Teach runtime dispatch to honor normalized preferred-runtime affinity before
falling back to existing tenant-default behavior.

Behavior:

- When dispatching a runtime-hosted tool, first check whether the current
  session/turn has a preferred runtime.
- If preferred runtime is attached, serves the tenant, and exposes the requested
  tool/target, dispatch there.
- If preferred runtime is unavailable, disallowed, or does not expose the tool,
  fall back to current dispatch behavior.
- Emit explicit diagnostics/logging for the fallback reason.
- Keep deterministic behavior for overlapping tools published by multiple
  runtimes.

## Acceptance criteria

- [x] Tool dispatch checks preferred runtime before generic tenant selection.
- [x] Preferred runtime is validated against tenant bindings and attached state.
- [x] Missing/ineligible preferred runtime falls back safely.
- [x] Logs or observable diagnostics explain why fallback happened.
- [x] Tests cover a one-tenant/two-runtime scenario with distinct and
      overlapping tool sets.

## Files

- Edit: `internal/kernel/tool_service.go`
- Edit: `internal/kernel/runtime_async.go`
- Edit: `internal/kernel/runtime_registry.go`
- Edit: `internal/kernel/tool_dispatch_test.go`
- Edit: `internal/kernel/runtime_async_test.go`

## Verify

```bash
go test ./internal/kernel -race -count=1
```

## Risk

Medium.

The main risk is creating surprising precedence between preferred runtime,
tool-owned runtime registration, and tenant defaults. Keep the resolution order
explicit and covered by tests.

## Notes

- Fallback should preserve today's behavior exactly when no preferred runtime is
  set.
- Surface enough diagnostics that operators can understand “why did this call go
  to runtime B instead of runtime A?”
- Preferred-runtime routing and safe fallback logic are now wired in kernel
  dispatch.
- When the preferred runtime cannot be used, kernel logs now include
  `preferred_runtime_id`, `selected_runtime_id`, and the fallback reason/code.

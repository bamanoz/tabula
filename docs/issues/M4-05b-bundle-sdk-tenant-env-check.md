# M4-05b — Bundle SDK: tenant env defense-in-depth check

Status: open
Phase: M4
Type: AFK
Repo: tabula-bundles
Labels: needs-triage, area/sdk, area/security, phase/m4

## Parent

Plan: `docs/plans/REMOTE_RUNTIME.md` (M4)
Companion to: `M4-05` (runtime-side enforcement)
Amendment: AMENDMENTS.md C5

## What to build

Worker-layer of the three-layer tenant enforcement (ADR §6).
At worker startup, both SDKs read `TABULA_TENANT_ID` and
`TABULA_KERNEL_ID`; if either is missing/empty, the SDK
refuses to invoke any tool handler and exits with
`internal_error` envelope.

Components:

- `tabula_plugin_sdk.runtime`:
  - Module-load checks that `TABULA_TENANT_ID` and
    `TABULA_KERNEL_ID` are set and non-empty.
  - On failure, write `{ok: false, error: {code:
    "internal_error", message: "missing tenant/kernel context"}}`
    to stdout and exit 1 before the dispatch loop runs.
- `tabula_plugin_sdk.runtime.current_tenant_id() -> str`:
  returns `$TABULA_TENANT_ID`.
- `tabula_plugin_sdk.runtime.current_kernel_id() -> str`:
  returns `$TABULA_KERNEL_ID`.
- Same for `tabula_skill_sdk.runtime`.

## Acceptance criteria

- [ ] Worker started without `TABULA_TENANT_ID` writes
      structured error and exits 1; harness surfaces clean
      `internal_error` to runtime.
- [ ] Worker started with both env vars runs normally.
- [ ] `current_tenant_id()` / `current_kernel_id()`
      accessible to plugin/skill code.
- [ ] Tests cover happy path + each missing-env path.

## Blocked by

- M4-05 (runtime sets the env vars on workers it spawns)
- M4-03b (SDK refactor scope alignment)

## Notes

- The point of this slice is defense in depth, not primary
  enforcement. M4-05 is the primary; this catches sloppy
  refactors that drop the env or misconfigured runtimes that
  ship without tenant context.
